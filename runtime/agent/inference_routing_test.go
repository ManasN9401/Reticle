package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/inference"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/routing"
)

// The proxy describes a provider failure for the dispatcher's own classifier, so a failure seen by
// the proxy is judged exactly as the same failure seen in a worker's stderr.
func TestProxyFailuresAreClassifiedLikeWorkerFailures(t *testing.T) {
	groq := `{"error":{"message":"Request too large for model gpt-oss-120b on tokens per minute (TPM): Limit 8000, Requested 10303, please reduce your message size and try again.","code":"rate_limit_exceeded"}}`
	cases := []struct {
		status   int
		body     string
		category string
	}{
		{413, groq, "model_request"},
		{429, `{"error":{"message":"Too many requests"}}`, "provider_rate_limit"},
		{401, `{"error":{"message":"Invalid API Key"}}`, "provider_access"},
		{404, `{"error":{"message":"The model does not exist","code":"model_not_found"}}`, "model_unavailable"},
		{503, `upstream overloaded`, "provider_transient"},
		{0, `dial tcp 127.0.0.1:1: connect: connection refused`, "provider_transient"},
	}
	for _, c := range cases {
		failure := &WorkerFailure{Reason: WorkerExitedNonZero, ExitCode: 1, Stderr: inference.FailureText(c.status, "", c.body)}
		got := classifyProviderFailure(failure)
		if !got.retryable || got.category != c.category {
			t.Errorf("status %d: got %+v, want retryable %s", c.status, got, c.category)
		}
	}
	if limit := routing.ParseRequestLimit(finalExceptionLine(inference.FailureText(413, "", groq))); limit != 8000 {
		t.Errorf("the size limit must be readable from the final line, got %d", limit)
	}
}

func TestOnlyWorkersRunningTheSharedLoopAreProxied(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "osint.py")
	direct := filepath.Join(dir, "architect.py")
	os.WriteFile(shared, []byte("from worker_sdk import run\nrun('x', 'osint')\n"), 0o600)
	os.WriteFile(direct, []byte("from litellm import completion\n"), 0o600)
	if !proxyAwareWorker(&Worker{Args: []string{shared}}) {
		t.Error("a worker_sdk.run worker honours the proxy")
	}
	if proxyAwareWorker(&Worker{Args: []string{direct}}) || proxyAwareWorker(&Worker{Args: []string{"-test.run=x"}}) {
		t.Error("a worker that calls its provider itself keeps its direct path and credential")
	}
}

