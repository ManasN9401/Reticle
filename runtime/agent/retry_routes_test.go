package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/routing"
)

func TestOutputLimitRepairIsBounded(t *testing.T) {
	for _, test := range []struct {
		message string
		want    int
	}{
		{"BadRequestError: max tokens must be less than or equal to 8192", 8192},
		{"BadRequestError: max_tokens must be less than 8192", 8191},
		{"BadRequestError: maximum context length is 8192", 0},
		{"BadRequestError: max_tokens must be less than 0", 0},
		{"BadRequestError: max_tokens must be less than 99999999999999999999999999", 0},
		{"max_tokens must be less than 8192\nBadRequestError: some unrelated failure", 0},
	} {
		t.Run(test.message, func(t *testing.T) {
			h := newRetryRoutes()
			model := &routing.Model{ID: "fixture/model", Provider: "fixture"}
			failure := &WorkerFailure{Reason: WorkerExitedNonZero, Stderr: "[RETICLE_RETRY_SAFE: NO_EFFECTS]\n" + test.message}
			disposition := classifyProviderFailure(failure)
			got := h.failed(model, disposition, failure, 12288)
			if got != (test.want > 0) || h.limits[model.Key()] != test.want {
				t.Fatalf("unexpected repair: %#v", h)
			}
			if h.failed(model, disposition, failure, 12288) {
				t.Fatal("repeated adjustment bypassed budget")
			}
			if !h.policy.Excluded[model.Key()] {
				t.Fatal("rejected request was not excluded")
			}
		})
	}
}

