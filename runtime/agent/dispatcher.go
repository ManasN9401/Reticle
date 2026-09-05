package agent

import (
	"context"
	"strings"
	"sync"
	"time"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/routing"
)

// Dispatcher listens for TaskReady events and schedules them to the appropriate Worker.
type Dispatcher struct {
	Logger       *logger.Logger
	Bus          *events.Bus
	Workers      map[WorkerID]*Worker
	Instructions *InstructionStore
	Router       *routing.ModelRouter
	RuntimeState *memory.RuntimeState
	
	activeTasksMu sync.Mutex
	activeTasks   map[string]map[TaskID]context.CancelFunc // ExecutionID -> TaskID -> CancelFunc
}

func NewDispatcher(l *logger.Logger, b *events.Bus, is *InstructionStore, r *routing.ModelRouter, rs *memory.RuntimeState) *Dispatcher {
	return &Dispatcher{
		Logger:       l,
		Bus:          b,
		Workers:      make(map[WorkerID]*Worker),
		Instructions: is,
		Router:       r,
		RuntimeState: rs,
		activeTasks:  make(map[string]map[TaskID]context.CancelFunc),
	}
}

func (d *Dispatcher) RegisterWorker(w *Worker) {
	d.Workers[w.ID] = w
}

func (d *Dispatcher) Start() {
	d.Bus.Subscribe(events.EventType("ExecutionKilled"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]string); ok {
			if execID, ok := payload["execution"]; ok {
				d.activeTasksMu.Lock()
				if tasks, exists := d.activeTasks[execID]; exists {
					for _, cancel := range tasks {
						cancel()
					}
					delete(d.activeTasks, execID)
				}
				d.activeTasksMu.Unlock()
			}
		}
	})

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

				// Dynamically select model if not forced
				if d.Router != nil {
					if !forced {
						selectedModel := d.Router.SelectModel(string(t.ID), string(w.ID), finalTier, 0.90, t.Modality)
						for selectedModel == nil {
							d.Logger.Info("All models are currently locked or penalized. Waiting 5 seconds before retrying routing...", "worker_id", w.ID)
							time.Sleep(5 * time.Second)
							selectedModel = d.Router.SelectModel(string(t.ID), string(w.ID), finalTier, 0.90, t.Modality)
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
				
				ctx, cancel := context.WithCancel(context.Background())
				
				d.activeTasksMu.Lock()
				if d.activeTasks[t.ExecutionID] == nil {
					d.activeTasks[t.ExecutionID] = make(map[TaskID]context.CancelFunc)
				}
				d.activeTasks[t.ExecutionID][t.ID] = cancel
				d.activeTasksMu.Unlock()
				
				_, failure := w.Execute(ctx, t)
				
				d.activeTasksMu.Lock()
				if d.activeTasks[t.ExecutionID] != nil {
					delete(d.activeTasks[t.ExecutionID], t.ID)
				}
				d.activeTasksMu.Unlock()
				cancel()
				
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
					if strings.Contains(failure.Stderr, "Insufficient credits") || strings.Contains(failure.Stderr, "invalid api key") || strings.Contains(failure.Stderr, "APIConnectionError") || strings.Contains(stderrLower, "exceeded your current quota") || strings.Contains(stderrLower, "code\":429") || strings.Contains(stderrLower, "code\": 429") || strings.Contains(stderrLower, "429 too many requests") || (strings.Contains(stderrLower, "ratelimiterror") && !strings.Contains(stderrLower, "request too large")) || strings.Contains(stderrLower, "403") || strings.Contains(stderrLower, "forbidden") || strings.Contains(stderrLower, "permission denied") {
						if apiKeyEnv, ok := t.Parameters["api_key"].(string); ok && apiKeyEnv != "" {
							d.Router.PenalizeProvider(string(w.ID), apiKeyEnv)
						}
					} else if strings.Contains(stderrLower, "requires terms acceptance") ||
						strings.Contains(stderrLower, "max_tokens must be less than") ||
						strings.Contains(stderrLower, "request too large") ||
						strings.Contains(stderrLower, "maximum context length") {
						d.Logger.Error("Fatal task error: context length exceeded. Aborting retries to prevent API spam.", "worker_id", w.ID)
						break // Do not retry, and do NOT globally penalize the model for a payload size issue
					} else if strings.Contains(stderrLower, "tool calling") && strings.Contains(stderrLower, "not supported") {
						if modelID, ok := t.Parameters["llm_model"].(string); ok && modelID != "" {
							d.Logger.Info("Model does not support tool calling, disabling globally", "model", modelID)
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
			d.Logger.Error("Worker execution aborted after exhausting all retries. The task could not complete successfully.", "worker_id", w.ID)
			d.Logger.Error("TROUBLESHOOTING: If the logs show repeated 429 Quota Exceeded errors, your API keys are out of credits or being throttled. Please check your provider billing dashboards or add new API keys to your environment.", "worker_id", w.ID)
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
