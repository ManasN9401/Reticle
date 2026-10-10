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
	"github.com/reticle/runtime/toolbroker"
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
	ToolBroker   *toolbroker.Broker

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

// StartToolBroker creates the loopback broker used by subsequent attempts.
// Startup is explicit so callers cannot silently run without the configured
// extension boundary when listener creation fails.
func (d *Dispatcher) StartToolBroker() error {
	if d.ToolBroker != nil {
		return nil
	}
	broker, err := toolbroker.New(d.Bus)
	if err != nil {
		return err
	}
	d.ToolBroker = broker
	return nil
}

func (d *Dispatcher) CloseToolBroker(ctx context.Context) error {
	if d.ToolBroker == nil {
		return nil
	}
	return d.ToolBroker.Close(ctx)
}

func (d *Dispatcher) RegisterWorker(w *Worker) {
	d.workersMu.Lock()
	defer d.workersMu.Unlock()
	d.Workers[w.ID] = w
}

// UnregisterWorker prevents new dispatches while already-running attempts keep
// their worker pointer and drain under the normal attempt lifecycle.
func (d *Dispatcher) UnregisterWorker(id WorkerID) {
	d.workersMu.Lock()
	delete(d.Workers, id)
	d.workersMu.Unlock()
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
		if d.ToolBroker != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = d.ToolBroker.Close(ctx)
			cancel()
		}
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
				if d.ToolBroker != nil {
					d.ToolBroker.RevokeExecution(execID)
				}
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
	d.Bus.Subscribe(events.EventType("ExecutionPaused"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]string); ok && d.ToolBroker != nil {
			d.ToolBroker.SetExecutionPaused(payload["execution"], true)
		}
	})
	d.Bus.Subscribe(events.EventType("ExecutionResumed"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]string); ok && d.ToolBroker != nil {
			d.ToolBroker.SetExecutionPaused(payload["execution"], false)
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
		task.MCPServers = append([]string(nil), worker.MCPServers...)
		task.BrokerPolicies = append([]string(nil), worker.BrokerPolicies...)
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
			baseKeys := []string{"user_prompt", "workspace_dir", "max_retries", "allow_native_execution", "ide_context", "prompt_attachments", "prompt_history", "global_effort", "agent_complexity", "task_timeout_seconds", "llm_num_ctx", "llm_max_tokens", "llm_temperature", "llm_first_token_timeout_seconds", "llm_max_iterations", "ollama_keep_alive"}
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
		if awaitsHuman(worker) {
			// Waiting for a person is not work that can overrun: only a kill ends it.
			cancel()
			ctx, cancel = context.WithCancel(context.Background())
		}
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
			lastDisposition := providerFailureDisposition{}
			routes := newRetryRoutes()
			// Once an attempt has left files behind, every later attempt resumes from them.
			resumeAfterFiles := false
			stopReason := "attempt_budget_exhausted"
			originalTokens, hadTokens := t.Memory["llm_max_tokens"]
			var attemptModel *routing.Model
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
					stopReason = "cancelled_or_deadline"
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
						selectedModel := d.Router.SelectModelWithPolicy(string(t.ID), string(w.ID), finalTier, 0.90, t.Modality, routes.policy)
						for selectedModel == nil {
							if d.Router.RouteAvailability(t.Modality, routes.policy).Potential == 0 {
								break
							}
							selectRetries++
							if selectRetries > 24 { // 2 minutes
								break
							}
							d.Logger.Info("No model route is currently eligible. Waiting 5 seconds before retrying routing...", "worker_id", w.ID, "detail", d.Router.ExplainNoRoute(t.Modality))
							select {
							case <-ctx.Done():
								selectRetries = 25
								continue
							case <-time.After(5 * time.Second):
							}
							selectedModel = d.Router.SelectModelWithPolicy(string(t.ID), string(w.ID), finalTier, 0.90, t.Modality, routes.policy)
						}

						if selectedModel == nil {
							stopReason = "no_eligible_route"
							if ctx.Err() != nil {
								stopReason = "cancelled_or_deadline"
							}
							detail := d.Router.ExplainNoRoute(t.Modality) + "; task routing: " + d.Router.RouteAvailability(t.Modality, routes.policy).String()
							if routes.attempts > 0 {
								detail = lastFailure.Stderr + "\n" + detail
							}
							lastFailure = &WorkerFailure{Reason: "no_models_available", ExitCode: 1, Stderr: detail}
							break
						}

						// Parameters survive across bounded attempts. Clear the
						// previous route before installing this one so a fallback
						// from a configured endpoint to a built-in/local model cannot
						// inherit the old API base or credential binding.
						applySelectedModelParameters(t.Parameters, selectedModel)
						attemptModel = selectedModel
					} else if attempt == 1 {
						forcedModel := fmt.Sprint(t.Parameters["llm_model"])
						if configured := d.Router.Model(forcedModel); configured != nil {
							applySelectedModelParameters(t.Parameters, configured)
							attemptModel = configured
						}
						d.Router.TrackForcedModel(string(t.ID), forcedModel)
					}
				}
				if t.Memory == nil {
					t.Memory = make(map[string]any)
				}
				delete(t.Memory, "resumed_after_files")
				if resumeAfterFiles {
					t.Memory["resumed_after_files"] = true
				}
				delete(t.Memory, "llm_max_tokens")
				if hadTokens {
					t.Memory["llm_max_tokens"] = originalTokens
				}
				if attemptModel != nil && routes.limits[attemptModel.Key()] > 0 {
					t.Memory["llm_max_tokens"] = routes.limits[attemptModel.Key()]
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
				attemptEnvironment := []string(nil)
				if d.ToolBroker != nil {
					capabilities := make([]string, len(t.Capabilities))
					for i, capability := range t.Capabilities {
						capabilities[i] = string(capability)
					}
					policies := append([]string(nil), t.BrokerPolicies...)
					for _, serverID := range t.MCPServers {
						policies = append(policies, "mcp.server:"+serverID)
					}
					credentials, brokerErr := d.ToolBroker.BeginAttemptWithPolicies(t.AttemptID, t.ExecutionID, string(t.ID), capabilities, policies)
					if brokerErr != nil {
						d.Bus.Publish("AttemptFinished", "dispatcher", map[string]any{"task_id": string(t.ID), "attempt_id": t.AttemptID, "outcome": "failed", "reason": "tool_broker_start_failed"})
						lastFailure = &WorkerFailure{Reason: WorkerStartFailed, ExitCode: -1, Stderr: brokerErr.Error()}
						stopReason = "tool_broker_start_failed"
						break
					}
					attemptEnvironment = credentials.Environment()
				}

				select {
				case d.slots <- struct{}{}:
				case <-ctx.Done():
					if d.ToolBroker != nil {
						d.ToolBroker.RevokeAttempt(t.AttemptID)
					}
					d.Bus.Publish("AttemptFinished", "dispatcher", map[string]any{"task_id": string(t.ID), "attempt_id": t.AttemptID, "outcome": "failed", "reason": "timeout"})
					d.Bus.Publish("WorkerFailed", "dispatcher", map[string]any{"task_id": string(t.ID), "worker_id": string(w.ID), "attempt_id": t.AttemptID, "reason": "timeout", "stderr": ctx.Err().Error()})
					return
				}
				routes.started(attemptModel)
				_, failure := w.ExecuteWithEnvironment(ctx, t, attemptEnvironment)
				<-d.slots
				if d.ToolBroker != nil {
					d.ToolBroker.RevokeAttempt(t.AttemptID)
				}

				if ctx.Err() != nil {
					d.Bus.Publish("AttemptFinished", "dispatcher", map[string]any{"task_id": string(t.ID), "attempt_id": t.AttemptID, "outcome": "failed", "reason": "cancelled"})
					d.Logger.Info("Worker execution was cancelled (likely killed by user). Aborting retries.", "worker_id", w.ID)
					cleanupExecutionContainers(t.ExecutionID)
					lastFailure = &WorkerFailure{Reason: "killed", ExitCode: -1, Stderr: "Context cancelled"}
					stopReason = "cancelled_or_deadline"
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
					stopReason = "worker_contract_failure"
					break
				}
				// Only route around recognized provider or model-behavior failures with
				// explicit proof that the worker did not start an external effect.
				disposition := classifyProviderFailure(failure)
				lastDisposition = disposition
				if !disposition.retryable {
					lastFailure = failure
					stopReason = "failure_not_retry_safe_or_unclassified"
					break
				}
				if disposition.afterFileEffects && !resumeAfterFiles {
					resumeAfterFiles = true
					d.Logger.Info("Attempt failed after writing workspace files; the next attempt resumes from them on another route.", "worker_id", w.ID, "task_id", t.ID)
				}
				// Failed attempt
				d.Logger.Error("Worker execution failed (attempt)", "worker_id", w.ID, "attempt", attempt, "reason", failure.Reason, "stderr", failure.Stderr)
				lastFailure = failure
				if !forced && routes.failed(attemptModel, disposition, failure, t.Memory["llm_max_tokens"]) {
					d.Router.Release(string(t.ID))
					d.Logger.Info("Provider supplied an output-token bound; next attempt will use the adjusted request on this route.", "task_id", t.ID, "model", attemptModel.ID, "max_tokens", routes.limits[attemptModel.Key()])
					continue
				}

				if d.Router != nil {
					if disposition.penalizeProvider {
						if apiKeyEnv, ok := t.Parameters["api_key"].(string); ok && apiKeyEnv != "" {
							// A provider that says when its quota resets is believed, so an exhausted
							// daily limit is not probed again every 15 minutes.
							resetAt := routing.QuotaResetAt(failure.Stderr, time.Now())
							d.Router.PenalizeProviderForUntil(string(w.ID), apiKeyEnv, disposition.category, disposition.category, resetAt)
						}
						if disposition.penalizeFamily {
							if modelID, ok := t.Parameters["llm_model"].(string); ok {
								if separator := strings.IndexByte(modelID, '/'); separator > 0 {
									d.Router.PenalizeProviderFamilyUntil(modelID[:separator], routing.QuotaResetAt(failure.Stderr, time.Now()))
								}
							}
						}
					} else if disposition.disableModel {
						if modelID, ok := t.Parameters["llm_model"].(string); ok && modelID != "" {
							d.Logger.Info("Model is incompatible with the required chat/tool API; disabling only this model and trying another.", "model", modelID)
							d.Router.PenalizeModel(modelID)
						}
					} else if disposition.category == "model_request" {
						d.Logger.Info("Model rejected this request; lowering its score and trying another.", "worker_id", w.ID)
					}
					if disposition.category == "provider_transient" || disposition.category == "timeout" {
						if attemptModel != nil {
							d.Router.CoolProviderSlot(routing.ProviderSlot(*attemptModel), time.Minute)
						}
					}
					d.Router.UpdateProbability(string(w.ID), string(t.ID), false)
				}

				// Don't retry if we forced the model
				if forced {
					stopReason = "forced_model_failed"
					break
				}
			}

			summary := "Recovery stopped: " + stopReason + " (" + routes.summary()
			if d.Router != nil {
				d.Router.Release(string(t.ID))
				summary += ", " + d.Router.RouteAvailability(t.Modality, routes.policy).String()
			}
			summary += ")"
			d.Logger.Error(summary, "worker_id", w.ID, "task_id", t.ID)
			lastFailure.Stderr += "\n" + summary
			switch lastDisposition.category {
			case "provider_rate_limit", "provider_account_quota", "provider_quota":
				d.Logger.Error("Last provider failure was a quota or rate limit; inspect credential health and the recovery stop reason.", "worker_id", w.ID, "category", lastDisposition.category)
			case "provider_access":
				d.Logger.Error("Provider authentication failed; inspect the selected credential.", "worker_id", w.ID)
			case "provider_transient":
				d.Logger.Error("Last provider failure was connectivity or service availability; the credential itself was not marked rate-limited.", "worker_id", w.ID)
			}
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