// Spawn actual workers through the dispatcher: two mixed-catalog failures must
// leave that provider, correct the next model's explicit limit and commit output.
func TestDispatcherRecoveryScenarios(t *testing.T) {
	for _, scenario := range []struct {
		mode                 string
		budget, wantAttempts int
		wantModels           []string
		stop                 string
	}{
		{"route-repair", 5, 4, []string{"bad/a", "bad/b", "good/a", "good/a"}, ""},
		{"route-limit-fallback", 5, 3, []string{"bad/a", "bad/a", "bad/b"}, ""},
		{"route-balance", 15, 4, []string{"bad/a", "bad/b", "good/a", "spare/a"}, ""},
		{"route-balance", 3, 3, []string{"bad/a", "bad/b", "good/a"}, "attempt_budget_exhausted"},
		{"route-reject", 2, 2, []string{"bad/a", "bad/b"}, "attempt_budget_exhausted"},
		{"route-reject", 15, 4, []string{"bad/a", "bad/b", "good/a", "bad/c"}, "no_eligible_route"},
		{"effect-stall", 5, 1, []string{"bad/a"}, "failure_not_retry_safe_or_unclassified"},
		// File-only effects may resume on another route, including after a stall or an exhausted
		// budget (effect-stall above, with no proof, stays terminal).
		{"resume-files", 5, 2, []string{"bad/a", "good/a"}, ""},
		{"resume-stall", 5, 2, []string{"bad/a", "bad/b"}, ""},
		{"resume-rejected", 5, 2, []string{"bad/a", "bad/b"}, ""},
		{"fresh-attempt", 5, 2, []string{"bad/a", "bad/a"}, ""},
	} {
		t.Run(fmt.Sprintf("%s-%d", scenario.mode, scenario.budget), func(t *testing.T) {
			t.Setenv("RETICLE_ROOT", t.TempDir())
			routing.ModelsMutex.Lock()
			old, health, locked := routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys
			routing.KeyHealthStates = nil
			routing.LockedKeys = nil
			routing.AvailableModels = []routing.Model{
				{ID: "bad/a", Provider: "bad", APIKeyEnv: "RETRY_TEST_BAD_KEY", Enabled: true, Modality: "text", Capability: 10},
				{ID: "bad/b", Provider: "bad", APIKeyEnv: "RETRY_TEST_BAD_KEY", Enabled: true, Modality: "text", Capability: 10},
				{ID: "bad/c", Provider: "bad", APIKeyEnv: "RETRY_TEST_BAD_KEY", Enabled: true, Modality: "text", Capability: 10},
				{ID: "good/a", Provider: "good", APIKeyEnv: "RETRY_TEST_GOOD_KEY", Enabled: true, Modality: "text", Capability: 40},
			}
			if scenario.mode == "route-balance" {
				routing.AvailableModels = append(routing.AvailableModels, routing.Model{ID: "spare/a", Provider: "spare", APIKeyEnv: "RETRY_TEST_SPARE_KEY", Enabled: true, Modality: "text", Capability: 40})
			}
			routing.ModelsMutex.Unlock()
			defer func() {
				routing.ModelsMutex.Lock()
				routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys = old, health, locked
				routing.ModelsMutex.Unlock()
			}()
			bus := events.NewBus("retry-test")
			defer bus.Close()
			log := &logger.Logger{}
			state := memory.NewRuntimeState()
			memory.NewManager(memory.NewArtifactStore(), state, memory.NewSessionState("retry-test"), bus)
			router := &routing.ModelRouter{Logger: log, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}, UseBayesianRouting: true}
			d := NewDispatcher(log, bus, nil, router, state)
			d.Start()
			defer d.running.Wait()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			d.RegisterWorker(NewWorker("fixture", exe, []string{"-test.run=^TestWorkerChild$"}, []string{"RETICLE_TEST_CHILD=" + scenario.mode}, log, bus))
			var models []string
			done := make(chan string, 1)
			bus.Subscribe("AttemptStarted", func(e events.RuntimeEvent) {
				models = append(models, fmt.Sprint(e.Payload.(map[string]any)["llm_model"]))
			})
			bus.Subscribe("WorkerCompleted", func(events.RuntimeEvent) { done <- "" })
			bus.Subscribe("WorkerFailed", func(e events.RuntimeEvent) { done <- fmt.Sprint(e.Payload.(map[string]any)["stderr"]) })
			bus.Publish("TaskCreated", "test", Task{ID: "run|node", AgentID: "fixture", ExecutionID: "run", Parameters: map[string]any{"effort": "minimal"}, Memory: map[string]any{"max_retries": scenario.budget, "llm_max_tokens": 12288, "task_timeout_seconds": 10}})
			select {
			case message := <-done:
				if scenario.stop == "" && message != "" {
					t.Fatal(message)
				}
				if scenario.stop != "" && !strings.Contains(message, scenario.stop) {
					t.Fatalf("missing stop reason: %s", message)
				}
				if len(models) != scenario.wantAttempts || strings.Join(models, ",") != strings.Join(scenario.wantModels, ",") {
					t.Fatalf("unexpected attempts: %v", models)
				}
				if scenario.stop == "attempt_budget_exhausted" && !strings.Contains(message, "untried_eligible_providers=1") {
					t.Fatalf("untried provider hidden: %s", message)
				}
				if scenario.mode == "route-balance" {
					states, lockedKeys := routing.KeyHealthSnapshot()
					if len(lockedKeys) != 1 || lockedKeys[0] != "RETRY_TEST_GOOD_KEY" {
						t.Fatalf("billing penalty escaped its credential: %v", lockedKeys)
					}
					for _, state := range states {
						if state.Key == "RETRY_TEST_GOOD_KEY" && (state.Status != routing.KeyQuotaExhausted || !state.RetryAt.After(time.Now())) {
							t.Fatalf("billing failure misclassified: %#v", state)
						}
					}
				}
			case <-time.After(15 * time.Second):
				t.Fatal("dispatcher failed to terminate")
			}
		})
	}
}

