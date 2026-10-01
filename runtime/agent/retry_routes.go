package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/reticle/runtime/routing"
)

// The ledger lives for one task, not one worker or provider. Routing preferences
// are separate from provider health and never authorize a worker replay.
type retryRoutes struct {
	policy           routing.RoutePolicy
	providerFailures map[string]int
	limits           map[string]int
	attempts         int
	providers        map[string]bool
}

func newRetryRoutes() *retryRoutes {
	return &retryRoutes{
		policy:           routing.RoutePolicy{Excluded: map[string]bool{}, Tried: map[string]bool{}, AvoidProviders: map[string]bool{}},
		providerFailures: map[string]int{}, limits: map[string]int{}, providers: map[string]bool{},
	}
}

func (h *retryRoutes) started(model *routing.Model) {
	h.attempts++
	h.policy.PreferredKey = ""
	if model != nil {
		h.policy.Tried[model.Key()] = true
		h.providers[model.Provider] = true
	}
}

// Adjust only an explicit output bound, never a context/input window. Consume
// another normal attempt, once per route. A model change restores the task's
// original token budget rather than inheriting another model's restriction.
var outputLimitPattern = regexp.MustCompile(`(?i)max[_ ]tokens must be less than( or equal to)? ([0-9]+)`)

func (h *retryRoutes) failed(model *routing.Model, disposition providerFailureDisposition, failure *WorkerFailure, requested any) bool {
	if model == nil {
		return false
	}
	key := model.Key()
	if disposition.category == "model_request" && h.limits[key] == 0 {
		// The final exception line contains the response, unlike preceding
		// traceback frames which can include prompts or source code.
		lines := strings.Split(strings.TrimSpace(failure.Stderr), "\n")
		match := outputLimitPattern.FindStringSubmatch(lines[len(lines)-1])
		var current int
		fmt.Sscan(fmt.Sprint(requested), &current)
		if len(match) == 3 {
			limit, err := strconv.Atoi(match[2])
			if match[1] == "" {
				limit--
			}
			if err == nil && limit > 0 && current > limit {
				h.limits[key] = limit
				h.policy.PreferredKey = key
				return true
			}
		}
	}
	if disposition.category == "model_request" || disposition.category == "model_incompatible" {
		h.policy.Excluded[key] = true
		h.providerFailures[model.Provider]++
		if h.providerFailures[model.Provider] >= 2 {
			h.policy.AvoidProviders[model.Provider] = true
		}
	}
	if disposition.category == "provider_transient" || disposition.category == "timeout" {
		h.policy.AvoidProviders[model.Provider] = true
	}
	return false
}

func (h *retryRoutes) summary() string {
	return fmt.Sprintf("attempts=%d, distinct_routes=%d, providers_tried=%d, task_excluded_routes=%d", h.attempts, len(h.policy.Tried), len(h.providers), len(h.policy.Excluded))
}
