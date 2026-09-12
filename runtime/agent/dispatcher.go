package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/routing"
	"strings"
	"sync"
	"time"
)

// Dispatcher listens for TaskReady events and schedules them to the appropriate Worker.
type Dispatcher struct {
	running      sync.WaitGroup
	slots        chan struct{}
	Logger       *logger.Logger
	Bus          *events.Bus
	Workers      map[WorkerID]*Worker
	Instructions *InstructionStore
	Router       *routing.ModelRouter
	RuntimeState *memory.RuntimeState

	workersMu     sync.RWMutex
	cancelled     map[string]bool
	activeTasksMu sync.Mutex
	activeTasks   map[string]map[TaskID]context.CancelFunc // ExecutionID -> TaskID -> CancelFunc
}

func NewDispatcher(l *logger.Logger, b *events.Bus, is *InstructionStore, r *routing.ModelRouter, rs *memory.RuntimeState) *Dispatcher {
	return &Dispatcher{
		slots:        make(chan struct{}, 16),
		Logger:       l,
		Bus:          b,
		Workers:      make(map[WorkerID]*Worker),
		Instructions: is,
		Router:       r,
		RuntimeState: rs,
		activeTasks:  make(map[string]map[TaskID]context.CancelFunc),
		cancelled:    make(map[string]bool),
	}
}

func (d *Dispatcher) RegisterWorker(w *Worker) {
	d.workersMu.Lock()
	defer d.workersMu.Unlock()
	d.Workers[w.ID] = w
}