// runTasksOnSharedRouter runs the given tasks one after another through one dispatcher and
// router, as the nodes of one workflow do, and returns the models each task was attempted on.
func runTasksOnSharedRouter(t *testing.T, mode string, tasks []Task) ([][]string, map[string]string) {
	t.Helper()
	routing.ModelsMutex.Lock()
	old, health, locked := routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys
	routing.KeyHealthStates, routing.LockedKeys = nil, nil
	routing.AvailableModels = []routing.Model{
		{ID: "bad/a", Provider: "bad", APIKeyEnv: "RETRY_TEST_BAD_KEY", Enabled: true, Modality: "text", Capability: 40},
		{ID: "good/a", Provider: "good", APIKeyEnv: "RETRY_TEST_GOOD_KEY", Enabled: true, Modality: "text", Capability: 40},
	}
	routing.ModelsMutex.Unlock()
	defer func() {
		routing.ModelsMutex.Lock()
		routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys = old, health, locked
		routing.ModelsMutex.Unlock()
	}()
	bus := events.NewBus("shared-router-test")
	defer bus.Close()
	log := &logger.Logger{}
	state := memory.NewRuntimeState()
	memory.NewManager(memory.NewArtifactStore(), state, memory.NewSessionState("shared-router-test"), bus)
	router := &routing.ModelRouter{Logger: log, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}, UseBayesianRouting: true}
	d := NewDispatcher(log, bus, nil, router, state)
	d.Start()
	defer d.running.Wait()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	d.RegisterWorker(NewWorker("fixture", exe, []string{"-test.run=^TestWorkerChild$"}, []string{"RETICLE_TEST_CHILD=" + mode}, log, bus))
	var mu sync.Mutex
	attempts := map[string][]string{}
	finished := make(chan string, len(tasks))
	failures := map[string]string{}
	bus.Subscribe("AttemptStarted", func(e events.RuntimeEvent) {
		payload := e.Payload.(map[string]any)
		mu.Lock()
		task := fmt.Sprint(payload["task_id"])
		attempts[task] = append(attempts[task], fmt.Sprint(payload["llm_model"]))
		mu.Unlock()
	})
	bus.Subscribe("WorkerCompleted", func(e events.RuntimeEvent) { finished <- fmt.Sprint(e.Payload.(map[string]any)["task_id"]) })
	bus.Subscribe("WorkerFailed", func(e events.RuntimeEvent) {
		payload := e.Payload.(map[string]any)
		mu.Lock()
		failures[fmt.Sprint(payload["task_id"])] = fmt.Sprint(payload["stderr"])
		mu.Unlock()
		finished <- fmt.Sprint(payload["task_id"])
	})
	var result [][]string
	for _, task := range tasks {
		bus.Publish("TaskCreated", "test", task)
		select {
		case <-finished:
		case <-time.After(15 * time.Second):
			t.Fatalf("task %s did not finish", task.ID)
		}
		mu.Lock()
		result = append(result, append([]string(nil), attempts[string(task.ID)]...))
		mu.Unlock()
	}
	return result, failures
}

func sharedRouterTask(id string, parameters map[string]any) Task {
	parameters["effort"] = "minimal"
	return Task{ID: TaskID(id), AgentID: "fixture", ExecutionID: "run", Parameters: parameters, Memory: map[string]any{"max_retries": 3, "task_timeout_seconds": 10}}
}

// Observed: all six nodes of one workflow spent their first two attempts on the same two
// models that 404 for the account, because only the task that found a dead route excluded it.
func TestDeadRouteIsNotRetriedByLaterTasks(t *testing.T) {
	attempts, _ := runTasksOnSharedRouter(t, "route-missing", []Task{
		sharedRouterTask("run|first", map[string]any{}),
		sharedRouterTask("run|second", map[string]any{}),
	})
	if strings.Join(attempts[0], ",") != "bad/a,good/a" {
		t.Fatalf("first task should find the dead route and move on: %v", attempts[0])
	}
	if strings.Join(attempts[1], ",") != "good/a" {
		t.Fatalf("second task retried a route already known to be dead: %v", attempts[1])
	}
}

// Observed: every node of a workflow tried the same Groq route and was told the minute's window was
// used up ("Limit 8000, Used 6424, Requested 6635"). Only that route waits, only for the time the
// provider said, and the credential's other models are not cooled with it.
func TestRateWindowCoolsOnlyThatRouteForLaterTasks(t *testing.T) {
	attempts, _ := runTasksOnSharedRouter(t, "route-window", []Task{
		sharedRouterTask("run|first", map[string]any{}),
		sharedRouterTask("run|second", map[string]any{}),
	})
	if strings.Join(attempts[0], ",") != "bad/a,good/a" {
		t.Fatalf("first task: %v", attempts[0])
	}
	if strings.Join(attempts[1], ",") != "good/a" {
		t.Fatalf("a route inside its cooling window was tried again: %v", attempts[1])
	}
}

// Observed: three Groq credentials, three attempts per node, all "Limit 8000, Requested 11399".
func TestLearnedRequestLimitKeepsLargeTasksOffTheRoute(t *testing.T) {
	large := strings.Repeat("x", 40000) // about 11k tokens at 3.5 characters each
	attempts, _ := runTasksOnSharedRouter(t, "route-too-large", []Task{
		sharedRouterTask("run|small", map[string]any{"system_prompt": "short"}),
		sharedRouterTask("run|large", map[string]any{"system_prompt": large}),
	})
	if len(attempts[0]) == 0 || attempts[0][0] != "bad/a" {
		t.Fatalf("a small task must still be allowed to learn the limit: %v", attempts[0])
	}
	if strings.Join(attempts[1], ",") != "good/a" {
		t.Fatalf("a task that cannot fit used the route anyway: %v", attempts[1])
	}
}

func attemptLimitTask(id string) Task {
	task := sharedRouterTask(id, map[string]any{})
	task.Memory["attempt_timeout_seconds"] = 2
	return task
}