func applySelectedModelParameters(parameters map[string]any, model *routing.Model) {
	delete(parameters, "llm_request_model")
	delete(parameters, "llm_api_base")
	delete(parameters, "api_key")
	parameters["llm_model"] = model.ID
	if model.RequestModel != "" {
		parameters["llm_request_model"] = model.RequestModel
	}
	if model.APIBase != "" {
		parameters["llm_api_base"] = model.APIBase
	}
	if model.APIKeyEnv != "" {
		parameters["api_key"] = model.APIKeyEnv
	}
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
	retryable bool
	// afterFileEffects is set when the failed attempt had already written workspace files
	// (and nothing that cannot be replayed), so the next attempt resumes rather than restarts.
	afterFileEffects bool
	penalizeProvider bool
	penalizeFamily   bool
	disableModel     bool
	category         string
}

const (
	retrySafeNoEffects   = "[RETICLE_RETRY_SAFE: NO_EFFECTS]"
	retrySafeFileEffects = "[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]"
)

// classifyProviderFailure decides whether a failed attempt may be tried again on another
// route. NO_EFFECTS proves the worker changed nothing. FILE_EFFECTS_ONLY proves it changed
// only workspace files, which an attempt on another model can safely build on (the tools
// that wrote them never overwrite or repeat an effect), so a provider or request failure
// may resume the node. A stall or exhausted budget with files written is not resumed: that
// is the model's own behaviour and another attempt would redo the same loop at more cost.
func classifyProviderFailure(f *WorkerFailure) providerFailureDisposition {
	disposition := classifyFailureByText(f)
	if !disposition.retryable || f == nil {
		return disposition
	}
	if !strings.Contains(f.Stderr, retrySafeNoEffects) && strings.Contains(f.Stderr, retrySafeFileEffects) {
		if disposition.category == "model_behavior" {
			return providerFailureDisposition{}
		}
		disposition.afterFileEffects = true
	}
	return disposition
}

