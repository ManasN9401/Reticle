package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/routing"
	"os"
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
	Devices      *DeviceLeaseManager

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
		Devices:      NewDeviceLeaseManager(),
	}
}

func (d *Dispatcher) RegisterWorker(w *Worker) {
	d.workersMu.Lock()
	defer d.workersMu.Unlock()
	d.Workers[w.ID] = w
}

func (d *Dispatcher) HasWorker(execution, worker string) bool {
	d.workersMu.RLock()
	defer d.workersMu.RUnlock()
	if _, ok := d.Workers[WorkerID(execution+"__"+worker)]; ok {
		return true
	}
	_, ok := d.Workers[WorkerID(worker)]
	return ok
}

func (d *Dispatcher) Start() {
	stopOnRuntimeFailure := func(event events.RuntimeEvent) {
		d.activeTasksMu.Lock()
		defer d.activeTasksMu.Unlock()
		for id, tasks := range d.activeTasks {
			d.cancelled[id] = true
			for _, cancel := range tasks {
				cancel()
			}
		}
		d.Logger.Error("Runtime cannot safely persist or route work; active work cancelled. Restart required.", "event", event.Type)
	}
	d.Bus.Subscribe("RuntimeOverloaded", stopOnRuntimeFailure)
	d.Bus.Subscribe("RuntimePersistenceFailed", stopOnRuntimeFailure)
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
		task.Capabilities = append([]Capability(nil), worker.Capabilities...)
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
			baseKeys := []string{"user_prompt", "workspace_dir", "max_retries", "allow_native_execution", "ide_context", "prompt_attachments", "prompt_history", "global_effort", "agent_complexity", "task_timeout_seconds", "llm_num_ctx", "llm_max_tokens", "llm_temperature", "llm_first_token_timeout_seconds", "ollama_keep_alive"}
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
			if hasCapability(w.Capabilities, CapabilityGPUUse) {
				devices, err := ParseDeviceList(os.Getenv("RETICLE_GPU_DEVICES"))
				if err != nil {
					d.Bus.Publish("WorkerFailed", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "reason": "invalid_gpu_devices", "stderr": err.Error()})
					return
				}
				warnings, err := AssessMLProfile(os.Getenv("RETICLE_ML_PROFILE"), DetectMLHostEnvironment(), devices)
				if err != nil {
					d.Bus.Publish("WorkerFailed", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "reason": "invalid_ml_profile", "stderr": err.Error()})
					return
				}
				for _, warning := range warnings {
					d.Bus.Publish("WorkerLog", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "log": "ML environment warning: " + warning})
				}
				release, err := d.Devices.Acquire(ctx, string(t.ID), devices)
				if err != nil {
					d.Bus.Publish("WorkerFailed", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "reason": "gpu_admission_cancelled", "stderr": err.Error()})
					return
				}
				defer release()
			}

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

				t.AttemptID = newAttemptID(t.ExecutionID)
				payload := map[string]any{
					"task_id":    string(t.ID),
					"worker_id":  w.ID,
					"attempt_id": t.AttemptID,
					"number":     attempt,
				}
				if t.Parameters != nil {
					if m, ok := t.Parameters["llm_model"]; ok {
						payload["llm_model"] = m
						payload["model"] = m
					}
				}

				d.Bus.Publish(events.EventType("AttemptStarted"), events.Component("dispatcher"), payload)
				d.Bus.Publish(events.EventType("TaskDispatched"), events.Component("dispatcher"), payload)

				select {
				case d.slots <- struct{}{}:
				case <-ctx.Done():
					d.Bus.Publish("AttemptFinished", "dispatcher", map[string]any{"task_id": string(t.ID), "attempt_id": t.AttemptID, "outcome": "failed", "reason": "timeout"})
					d.Bus.Publish("WorkerFailed", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "attempt_id": t.AttemptID, "reason": "timeout", "stderr": ctx.Err().Error()})
					return
				}
				_, failure := w.Execute(ctx, t)
				<-d.slots

				if ctx.Err() != nil {
					d.Bus.Publish("AttemptFinished", "dispatcher", map[string]any{"task_id": string(t.ID), "attempt_id": t.AttemptID, "outcome": "failed", "reason": "cancelled"})
					d.Logger.Info("Worker execution was cancelled (likely killed by user). Aborting retries.", "worker_id", w.ID)
					cleanupExecutionContainers(t.ExecutionID)
					lastFailure = &WorkerFailure{Reason: "killed", ExitCode: -1, Stderr: "Context cancelled"}
					break
				}

				if failure == nil {
					d.Bus.Publish("AttemptFinished", "dispatcher", map[string]any{"task_id": string(t.ID), "attempt_id": t.AttemptID, "outcome": "succeeded"})
					d.Bus.Publish("WorkerCompleted", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "attempt_id": t.AttemptID})
					if d.Router != nil {
						d.Router.UpdateProbability(string(w.ID), string(t.ID), true)
					}
					return // Success
				}
				d.Bus.Publish("AttemptFinished", "dispatcher", map[string]any{"task_id": string(t.ID), "attempt_id": t.AttemptID, "outcome": "failed", "reason": string(failure.Reason)})

				if failure.Reason == WorkerProtocolError || failure.Reason == WorkerStartFailed || failure.Reason == WorkerInvalidJSON {
					lastFailure = failure
					break
				}
				// Only route around recognized provider or model-behavior failures with
				// explicit proof that the worker did not start an external effect.
				disposition := classifyProviderFailure(failure)
				if !disposition.retryable {
					lastFailure = failure
					break
				}
				// Failed attempt
				d.Logger.Error("Worker execution failed (attempt)", "worker_id", w.ID, "attempt", attempt, "reason", failure.Reason, "stderr", failure.Stderr)
				lastFailure = failure

				if d.Router != nil {
					if disposition.penalizeProvider {
						if apiKeyEnv, ok := t.Parameters["api_key"].(string); ok && apiKeyEnv != "" {
							d.Router.PenalizeProvider(string(w.ID), apiKeyEnv)
						}
						if disposition.penalizeFamily {
							if modelID, ok := t.Parameters["llm_model"].(string); ok {
								if separator := strings.IndexByte(modelID, '/'); separator > 0 {
									d.Router.PenalizeProviderFamily(modelID[:separator])
								}
							}
						}
					} else if disposition.disableModel {
						if modelID, ok := t.Parameters["llm_model"].(string); ok && modelID != "" {
							d.Logger.Info("Model does not support tool calling, disabling globally", "model", modelID)
							d.Router.PenalizeModel(modelID)
						}
					} else if disposition.category == "model_request" {
						d.Logger.Info("Model rejected this request; lowering its score and trying another.", "worker_id", w.ID)
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
				"task_id":    string(t.ID),
				"worker_id":  w.ID,
				"attempt_id": t.AttemptID,
				"reason":     lastFailure.Reason,
				"exit_code":  lastFailure.ExitCode,
				"stderr":     lastFailure.Stderr,
			})
		}(worker, task, isForced)
	})
}