// Observed: one hung attempt held a node for 100 minutes of its 2 hour deadline. The attempt now
// has its own limit; with only file effects reported, the node moves to another route.
func TestAttemptLimitMovesOnWhenOnlyFilesWereChanged(t *testing.T) {
	attempts, failures := runTasksOnSharedRouter(t, "hang-after-files", []Task{attemptLimitTask("run|node")})
	if strings.Join(attempts[0], ",") != "bad/a,good/a" {
		t.Fatalf("the node did not move on after the attempt limit: %v (%v)", attempts[0], failures)
	}
	if message, failed := failures["run|node"]; failed {
		t.Fatalf("the node failed: %s", message)
	}
}

// An attempt that had run a command cannot be replayed, so the limit ends the node, and says why.
func TestAttemptLimitWithUnreplayableEffectsIsTerminal(t *testing.T) {
	attempts, failures := runTasksOnSharedRouter(t, "hang-after-command", []Task{attemptLimitTask("run|node")})
	if len(attempts[0]) != 1 {
		t.Fatalf("an unreplayable attempt was started again: %v", attempts[0])
	}
	message := failures["run|node"]
	for _, want := range []string{"Attempt time limit of 2 seconds", "failure_not_retry_safe_or_unclassified"} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing %q: %s", want, message)
		}
	}
}

func TestProgressProofUsesTheLastProgressLine(t *testing.T) {
	for _, test := range []struct {
		name, stderr, want string
	}{
		{"nothing reported", "worker started\n", ""},
		{"no changes", effectsNone + "\n", retrySafeNoEffects},
		{"files", effectsNone + "\nlog\n" + effectsFiles + "\n", retrySafeFileEffects},
		{"a command after files", effectsNone + "\n" + effectsFiles + "\n" + effectsUnsafe + "\n", ""},
	} {
		if got := progressProof(test.stderr); got != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
}

// One attempt that never returns used to end the node as "Context cancelled", indistinguishable
// from a user's kill. The deadline is shared by every attempt, so the report must say it was the
// deadline, which attempt held it, and must not blame an earlier attempt's rate limit.
func TestHungAttemptIsReportedAsTheTaskDeadline(t *testing.T) {
	routing.ModelsMutex.Lock()
	old, health, locked := routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys
	routing.KeyHealthStates, routing.LockedKeys = nil, nil
	routing.AvailableModels = []routing.Model{{ID: "slow/a", Provider: "slow", APIKeyEnv: "RETRY_TEST_SLOW_KEY", Enabled: true, Modality: "text", Capability: 40}}
	routing.ModelsMutex.Unlock()
	defer func() {
		routing.ModelsMutex.Lock()
		routing.AvailableModels, routing.KeyHealthStates, routing.LockedKeys = old, health, locked
		routing.ModelsMutex.Unlock()
	}()
	bus := events.NewBus("deadline-test")
	defer bus.Close()
	log := &logger.Logger{}
	state := memory.NewRuntimeState()
	memory.NewManager(memory.NewArtifactStore(), state, memory.NewSessionState("deadline-test"), bus)
	router := &routing.ModelRouter{Logger: log, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}, UseBayesianRouting: true}
	d := NewDispatcher(log, bus, nil, router, state)
	d.Start()
	defer d.running.Wait()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	d.RegisterWorker(NewWorker("fixture", exe, []string{"-test.run=^TestWorkerChild$"}, []string{"RETICLE_TEST_CHILD=hang"}, log, bus))
	failed := make(chan string, 1)
	bus.Subscribe("WorkerFailed", func(e events.RuntimeEvent) { failed <- fmt.Sprint(e.Payload.(map[string]any)["stderr"]) })
	bus.Publish("TaskCreated", "test", Task{ID: "run|node", AgentID: "fixture", ExecutionID: "run", Parameters: map[string]any{"effort": "minimal"}, Memory: map[string]any{"max_retries": 3, "task_timeout_seconds": 1}})
	select {
	case message := <-failed:
		for _, want := range []string{"Task deadline of 1 seconds", "attempt 1 (slow/a)", "task_deadline_exceeded"} {
			if !strings.Contains(message, want) {
				t.Fatalf("missing %q: %s", want, message)
			}
		}
		if strings.Contains(message, "Context cancelled") {
			t.Fatalf("a deadline was reported as a cancellation: %s", message)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("dispatcher did not stop at the deadline")
	}
}

// Text earlier in stderr (a quoted prompt, a skill, a tool's output) must not decide how the
// failure is handled: only the exception the traceback ends with is the provider's answer.
func TestModelUnavailableIsJudgedByTheFinalException(t *testing.T) {
	quoted := "[RETICLE_RETRY_SAFE: NO_EFFECTS]\nprompt: handle 'Not found for account' and model_not_found\n"
	got := classifyProviderFailure(&WorkerFailure{Reason: WorkerExitedNonZero, Stderr: quoted + "litellm.RateLimitError: GroqException - Request too large"})
	if got.category == "model_unavailable" {
		t.Fatalf("quoted text decided the classification: %#v", got)
	}
	got = classifyProviderFailure(&WorkerFailure{Reason: WorkerExitedNonZero, Stderr: quoted + "litellm.NotFoundError: model_not_found"})
	if got.category != "model_unavailable" || !got.retryable {
		t.Fatalf("final exception was ignored: %#v", got)
	}
}

func TestFileEffectProofResumesOnlyProviderFailures(t *testing.T) {
	const file = "[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]\n"
	for _, test := range []struct {
		name, stderr string
		retryable    bool
		resume       bool
	}{
		{"provider rate limit", file + "RateLimitError: Request too large", true, true},
		{"gateway timeout", file + "litellm.Timeout: Timeout Error: OpenAIException - Error code: 504", true, true},
		{"account quota", file + "RateLimitError: free-models-per-day", true, true},
		{"rejected request", file + "BadRequestError: invalid message", true, true},
		{"stall after files", file + "Agent stalled: repeated identical tool requests", true, true},
		{"empty replies after files", file + "Agent stalled: model returned empty responses", true, true},
		{"text-only replies after files", file + "RuntimeError: Agent stalled: model replied with text but no tool call for 4 consecutive iterations", true, true},
		{"completion rejected repeatedly after files", file + "Agent stalled: completion repeatedly rejected (6 times with no progress)", true, true},
		{"budget exhausted after files", file + "Agent iteration budget exhausted without verified completion", true, true},
		{"budget exhausted with no marker means an effect that cannot be replayed", "Agent iteration budget exhausted without verified completion", false, false},
		{"no marker means an effect that cannot be replayed", "RateLimitError: boom", false, false},
		{"no effects", "[RETICLE_RETRY_SAFE: NO_EFFECTS]\nRateLimitError: boom", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := classifyProviderFailure(&WorkerFailure{Reason: WorkerExitedNonZero, Stderr: test.stderr})
			if got.retryable != test.retryable || got.afterFileEffects != test.resume {
				t.Fatalf("retryable=%v resume=%v, want %v/%v", got.retryable, got.afterFileEffects, test.retryable, test.resume)
			}
		})
	}
}