// The case that stopped an OSINT node: a route whose per-minute cap is below the request. The proxy
// moves the same request to a route that can take it, and the router learns the small route's
// limit, so later tasks of that size skip it.
func TestATooLargeRequestMovesToARouteThatFitsAndTheLimitIsLearned(t *testing.T) {
	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		io.WriteString(w, `{"error":{"message":"Request too large for model small on tokens per minute (TPM): Limit 8000, Requested 10303, please reduce your message size and try again.","code":"rate_limit_exceeded"}}`)
	}))
	defer small.Close()
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"from\":\"large\"}\n\ndata: [DONE]\n\n")
	}))
	defer large.Close()
	t.Setenv("PROXY_TEST_SMALL_KEY", "small-secret")
	t.Setenv("PROXY_TEST_LARGE_KEY", "large-secret")
	smallRoute := routing.Model{ID: "small/m", Provider: "small", APIBase: small.URL + "/v1", APIKeyEnv: "PROXY_TEST_SMALL_KEY", Enabled: true, Modality: "text", Capability: 60}
	largeRoute := routing.Model{ID: "large/m", Provider: "large", APIBase: large.URL + "/v1", APIKeyEnv: "PROXY_TEST_LARGE_KEY", Enabled: true, Modality: "text", Capability: 40}
	routing.ModelsMutex.Lock()
	old, health, locked := routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys
	routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys = []routing.Model{smallRoute, largeRoute}, nil, nil
	routing.ModelsMutex.Unlock()
	defer func() {
		routing.ModelsMutex.Lock()
		routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys = old, health, locked
		routing.ModelsMutex.Unlock()
	}()

	bus := events.NewBus("inference-proxy-test")
	defer bus.Close()
	log := &logger.Logger{}
	router := &routing.ModelRouter{Logger: log, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}
	d := NewDispatcher(log, bus, nil, router, memory.NewRuntimeState())
	if err := d.StartInferenceProxy(); err != nil {
		t.Fatal(err)
	}
	defer d.CloseInferenceProxy(context.Background())
	script := filepath.Join(t.TempDir(), "osint.py")
	os.WriteFile(script, []byte("from worker_sdk import run\n"), 0o600)
	worker := &Worker{ID: "osint-agent", Args: []string{script}, Logger: log, Bus: bus}
	task := Task{ID: "exec-1|research", Modality: "text", Parameters: map[string]any{}, Memory: map[string]any{}}
	routes := newRetryRoutes()
	routes.policy.MinRequestTokens = 10303
	routes.started(&smallRoute)

	session, environment := d.beginProxiedInference(worker, task, &smallRoute, routes, routing.GetEffortTier("standard"), 2)
	if session == nil {
		t.Fatal("an eligible route and a shared-loop worker must be proxied")
	}
	defer d.Inference.End(session)
	var url, token string
	for _, item := range environment {
		if value, ok := strings.CutPrefix(item, "RETICLE_INFERENCE_URL="); ok {
			url = value
		}
		if value, ok := strings.CutPrefix(item, "RETICLE_INFERENCE_TOKEN="); ok {
			token = value
		}
	}
	request, _ := http.NewRequest(http.MethodPost, url+"/chat/completions", strings.NewReader(`{"model":"openai/x","stream":true,"messages":[]}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), `"from":"large"`) {
		t.Fatalf("got %d %q", response.StatusCode, body)
	}
	if session.Route().ID != "large/m" || session.Failovers() != 1 || session.Exhausted() {
		t.Fatalf("route %s failovers %d exhausted %v", session.Route().ID, session.Failovers(), session.Exhausted())
	}
	if !routes.policy.Excluded[smallRoute.Key()] || !routes.policy.Tried[largeRoute.Key()] {
		t.Fatalf("task routing state: excluded %v tried %v", routes.policy.Excluded, routes.policy.Tried)
	}
	// Another task of the same size, with a fresh policy, now skips the small route outright.
	d.Router.Release(string(task.ID))
	next := d.Router.SelectModelWithPolicy("exec-2|research", "osint-agent", routing.GetEffortTier("standard"), 0.90, "text",
		routing.RoutePolicy{Excluded: map[string]bool{}, Tried: map[string]bool{}, AvoidProviders: map[string]bool{}, MinRequestTokens: 10303})
	if next == nil || next.ID != "large/m" {
		t.Fatalf("a later task of that size was routed to %v", next)
	}
}

func TestNoFailoverBeyondTheNodesAttemptBudget(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "overloaded")
	}))
	defer failing.Close()
	route := routing.Model{ID: "only/m", Provider: "only", APIBase: failing.URL + "/v1", Enabled: true, Modality: "text", Capability: 40}
	routing.ModelsMutex.Lock()
	old := routing.AvailableModels
	routing.AvailableModels = []routing.Model{route, {ID: "other/m", Provider: "other", APIBase: failing.URL + "/v1", Enabled: true, Modality: "text", Capability: 30}}
	routing.ModelsMutex.Unlock()
	defer func() { routing.ModelsMutex.Lock(); routing.AvailableModels = old; routing.ModelsMutex.Unlock() }()
	bus := events.NewBus("inference-budget-test")
	defer bus.Close()
	log := &logger.Logger{}
	router := &routing.ModelRouter{Logger: log, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}
	d := NewDispatcher(log, bus, nil, router, memory.NewRuntimeState())
	if err := d.StartInferenceProxy(); err != nil {
		t.Fatal(err)
	}
	defer d.CloseInferenceProxy(context.Background())
	script := filepath.Join(t.TempDir(), "w.py")
	os.WriteFile(script, []byte("from worker_sdk import run\n"), 0o600)
	routes := newRetryRoutes()
	session, environment := d.beginProxiedInference(&Worker{ID: "w", Args: []string{script}, Logger: log, Bus: bus},
		Task{ID: "exec-3|n", Modality: "text", Parameters: map[string]any{}, Memory: map[string]any{}}, &route, routes, 0, 0)
	if session == nil {
		t.Fatal("expected a session")
	}
	defer d.Inference.End(session)
	token := strings.TrimPrefix(environment[1], "RETICLE_INFERENCE_TOKEN=")
	request, _ := http.NewRequest(http.MethodPost, strings.TrimPrefix(environment[0], "RETICLE_INFERENCE_URL=")+"/chat/completions", strings.NewReader(`{"messages":[]}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || session.Failovers() != 0 || !session.Exhausted() {
		t.Fatalf("with no attempts left the failure is relayed: %d, failovers %d", response.StatusCode, session.Failovers())
	}
}

// The whole attempt loop: the worker process is given the proxy session and no provider key, its
// one request fails on the first route and is answered by the second, and the node completes in a
// single worker start, with the move counted against the node's attempts.
func TestAProxiedAttemptCompletesAfterAFailoverWithoutRestartingTheWorker(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("python is not on PATH")
	}
	var failingHits atomic.Int32
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failingHits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "overloaded")
	}))
	defer failing.Close()
	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"answered by the second route"}}]}`)
	}))
	defer working.Close()
	t.Setenv("PROXY_LOOP_FIRST_KEY", "first-secret")
	t.Setenv("PROXY_LOOP_SECOND_KEY", "second-secret")
	routing.ModelsMutex.Lock()
	old, health, locked := routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys
	routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys = []routing.Model{
		{ID: "first/m", Provider: "first", APIBase: failing.URL + "/v1", APIKeyEnv: "PROXY_LOOP_FIRST_KEY", Enabled: true, Modality: "text", Capability: 60},
		{ID: "second/m", Provider: "second", APIBase: working.URL + "/v1", APIKeyEnv: "PROXY_LOOP_SECOND_KEY", Enabled: true, Modality: "text", Capability: 40},
	}, nil, nil
	routing.ModelsMutex.Unlock()
	defer func() {
		routing.ModelsMutex.Lock()
		routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys = old, health, locked
		routing.ModelsMutex.Unlock()
	}()

	// A stand-in for worker_sdk: one request through the proxy, then the response protocol.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "worker_sdk.py"), []byte(`import json, os, sys, urllib.request
