package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/inference"
	"github.com/reticle/runtime/routing"
)

// StartInferenceProxy starts the loopback inference proxy (RFC-052). Without it, every attempt
// keeps the direct path: the worker calls its provider and a failed request restarts the worker.
func (d *Dispatcher) StartInferenceProxy() error {
	if d.Inference != nil {
		return nil
	}
	proxy, err := inference.New()
	if err != nil {
		return err
	}
	d.Inference = proxy
	return nil
}

func (d *Dispatcher) CloseInferenceProxy(ctx context.Context) error {
	if d.Inference == nil {
		return nil
	}
	return d.Inference.Close(ctx)
}

var (
	runsModelLoop  = regexp.MustCompile(`(?m)^from worker_sdk import .*\brun\b`)
	proxyAwareSeen sync.Map // entrypoint path -> bool
)

// proxyAwareWorker reports whether a worker sends its model requests through worker_sdk.run,
// which honours RETICLE_INFERENCE_URL. Any other worker (the architect calls LiteLLM itself) keeps
// the direct path and its credential.
func proxyAwareWorker(w *Worker) bool {
	if w == nil {
		return false
	}
	for _, arg := range w.Args {
		if !strings.EqualFold(filepath.Ext(arg), ".py") {
			continue
		}
		if known, ok := proxyAwareSeen.Load(arg); ok {
			return known.(bool)
		}
		source, err := os.ReadFile(arg)
		aware := err == nil && runsModelLoop.Match(source)
		if err == nil {
			proxyAwareSeen.Store(arg, aware)
		}
		return aware
	}
	return false
}

// beginProxiedInference opens a proxy session for one attempt when the attempt's route and worker
// allow it, and returns the environment the worker needs. budget is how many failovers the
// attempt may make: each consumes one of the node's attempts, as RFC-052 requires.
// RETICLE_INFERENCE_PROXY=false keeps every attempt on the direct path.
func (d *Dispatcher) beginProxiedInference(w *Worker, t Task, route *routing.Model, routes *retryRoutes, tier, budget int) (*inference.Session, []string) {
	if d.Inference == nil || d.Router == nil || route == nil || os.Getenv("RETICLE_INFERENCE_PROXY") == "false" ||
		!inference.Eligible(*route) || !proxyAwareWorker(w) {
		return nil, nil
	}
	session, environment, err := d.Inference.Begin(*route)
	if err != nil {
		d.Logger.Error("Inference proxy session could not start; this attempt calls its provider directly.", "worker_id", w.ID, "error", err)
		return nil, nil
	}
	session.Failover = func(failed routing.Model, text string) (routing.Model, bool) {
		failure := &WorkerFailure{Reason: WorkerExitedNonZero, ExitCode: 1, Stderr: text}
		disposition := classifyProviderFailure(failure)
		// The same facts a restarted attempt would record: task-local exclusions, learned size limits,
		// cooled routes and credentials. An output-token bound is not applied here; it belongs to the
		// whole request budget and is handled when an attempt restarts.
		routes.failed(&failed, disposition, failure, t.Memory["llm_max_tokens"])
		d.learnProviderFailure(w, t, &failed, disposition, failure, routes)
		if !disposition.retryable || session.Failovers() >= budget {
			return routing.Model{}, false
		}
		d.Router.Release(string(t.ID))
		// Routes the proxy cannot serve are skipped for this request only: a later restart of the
		// worker may still use them directly.
		policy := routes.policy
		policy.Excluded = make(map[string]bool, len(routes.policy.Excluded))
		for key := range routes.policy.Excluded {
			policy.Excluded[key] = true
		}
		for picks := 0; picks < 16; picks++ {
			next := d.Router.SelectModelWithPolicy(string(t.ID), string(w.ID), tier, 0.90, t.Modality, policy)
			if next == nil {
				return routing.Model{}, false
			}
			if !inference.Eligible(*next) {
				policy.Excluded[next.Key()] = true
				d.Router.Release(string(t.ID))
				continue
			}
			routes.started(next)
			d.Logger.Info("Provider rejected a request; the inference proxy repeats only that request on another route.",
				"worker_id", w.ID, "task_id", t.ID, "from", failed.ID, "to", next.ID, "category", disposition.category)
			return *next, true
		}
		return routing.Model{}, false
	}
	return session, environment
}