func classifyFailureByText(f *WorkerFailure) providerFailureDisposition {
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

	// A stalled conversation does not prove that replaying its tools is safe.
	if !strings.Contains(f.Stderr, retrySafeNoEffects) && !strings.Contains(f.Stderr, retrySafeFileEffects) {
		return result
	}
	// A model that cannot get its work accepted, with every file already on disk, is a good
	// case for another model to take over: it only has to verify and finish. Unlike the other
	// stalls this one is cheap (the worker stops it after a few rejections) and resumable.
	if containsAny("agent stalled: completion repeatedly rejected") {
		return providerFailureDisposition{retryable: true, category: "completion_rejected"}
	}
	if containsAny("agent stalled: repeated identical tool requests", "agent stalled: model returned empty responses", "agent iteration budget exhausted", "agent time budget exhausted") {
		return providerFailureDisposition{retryable: true, category: "model_behavior"}
	}

	if (strings.Contains(message, "tool calling") && strings.Contains(message, "not supported")) ||
		strings.Contains(message, "only available on agentic harnesses") ||
		containsAny(
			"not supported by the chat api",
			"does not support the chat api",
			"not supported by the chat completions api",
			"is not a chat model",
			"model does not support chat completions",
		) {
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
	// Compatible endpoints often wrap billing failures in APIError rather than
	// a dedicated quota exception. Match explicit billing evidence, never the
	// generic wrapper: it also covers unknown, potentially non-retryable errors.
	if containsAny(
		"insufficient credits", "insufficient balance", "insufficient_quota",
		"credit balance is too low", "exceeded your current quota",
		"402 payment required", "status code: 402", "error code: 402",
	) {
		return providerFailureDisposition{retryable: true, penalizeProvider: true, category: "provider_quota"}
	}
	if containsAny("invalid api key", "authenticationerror", "401 unauthorized", "status code: 401", "401 client error", "permissiondeniederror", "403 forbidden", "status code: 403", "403 client error", "permission denied") {
		return providerFailureDisposition{retryable: true, penalizeProvider: true, category: "provider_access"}
	}
	if containsAny("requires terms acceptance", "max_tokens must be less than", "request too large", "maximum context length", "badrequesterror", "bad request", "status code: 400", "400 client error", "notfounderror", "404 not found", "status code: 404", "unprocessableentityerror", "422 unprocessable") {
		return providerFailureDisposition{retryable: true, category: "model_request"}
	}
	if containsAny("ratelimiterror", "code\":429", "code\": 429", "429 too many requests", "status code: 429") {
		return providerFailureDisposition{retryable: true, penalizeProvider: true, category: "provider_rate_limit"}
	}
	if containsAny("apiconnectionerror", "serviceunavailableerror", "internalservererror", "500 internal server error", "502 bad gateway", "503 service unavailable", "504 gateway timeout") {
		return providerFailureDisposition{retryable: true, category: "provider_transient"}
	}
	if containsAny("midstreamfallbackerror", "timeout error", "a timeout occurred", "timed out", "produced no stream event") {
		return providerFailureDisposition{retryable: true, category: "timeout"}
	}
	return result
}

func retryableProviderFailure(f *WorkerFailure) bool {
	return classifyProviderFailure(f).retryable
}