func (d *Dispatcher) Start() {
	d.Bus.Subscribe("RuntimeOverloaded", func(events.RuntimeEvent) {
		d.activeTasksMu.Lock()
		defer d.activeTasksMu.Unlock()
		for id, tasks := range d.activeTasks {
			d.cancelled[id] = true
			for _, cancel := range tasks {
				cancel()
			}
		}
		d.Logger.Error("Event queue capacity exceeded; active work cancelled. Restart required.")
	})
	d.Bus.Subscribe("RuntimeShutdown", func(events.RuntimeEvent) {
		d.activeTasksMu.Lock()
		for id, tasks := range d.activeTasks {
			d.cancelled[id] = true
			for _, cancel := range tasks {
				cancel()
			}
		}
		d.activeTasksMu.Unlock()
		done := make(chan struct{})
		go func() { d.running.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	d.Bus.Subscribe(events.EventType("ExecutionKilled"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]string); ok {
			if execID, ok := payload["execution"]; ok {
				d.activeTasksMu.Lock()
				d.cancelled[execID] = true
				d.cancelled["compile-"+execID] = true
				for _, cancel := range d.activeTasks["compile-"+execID] {
					cancel()
				}
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
		d.workersMu.RLock()
		worker, ok := d.Workers[WorkerID(task.ExecutionID+"__"+string(workerID))]
		if !ok {
			worker, ok = d.Workers[workerID]
		}
		d.workersMu.RUnlock()
		if !ok {
			d.Bus.Publish("WorkerFailed", "dispatcher", map[string]any{"task_id": string(task.ID), "worker_id": string(workerID), "reason": "worker_not_found"})
			return
		}

		task.Parameters = copyParameters(task.Parameters)
		task.Memory = copyParameters(task.Memory)
		task.MemoryMetadata = make(map[string]MemoryReference)
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
			baseKeys := []string{"user_prompt", "workspace_dir", "max_retries", "allow_native_execution", "ide_context", "prompt_attachments", "prompt_history", "global_effort", "agent_complexity", "task_timeout_seconds"}
			for _, key := range baseKeys {
				// Execution-scoped first (per-prompt), then global fallback
				if val, found := d.RuntimeState.Get(memory.ScopeExecution, task.ExecutionID, key); found {
					task.Memory[key] = val.Value
					task.MemoryMetadata[key] = MemoryReference{Scope: val.Scope, ScopeID: val.ScopeID, Version: val.Version}
				} else if val, found := d.RuntimeState.Get(memory.ScopeGlobal, "global", key); found {
					task.Memory[key] = val.Value
					task.MemoryMetadata[key] = MemoryReference{Scope: val.Scope, ScopeID: val.ScopeID, Version: val.Version}
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
					task.MemoryMetadata[key] = MemoryReference{Scope: val.Scope, ScopeID: val.ScopeID, Version: val.Version}
				} else if val, found := d.RuntimeState.Get(memory.ScopeExecution, task.ExecutionID, key); found {
					task.Memory[key] = val.Value
					task.MemoryMetadata[key] = MemoryReference{Scope: val.Scope, ScopeID: val.ScopeID, Version: val.Version}
				} else if val, found := d.RuntimeState.Get(memory.ScopeWorkflow, task.Workflow, key); found {
					task.Memory[key] = val.Value
					task.MemoryMetadata[key] = MemoryReference{Scope: val.Scope, ScopeID: val.ScopeID, Version: val.Version}
				} else if val, found := d.RuntimeState.Get(memory.ScopeGlobal, "global", key); found {
					task.Memory[key] = val.Value
					task.MemoryMetadata[key] = MemoryReference{Scope: val.Scope, ScopeID: val.ScopeID, Version: val.Version}
				}
			}
		}

		timeout := 7200
		if value, ok := task.Memory["task_timeout_seconds"]; ok {
			fmt.Sscan(fmt.Sprint(value), &timeout)
		}
		if timeout < 1 {
			timeout = 1
		}
		if timeout > 7200 {
			timeout = 7200
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		d.activeTasksMu.Lock()
		if d.cancelled[task.ExecutionID] || d.activeTasks[task.ExecutionID][task.ID] != nil {
			d.activeTasksMu.Unlock()
			cancel()
			return
		}
		if d.activeTasks[task.ExecutionID] == nil {
			d.activeTasks[task.ExecutionID] = make(map[TaskID]context.CancelFunc)
		}
		d.activeTasks[task.ExecutionID][task.ID] = cancel
		d.activeTasksMu.Unlock()
		d.running.Add(1)
		// Asynchronously invoke the worker directly
		go func(w *Worker, t Task, forced bool) {
			defer d.running.Done()
			defer cancel()
			defer func() {
				d.activeTasksMu.Lock()
				delete(d.activeTasks[t.ExecutionID], t.ID)
				d.activeTasksMu.Unlock()
				if d.Router != nil {
					d.Router.Release(string(t.ID))
				}
			}()
			maxRetries := 3
			if value, ok := t.Memory["max_retries"]; ok {
				fmt.Sscan(fmt.Sprint(value), &maxRetries)
			}
			if maxRetries < 1 {
				maxRetries = 1
			}
			if maxRetries > 15 {
				maxRetries = 15
			}
			lastFailure := &WorkerFailure{Reason: "cancelled", ExitCode: -1, Stderr: "Execution cancelled"}

			for attempt := 1; attempt <= maxRetries; attempt++ {
				if ctx.Err() != nil {
					break
				}
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
				if d.Router != nil && !deterministicWorker(w.ID) {
					if !forced {
						selectRetries := 0
						selectedModel := d.Router.SelectModel(string(t.ID), string(w.ID), finalTier, 0.90, t.Modality)
						for selectedModel == nil {
							selectRetries++
							if selectRetries > 24 { // 2 minutes
								break
							}
							d.Logger.Info("All models are currently locked or penalized. Waiting 5 seconds before retrying routing...", "worker_id", w.ID)
							select {
							case <-ctx.Done():
								selectRetries = 25
								continue
							case <-time.After(5 * time.Second):
							}
							selectedModel = d.Router.SelectModel(string(t.ID), string(w.ID), finalTier, 0.90, t.Modality)
						}

						if selectedModel == nil {
							d.Logger.Error("No models became available after 2 minutes of waiting. Aborting task.", "worker_id", w.ID)
							lastFailure = &WorkerFailure{Reason: "no_models_available", ExitCode: 1, Stderr: "No models available for selection. All models might be permanently disabled or rate-limited for too long."}
							break
						}

						t.Parameters["llm_model"] = selectedModel.ID
						if selectedModel.APIKeyEnv != "" {
							t.Parameters["api_key"] = selectedModel.APIKeyEnv
						}
					} else if attempt == 1 {
						d.Router.TrackForcedModel(string(t.ID), fmt.Sprint(t.Parameters["llm_model"]))
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

				select {
				case d.slots <- struct{}{}:
				case <-ctx.Done():
					d.Bus.Publish("WorkerFailed", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "reason": "timeout", "stderr": ctx.Err().Error()})
					return
				}
				_, failure := w.Execute(ctx, t)
				<-d.slots

				if ctx.Err() != nil {
					d.Logger.Info("Worker execution was cancelled (likely killed by user). Aborting retries.", "worker_id", w.ID)
					cleanupExecutionContainers(t.ExecutionID)
					lastFailure = &WorkerFailure{Reason: "killed", ExitCode: -1, Stderr: "Context cancelled"}
					break
				}

				if failure == nil {
					if d.Router != nil {
						d.Router.UpdateProbability(string(w.ID), string(t.ID), true)
					}
					return // Success
				}

				if failure.Reason == WorkerProtocolError || failure.Reason == WorkerStartFailed || failure.Reason == WorkerInvalidJSON {
					lastFailure = failure
					break
				}
				// Only retry transient provider failures; arbitrary command failures may have side effects.
				if !retryableProviderFailure(failure) {
					lastFailure = failure
					break
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
						d.Logger.Info("Context length exceeded for this model, penalizing it for this task and trying another.", "worker_id", w.ID)
						// Fall through to UpdateProbability(..., false) so it picks a different model instead of aborting the whole task
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

func copyParameters(p map[string]any) map[string]any {
	out := make(map[string]any)
	data, err := json.Marshal(p)
	if err == nil {
		json.Unmarshal(data, &out)
	}
	if out == nil {
		out = make(map[string]any)
	}
	return out
}
func deterministicWorker(id WorkerID) bool {
	switch id {
	case "scaffolder-agent", "writer-agent", "coder-agent", "hermes-coder-agent", "quant-agent", "osint-agent", "browser-agent", "hitl-agent", "graphify-agent":
		return true
	}
	return false
}

func retryableProviderFailure(f *WorkerFailure) bool {
	if f.Reason != WorkerExitedNonZero {
		return false
	}
	if !strings.Contains(f.Stderr, "[RETICLE_RETRY_SAFE: NO_EFFECTS]") {
		return false
	}
	message := strings.ToLower(f.Stderr)
	for _, marker := range []string{"ratelimiterror", "apiconnectionerror", "serviceunavailableerror", "429 too many requests", "exceeded your current quota"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