// learnProviderFailure records what a provider failure proves about the route, the credential or
// the model, for every later selection. route is the route that failed.
func (d *Dispatcher) learnProviderFailure(w *Worker, t Task, route *routing.Model, disposition providerFailureDisposition, failure *WorkerFailure, routes *retryRoutes) {
	if d.Router == nil {
		return
	}
	apiKeyEnv, _ := t.Parameters["api_key"].(string)
	modelID, _ := t.Parameters["llm_model"].(string)
	if route != nil {
		apiKeyEnv, modelID = route.APIKeyEnv, route.ID
	}
	if disposition.penalizeProvider {
		if apiKeyEnv != "" {
			// A provider that says when its quota resets is believed, so an exhausted
			// daily limit is not probed again every 15 minutes.
			resetAt := routing.QuotaResetAt(failure.Stderr, time.Now())
			if resetAt.IsZero() && isBillingFailure(failure.Stderr) {
				resetAt = time.Now().Add(routing.BillingRetryAfter)
			}
			d.Router.PenalizeProviderForUntil(string(w.ID), apiKeyEnv, disposition.category, disposition.category, resetAt)
		}
		if disposition.penalizeFamily {
			if separator := strings.IndexByte(modelID, '/'); separator > 0 {
				d.Router.PenalizeProviderFamilyUntil(modelID[:separator], routing.QuotaResetAt(failure.Stderr, time.Now()))
			}
		}
	} else if disposition.disableModel {
		if modelID != "" {
			d.Logger.Info("Model is incompatible with the required chat/tool API; disabling only this model and trying another.", "model", modelID)
			d.Router.PenalizeModel(modelID)
		}
	} else if disposition.category == "model_unavailable" && route != nil {
		d.Logger.Info("Provider does not serve this model to this credential; skipping the route for every task.", "worker_id", w.ID, "model", route.ID)
		d.Router.MarkRouteDead(*route, time.Now().Add(modelUnavailableFor), "model not found for this credential")
	} else if disposition.category == "provider_rate_window" && route != nil {
		// Only this route waits, for as long as the provider said; a request that alone takes
		// most of the window is kept off the route for tasks of that size.
		text := finalExceptionLine(failure.Stderr)
		if window, ok := routing.ParseRateWindow(text); ok {
			wait := window.Cooldown()
			d.Logger.Info("Rate window used up on one route; cooling only that route.", "worker_id", w.ID, "model", route.ID, "wait", wait.String())
			d.Router.MarkRouteUnavailable(route.Key(), time.Now().Add(wait), fmt.Sprintf("rate window: limit %d, used %d, requested %d", window.Limit, window.Used, window.Requested))
			if cap := window.EffectiveRequestCap(); cap > 0 && routing.IsPerMinute(text) {
				d.Router.LearnRequestLimit(route.Key(), cap)
			}
		}
	} else if disposition.category == "model_request" {
		d.Logger.Info("Model rejected this request; lowering its score and trying another.", "worker_id", w.ID)
		// "Limit 8000, Requested 11399": a size cap is a fact about the route. Learn it so
		// later tasks that cannot fit skip it, and spare this task the same model on
		// its other credentials.
		if route != nil {
			if limit := routing.ParseRequestLimit(finalExceptionLine(failure.Stderr)); limit > 0 {
				d.Router.LearnRequestLimit(route.Key(), limit)
				if routes.policy.MinRequestTokens > limit {
					for _, sibling := range d.Router.RoutesOfModel(route.ID, route.Provider) {
						routes.policy.Excluded[sibling] = true
					}
				}
			}
		}
	}
	if disposition.category == "provider_transient" || disposition.category == "timeout" {
		if route != nil {
			d.Router.CoolProviderSlot(routing.ProviderSlot(*route), time.Minute)
		}
	}
	d.Router.UpdateProbability(string(w.ID), string(t.ID), false)
}
