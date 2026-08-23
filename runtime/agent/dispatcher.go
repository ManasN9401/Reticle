package agent

import (
	"strings"
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
	"github.com/hyperparallel/runtime/memory"
	"github.com/hyperparallel/runtime/routing"
)

// Dispatcher listens for TaskReady events and schedules them to the appropriate Worker.
type Dispatcher struct {
	Logger       *logger.Logger
	Bus          *events.Bus
	Workers      map[WorkerID]*Worker
	Instructions *InstructionStore
	Router       *routing.ModelRouter
	RuntimeState *memory.RuntimeState
}

func NewDispatcher(l *logger.Logger, b *events.Bus, is *InstructionStore, r *routing.ModelRouter, rs *memory.RuntimeState) *Dispatcher {
	return &Dispatcher{
		Logger:       l,
		Bus:          b,
		Workers:      make(map[WorkerID]*Worker),
		Instructions: is,
		Router:       r,
		RuntimeState: rs,
	}
}

func (d *Dispatcher) RegisterWorker(w *Worker) {
	d.Workers[w.ID] = w
}

func (d *Dispatcher) Start() {
	d.Bus.Subscribe(events.EventType("TaskCreated"), func(e events.RuntimeEvent) {
		task, ok := e.Payload.(Task)
		if !ok {
			d.Logger.Error("Dispatcher received invalid TaskCreated payload")
			return
		}

		workerID := WorkerID(task.AgentID)
		worker, ok := d.Workers[workerID]
		if !ok {
			d.Logger.Error("Dispatcher failed to trigger worker", "worker_id", workerID, "error", "worker not found")
			return
		}

		// Inject instructions dynamically
		if d.Instructions != nil {
			task.Instructions = d.Instructions.GetForTask(task.AgentID, task.Workflow)
		}
		if task.Parameters == nil {
			task.Parameters = make(map[string]any)
		}
		_, isForced := task.Parameters["llm_model"]

		// Inject Implicit Base Context (every agent gets these regardless of YAML)
		if d.RuntimeState != nil {
			if task.Memory == nil {
				task.Memory = make(map[string]any)
			}
			baseKeys := []string{"user_prompt", "workspace_dir"}
			for _, key := range baseKeys {
				// Execution-scoped first (per-prompt), then global fallback
				if val, found := d.RuntimeState.Get(memory.ScopeExecution, task.ExecutionID, key); found {
					task.Memory[key] = val.Value
				} else if val, found := d.RuntimeState.Get(memory.ScopeGlobal, "global", key); found {
					task.Memory[key] = val.Value
				}
			}
		}

		// Inject Required Shared Memory (agent-specific keys from YAML)
		if d.RuntimeState != nil && len(worker.RequiredMemory) > 0 {
			for _, key := range worker.RequiredMemory {
				if _, alreadySet := task.Memory[key]; alreadySet {
					continue // Don't overwrite base context
				}
				// Hierarchical resolution: Agent -> Execution -> Workflow -> Global
				if val, found := d.RuntimeState.Get(memory.ScopeAgent, string(workerID), key); found {
					task.Memory[key] = val.Value
				} else if val, found := d.RuntimeState.Get(memory.ScopeExecution, task.ExecutionID, key); found {
					task.Memory[key] = val.Value
				} else if val, found := d.RuntimeState.Get(memory.ScopeWorkflow, task.Workflow, key); found {
					task.Memory[key] = val.Value
				} else if val, found := d.RuntimeState.Get(memory.ScopeGlobal, "global", key); found {
					task.Memory[key] = val.Value
				}
			}
		}

		// Asynchronously invoke the worker directly
		go func(w *Worker, t Task, forced bool) {
			maxRetries := 15
			if v, ok := t.Memory["max_retries"].(int); ok {
				maxRetries = v
			}
			var lastFailure *WorkerFailure

			for attempt := 1; attempt <= maxRetries; attempt++ {
				// Extract effort - Apply global UI effort as a modifier to the task's base effort
				taskEffortStr := "standard"
				if e, ok := t.Parameters["effort"].(string); ok && e != "" {
					taskEffortStr = e
				}
				globalModifier := 0
				if globalEffort, ok := t.Memory["global_effort"].(string); ok && globalEffort != "" {
					globalModifier = routing.GetTierDelta(globalEffort)
				}

				baseTier := routing.GetEffortTier(taskEffortStr)
				finalTier := baseTier + globalModifier
				effortStr := routing.TierToEffortString(finalTier)

				// Dynamically select model if not forced
				if d.Router != nil {
					if !forced {
						selectedModel := d.Router.SelectModel(string(t.ID), string(w.ID), effortStr, 0.90)
						for selectedModel == nil {
							d.Logger.Warn("All models are currently locked or penalized. Waiting 5 seconds before retrying routing...", "worker_id", w.ID)
							time.Sleep(5 * time.Second)
							selectedModel = d.Router.SelectModel(string(t.ID), string(w.ID), effortStr, 0.90)
						}
						
						t.Parameters["llm_model"] = selectedModel.ID
						if selectedModel.APIKeyEnv != "" {
							t.Parameters["api_key"] = selectedModel.APIKeyEnv
						}
					} else if attempt == 1 {
						d.Router.TrackForcedModel(string(t.ID), t.Parameters["llm_model"].(string))
					}
				}

				payload := map[string]any{
					"task_id":   string(t.ID),
					"worker_id": w.ID,
				}
				if t.Parameters != nil {
					if m, ok := t.Parameters["llm_model"]; ok {
						payload["llm_model"] = m
					}
				}

				d.Bus.Publish(events.EventType("TaskDispatched"), events.Component("dispatcher"), payload)
				
				_, failure := w.Execute(t)
				if failure == nil {
					if d.Router != nil {
						d.Router.UpdateProbability(string(w.ID), string(t.ID), true)
					}
					return // Success
				}

				// Failed attempt
				d.Logger.Error("Worker execution failed (attempt)", "worker_id", w.ID, "attempt", attempt, "reason", failure.Reason, "stderr", failure.Stderr)
				lastFailure = failure

				if d.Router != nil {
					stderrLower := strings.ToLower(failure.Stderr)
					if strings.Contains(failure.Stderr, "Insufficient credits") || strings.Contains(failure.Stderr, "invalid api key") || strings.Contains(failure.Stderr, "APIConnectionError") || strings.Contains(stderrLower, "exceeded your current quota") || strings.Contains(stderrLower, "code\":429") || (strings.Contains(stderrLower, "ratelimiterror") && !strings.Contains(stderrLower, "request too large")) {
						if apiKeyEnv, ok := t.Parameters["api_key"].(string); ok && apiKeyEnv != "" {
							d.Router.PenalizeProvider(string(w.ID), apiKeyEnv)
						}
					} else if strings.Contains(stderrLower, "requires terms acceptance") ||
						strings.Contains(stderrLower, "max_tokens must be less than") ||
						strings.Contains(stderrLower, "request too large") ||
						strings.Contains(stderrLower, "maximum context length") {
						if modelID, ok := t.Parameters["llm_model"].(string); ok && modelID != "" {
							d.Router.PenalizeModel(modelID)
						}
					}
					d.Router.UpdateProbability(string(w.ID), string(t.ID), false)
				}
				
				// Don't retry if we forced the model
				if forced {
					break
				}
			}

			// All attempts exhausted
			d.Logger.Error("Worker execution finally failed", "worker_id", w.ID, "reason", lastFailure.Reason, "stderr", lastFailure.Stderr)
			d.Bus.Publish(events.EventType("WorkerFailed"), events.Component("dispatcher"), map[string]any{
				"task_id":   string(t.ID),
				"worker_id": w.ID,
				"reason":    lastFailure.Reason,
				"exit_code": lastFailure.ExitCode,
				"stderr":    lastFailure.Stderr,
			})
		}(worker, task, isForced)
	})
}