def run(instructions, kind="coding"):
    req = json.load(sys.stdin)
    leaked = [name for name in ("PROXY_LOOP_FIRST_KEY", "PROXY_LOOP_SECOND_KEY") if os.environ.get(name)]
    request = urllib.request.Request(os.environ["RETICLE_INFERENCE_URL"] + "/chat/completions",
        data=json.dumps({"model": "openai/x", "messages": []}).encode(),
        headers={"Authorization": "Bearer " + os.environ["RETICLE_INFERENCE_TOKEN"], "Content-Type": "application/json"})
    reply = json.load(urllib.request.urlopen(request, timeout=20))
    text = reply["choices"][0]["message"]["content"]
    with open(os.path.join(os.path.dirname(__file__), "answer.txt"), "w") as stream:
        stream.write(text + ("; leaked " + ",".join(leaked) if leaked else ""))
    print(json.dumps({"id": req["id"], "result": text}))
`), 0o600)
	script := filepath.Join(dir, "fixture.py")
	os.WriteFile(script, []byte("from worker_sdk import run\nrun('fixture')\n"), 0o600)

	bus := events.NewBus("inference-loop-test")
	defer bus.Close()
	log := &logger.Logger{}
	state := memory.NewRuntimeState()
	memory.NewManager(memory.NewArtifactStore(), state, memory.NewSessionState("inference-loop-test"), bus)
	router := &routing.ModelRouter{Logger: log, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}
	d := NewDispatcher(log, bus, nil, router, state)
	if err := d.StartInferenceProxy(); err != nil {
		t.Fatal(err)
	}
	defer d.CloseInferenceProxy(context.Background())
	d.Start()
	defer d.running.Wait()
	d.RegisterWorker(NewWorker("proxy-fixture", python, []string{script}, nil, log, bus))

	var mu sync.Mutex
	starts := 0
	done := make(chan string, 1)
	bus.Subscribe("AttemptStarted", func(events.RuntimeEvent) { mu.Lock(); starts++; mu.Unlock() })
	bus.Subscribe("WorkerCompleted", func(events.RuntimeEvent) { done <- "completed" })
	bus.Subscribe("WorkerFailed", func(e events.RuntimeEvent) { done <- fmt.Sprint(e.Payload.(map[string]any)["stderr"]) })
	bus.Publish("TaskCreated", "test", Task{ID: "run|node", AgentID: "proxy-fixture", ExecutionID: "run",
		Parameters: map[string]any{"effort": "high"}, Memory: map[string]any{"max_retries": 3, "task_timeout_seconds": 60}})
	select {
	case outcome := <-done:
		if outcome != "completed" {
			t.Fatalf("the node failed: %s", outcome)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the node did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 {
		t.Fatalf("the worker was started %d times; a failed request must not restart it", starts)
	}
	if failingHits.Load() != 1 {
		t.Fatalf("the first route was asked %d times; the test needs it to fail once", failingHits.Load())
	}
	answer, _ := os.ReadFile(filepath.Join(dir, "answer.txt"))
	if string(answer) != "answered by the second route" {
		t.Fatalf("worker saw %q (a leaked key is named here)", answer)
	}
}