// The approval checkpoint waits for a person, so the task deadline other workers run under must not end it.
func TestHumanCheckpointOutlivesTheTaskDeadline(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RETICLE_ROOT", root)
	bus := events.NewBus("hitl-test")
	defer bus.Close()
	log := &logger.Logger{}
	state := memory.NewRuntimeState()
	memory.NewManager(memory.NewArtifactStore(), state, memory.NewSessionState("hitl-test"), bus)
	d := NewDispatcher(log, bus, nil, nil, state)
	d.Start()
	defer d.running.Wait()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	d.RegisterWorker(NewWorker("hitl-agent", exe, nil, nil, log, bus))
	done := make(chan string, 1)
	bus.Subscribe("WorkerCompleted", func(events.RuntimeEvent) { done <- "completed" })
	bus.Subscribe("WorkerFailed", func(e events.RuntimeEvent) { done <- fmt.Sprint(e.Payload.(map[string]any)["stderr"]) })
	decide := make(chan string, 1)
	bus.Subscribe("WorkerLog", func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if line, _ := payload["log"].(string); strings.HasPrefix(line, "[TOOL] Checkpoint file created at: ") {
				decide <- strings.TrimPrefix(line, "[TOOL] Checkpoint file created at: ")
			}
		}
	})
	// A one second task deadline; the person takes three seconds to decide.
	bus.Publish("TaskCreated", "test", Task{ID: "run|approve", AgentID: "hitl-agent", ExecutionID: "run", Memory: map[string]any{"task_timeout_seconds": 1}})
	select {
	case path := <-decide:
		time.Sleep(3 * time.Second)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		data, _ := json.Marshal(map[string]string{"hash": hex.EncodeToString(sum[:]), "decision": "APPROVED"})
		if err := os.WriteFile(path+".decision.json", data, 0600); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the checkpoint never started waiting")
	}
	select {
	case message := <-done:
		if message != "completed" {
			t.Fatalf("the checkpoint was ended before the person decided: %s", message)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the approved checkpoint never completed")
	}
}
