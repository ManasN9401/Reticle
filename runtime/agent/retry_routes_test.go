package agent

import (
	"fmt"
	"os"
	"strings"
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