func newAttemptID(execution string) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return execution + "/" + hex.EncodeToString(bytes)
	}
	return fmt.Sprintf("%s/%d", execution, time.Now().UnixNano())
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

type providerFailureDisposition struct {
	retryable        bool
	penalizeProvider bool
	penalizeFamily   bool
	disableModel     bool
	category         string
}

func classifyProviderFailure(f *WorkerFailure) providerFailureDisposition {
	var result providerFailureDisposition
	if f == nil || f.Reason != WorkerExitedNonZero {
		return result
	}
	message := strings.ToLower(f.Stderr)
	containsAny := func(markers ...string) bool {
		for _, marker := range markers {
			if strings.Contains(message, marker) {
				return true
			}
		}
		return false
	}

	// Agent-side stalls (stuck repeating the same tool call, or exhausting
	// its iteration/time budget without ever reaching verified completion)
	// are a property of the stuck conversation, not of any partial effects
	// it produced along the way. A retry reuses the same task/session, so
	// any files the stalled attempt already wrote are still there — the
	// fresh attempt just needs to notice that and finish (e.g. write_file's
	// "File exists" message now tells it to read and verify instead of
	// re-writing). Unlike provider/timeout failures, this is retry-safe
	// regardless of whether NO_EFFECTS was printed, so it's checked before
	// that gate.
	if containsAny("agent stalled: repeated identical tool requests", "agent iteration budget exhausted", "agent time budget exhausted") {
		return providerFailureDisposition{retryable: true, category: "model_behavior"}
	}

	if !strings.Contains(f.Stderr, "[RETICLE_RETRY_SAFE: NO_EFFECTS]") {
		return result
	}

	if (strings.Contains(message, "tool calling") && strings.Contains(message, "not supported")) ||
		strings.Contains(message, "only available on agentic harnesses") {
		return providerFailureDisposition{retryable: true, disableModel: true, category: "model_incompatible"}
	}
	// The architect's own DAG schema validation (architect.py's validate_dag)
	// rejects a structurally invalid plan — e.g. a referenced agent missing
	// is_new — before any work starts. That's the model producing malformed
	// output, not a provider fault, but it's exactly as retry-safe: nothing
	// was written, and another generation attempt may simply comply with the
	// schema this time.
	if containsAny("validation failed:") {
		return providerFailureDisposition{retryable: true, category: "model_behavior"}
	}
	if containsAny("free-models-per-day", "openrouter_free_tier_daily") {
		return providerFailureDisposition{retryable: true, penalizeProvider: true, penalizeFamily: true, category: "provider_account_quota"}
	}
	// Check access failures before generic request wrappers. Some providers and
	// LiteLLM surface a 401/403 as BadRequestError even though every model using
	// the same credential will fail.
	if containsAny("insufficient credits", "invalid api key", "authenticationerror", "401 unauthorized", "status code: 401", "401 client error", "exceeded your current quota", "permissiondeniederror", "403 forbidden", "status code: 403", "403 client error", "permission denied") {
		return providerFailureDisposition{retryable: true, penalizeProvider: true, category: "provider_access"}
	}
	if containsAny("requires terms acceptance", "max_tokens must be less than", "request too large", "maximum context length", "badrequesterror", "bad request", "status code: 400", "400 client error", "notfounderror", "404 not found", "status code: 404", "unprocessableentityerror", "422 unprocessable") {
		return providerFailureDisposition{retryable: true, category: "model_request"}
	}
	if containsAny("ratelimiterror", "code\":429", "code\": 429", "429 too many requests", "status code: 429", "apiconnectionerror", "serviceunavailableerror", "internalservererror", "500 internal server error", "502 bad gateway", "503 service unavailable", "504 gateway timeout") {
		return providerFailureDisposition{retryable: true, penalizeProvider: true, category: "provider_transient"}
	}
	if containsAny("midstreamfallbackerror", "timeout error", "a timeout occurred", "timed out") {
		return providerFailureDisposition{retryable: true, category: "timeout"}
	}
	return result
}

func retryableProviderFailure(f *WorkerFailure) bool {
	return classifyProviderFailure(f).retryable
}
