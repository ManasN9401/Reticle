package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/reticle/runtime/telemetry"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/orchestrator"
	"github.com/reticle/runtime/routing"
)

type ExecutionStatus string

const (
	StatusPending   ExecutionStatus = "PENDING"
	StatusRunning   ExecutionStatus = "RUNNING"
	StatusPaused    ExecutionStatus = "PAUSED"
	StatusCompleted ExecutionStatus = "COMPLETED"
	StatusFailed    ExecutionStatus = "FAILED"
)

type ExecutionMode string

const (
	ModeParallel   ExecutionMode = "parallel"
	ModeSequential ExecutionMode = "sequential"
)

type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Path     string `json:"path"`
}

type WaitlistItem struct {
	ID              string          `json:"id"`
	Prompt          string          `json:"prompt"`
	Status          ExecutionStatus `json:"status"`
	Group           string          `json:"group"`
	Mode            ExecutionMode   `json:"mode"`
	IDEContext      string          `json:"ide_context,omitempty"`
	Effort          string          `json:"effort,omitempty"`
	AgentComplexity int             `json:"agent_complexity,omitempty"`
	Attachments     []Attachment    `json:"attachments,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type WaitlistPayload struct {
	Items          []*WaitlistItem `json:"items"`
	MaxWorkers     int             `json:"maxWorkers"`
	RunningWorkers int             `json:"runningWorkers"`
	LockedKeys     []string        `json:"lockedKeys"`
}

type WaitlistManager struct {
	mu             sync.Mutex
	items          []*WaitlistItem
	nextID         int
	filePath       string
	maxWorkers     int
	graphEngine    *agent.GraphEngine
	orchestrator   *orchestrator.Orchestrator
	registry       *agent.Registry
	dispatcher     *agent.Dispatcher
	envManager     *agent.EnvironmentManager
	compilerDef    *agent.WorkflowDefinition
	globalWorkflow *agent.WorkflowDefinition
	runtimeFailed  bool
}

func NewWaitlistManager(filePath string, maxWorkers int, engine *agent.GraphEngine, orch *orchestrator.Orchestrator, reg *agent.Registry, disp *agent.Dispatcher, env *agent.EnvironmentManager) *WaitlistManager {
	wm := &WaitlistManager{
		filePath:     filePath,
		maxWorkers:   maxWorkers,
		graphEngine:  engine,
		orchestrator: orch,
		registry:     reg,
		dispatcher:   disp,
		envManager:   env,
		items:        make([]*WaitlistItem, 0),
		nextID:       1,
	}
	wm.load()
	orch.Bus.Subscribe("RuntimeOverloaded", func(events.RuntimeEvent) {
		wm.mu.Lock()
		defer wm.mu.Unlock()
		wm.runtimeFailed = true
		for _, item := range wm.items {
			if item.Status == StatusRunning || item.Status == StatusPending || item.Status == StatusPaused {
				item.Status = StatusFailed
			}
		}
		wm.save()
	})
	orch.Bus.Subscribe("RuntimePersistenceFailed", func(event events.RuntimeEvent) {
		wm.mu.Lock()
		defer wm.mu.Unlock()
		wm.runtimeFailed = true
		affected := make(map[string]bool)
		if payload, ok := event.Payload.(map[string]any); ok {
			if ids, ok := payload["execution_ids"].([]string); ok {
				for _, id := range ids {
					affected[strings.TrimPrefix(id, "compile-")] = true
				}
			}
			if id, ok := payload["execution"].(string); ok && id != "" {
				affected[strings.TrimPrefix(id, "compile-")] = true
			}
		}
		for _, item := range wm.items {
			if item.Status != StatusRunning && item.Status != StatusPaused {
				continue
			}
			if len(affected) == 0 || affected[item.ID] {
				item.Status = StatusFailed
			}
		}
		_ = wm.save()
	})

	// Subscribe to events
	orch.Bus.Subscribe(events.EventType("WorkflowCompleted"), func(event events.RuntimeEvent) {
		if payload, ok := event.Payload.(map[string]any); ok {
			execID, ok := payload["execution"].(string)
			if ok {
				if strings.HasPrefix(execID, "compile-") {
					go func() {
						sessionID := strings.TrimPrefix(execID, "compile-")
						if !wm.awaitRunnable(sessionID) {
							return
						}

						// Load any dynamically generated agents first
						agentDir, _ := filepath.Abs(filepath.Join(os.Getenv("RETICLE_ROOT"), ".reticle", "sessions", sessionID, "agents"))
						if err := wm.registry.LoadAgentsForExecution(agentDir, sessionID); err != nil {
							wm.updateStatus(sessionID, StatusFailed)
							wm.Pump()
							return
						}
						newWorkers := wm.registry.BuildWorkersForExecution(sessionID, wm.orchestrator.Logger, wm.orchestrator.Bus, wm.envManager)
						for _, w := range newWorkers {
							wm.dispatcher.RegisterWorker(w)
						}

						wfDir, _ := filepath.Abs(filepath.Join(os.Getenv("RETICLE_ROOT"), ".reticle", "sessions", sessionID, "workflows"))
						loadErr := wm.registry.LoadWorkflowsForExecution(wfDir, sessionID)
						if loadErr == nil {
							wfName := "workflow_" + sessionID
							if wf, exists := wm.registry.GetWorkflow(wfName); exists {
								if !wm.awaitRunnable(sessionID) {
									return
								}
								if err := wm.graphEngine.SubmitWorkflow(wf, sessionID); err != nil {
									wm.updateStatus(sessionID, StatusFailed)
									wm.Pump()
								}
							} else {
								wm.orchestrator.Logger.Error("[Waitlist] Missing workflow ID in registry", "wfName", wfName, "wfDir", wfDir)
								wm.updateStatus(sessionID, StatusFailed)
								wm.Pump()
							}
						} else {
							wm.orchestrator.Logger.Error("[Waitlist] Failed to load workflow", "loadErr", loadErr, "wfDir", wfDir)
							wm.updateStatus(sessionID, StatusFailed)
							wm.Pump() // Compilation failed or missing workflow
						}
					}()
				} else {
					srcDir := filepath.Join(os.Getenv("RETICLE_ROOT"), ".reticle", "sessions", execID)
					dstDir := filepath.Join(filepath.Dir(wm.filePath), execID)
					if err := copySession(srcDir, dstDir); err != nil {
						wm.orchestrator.Logger.Error("Output persistence failed", "execution", execID, "error", err)
						wm.updateStatus(execID, StatusFailed)
					} else {
						wm.updateStatus(execID, StatusCompleted)
					}

					wm.Pump()
				}
			}
		}
	})

	orch.Bus.Subscribe(events.EventType("WorkflowFailed"), func(event events.RuntimeEvent) {
		if payload, ok := event.Payload.(map[string]any); ok {
			execID, ok := payload["execution"].(string)
			if ok {
				if strings.HasPrefix(execID, "compile-") {
					sessionID := strings.TrimPrefix(execID, "compile-")
					wm.updateStatus(sessionID, StatusFailed)
				} else {
					wm.updateStatus(execID, StatusFailed)
				}
				wm.Pump()
			}
		}
	})

	orch.Bus.Subscribe(events.EventType("WaitlistCommand"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]any); ok {
			action, _ := payload["action"].(string)
			switch action {
			case "enqueue":
				prompt, _ := payload["prompt"].(string)
				group, _ := payload["group"].(string)
				modeStr, _ := payload["mode"].(string)
				ideContext, _ := payload["ide_context"].(string)
				effort, _ := payload["effort"].(string)

				var attachments []Attachment
				if atts, ok := payload["attachments"].([]any); ok {
					for _, att := range atts {
						if aMap, ok := att.(map[string]any); ok {
							id, _ := aMap["id"].(string)
							filename, _ := aMap["filename"].(string)
							mimeType, _ := aMap["mime_type"].(string)
							path, _ := aMap["path"].(string)
							attachments = append(attachments, Attachment{
								ID:       id,
								Filename: filename,
								MimeType: mimeType,
								Path:     path,
							})
						}
					}
				}

				mode := ModeParallel
				if modeStr == "sequential" {
					mode = ModeSequential
				}

				agentComplexity := 5
				if c, ok := payload["agent_complexity"].(float64); ok {
					agentComplexity = int(c)
				}

				wm.Enqueue(prompt, group, mode, ideContext, effort, agentComplexity, attachments)
			case "remove":
				id, _ := payload["id"].(string)
				wm.Remove(id)
			case "kill":
				id, _ := payload["id"].(string)
				wm.updateStatus(id, StatusFailed)
				orch.Bus.Publish(events.EventType("ExecutionKilled"), events.Component("waitlist"), map[string]string{"execution": id})
			case "pause":
				id, _ := payload["id"].(string)
				wm.updateStatus(id, StatusPaused)
				orch.Bus.Publish(events.EventType("ExecutionPaused"), events.Component("waitlist"), map[string]string{"execution": id})
			case "resume":
				id, _ := payload["id"].(string)
				wm.updateStatus(id, StatusRunning)
				orch.Bus.Publish(events.EventType("ExecutionResumed"), events.Component("waitlist"), map[string]string{"execution": id})
			case "update_settings":
				if bayesian, ok := payload["use_bayesian_routing"].(bool); ok {
					if wm.dispatcher != nil && wm.dispatcher.Router != nil {
						wm.dispatcher.Router.SetLearning(bayesian)
						wm.orchestrator.Logger.Info("Global settings updated", "use_bayesian_routing", bayesian)
					}
				}
				if numCtx, ok := payload["num_ctx"].(float64); ok {
					wm.orchestrator.Settings.NumCtx = int(numCtx)
					wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{Scope: memory.ScopeGlobal, ScopeID: "global", Key: "llm_num_ctx", Value: int(numCtx), Owner: "forge"})
				}
				if maxTokens, ok := payload["max_tokens"].(float64); ok {
					wm.orchestrator.Settings.MaxTokens = int(maxTokens)
					wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{Scope: memory.ScopeGlobal, ScopeID: "global", Key: "llm_max_tokens", Value: int(maxTokens), Owner: "forge"})
				}
				if temperature, ok := payload["temperature"].(float64); ok {
					wm.orchestrator.Settings.Temperature = temperature
					wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{Scope: memory.ScopeGlobal, ScopeID: "global", Key: "llm_temperature", Value: temperature, Owner: "forge"})
				}
				wm.orchestrator.Logger.Info("LLM settings updated", "num_ctx", wm.orchestrator.Settings.NumCtx, "max_tokens", wm.orchestrator.Settings.MaxTokens, "temperature", wm.orchestrator.Settings.Temperature)
			}
		}
	})

	orch.Bus.Subscribe(events.EventType("WaitlistStateRequested"), func(e events.RuntimeEvent) {
		wm.mu.Lock()
		defer wm.mu.Unlock()
		if wm.orchestrator != nil && wm.orchestrator.Bus != nil {
			runningTotal := 0
			for _, item := range wm.items {
				if item.Status == StatusRunning || item.Status == StatusPaused {
					runningTotal++
				}
			}
			payload := WaitlistPayload{
				Items:          cloneWaitlist(wm.items),
				MaxWorkers:     wm.maxWorkers,
				RunningWorkers: runningTotal,
			}
			wm.orchestrator.Bus.Publish(events.EventType("WaitlistUpdated"), events.Component("waitlist"), payload)
		}
	})

	return wm
}

func (wm *WaitlistManager) Enqueue(prompt string, group string, mode ExecutionMode, ideContext string, effort string, agentComplexity int, attachments []Attachment) {
	if wm.orchestrator != nil && !wm.orchestrator.Bus.Accepting() {
		wm.orchestrator.Logger.Error("Runtime unavailable; restart required")
		return
	}
	wm.mu.Lock()
	if len(wm.items) >= 1000 {
		wm.mu.Unlock()
		fmt.Println("Queue history limit reached; remove completed entries before adding more work")
		return
	}

	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		wm.mu.Unlock()
		panic(err)
	}
	id := "exec-" + hex.EncodeToString(randomID)
	wm.nextID++

	if mode == ModeSequential && group == "" {
		group = "sequential"
	}
	if mode == "" {
		mode = ModeParallel
	}

	// Truncate IDE context to prevent massive payloads crashing the bus
	if len(ideContext) > 5000 {
		ideContext = ideContext[:5000] + "\n\n[WARNING: IDE Context Truncated. Some lines omitted. Use read_file tool to view full file contents if needed!]"
	}

	item := &WaitlistItem{
		ID:              id,
		Prompt:          prompt,
		Status:          StatusPending,
		Group:           group,
		Mode:            mode,
		IDEContext:      ideContext,
		Effort:          effort,
		AgentComplexity: agentComplexity,
		Attachments:     attachments,
		CreatedAt:       time.Now(),
	}
	wm.items = append(wm.items, item)
	if err := wm.save(); err != nil {
		wm.items = wm.items[:len(wm.items)-1]
		wm.mu.Unlock()
		return
	}
	wm.mu.Unlock()

	fmt.Printf("\n[Waitlist] Queued [%s] in group '%s'\n> ", id, group)

	wm.Pump()
}

func (wm *WaitlistManager) Remove(id string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for i, item := range wm.items {
		if item.ID == id {
			if item.Status == StatusRunning || item.Status == StatusPaused {
				return
			}
			wm.items = append(wm.items[:i], wm.items[i+1:]...)
			wm.save()
			break
		}
	}
}

func (wm *WaitlistManager) updateStatus(id string, status ExecutionStatus) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for _, item := range wm.items {
		if item.ID == id {
			if item.Status == StatusCompleted || item.Status == StatusFailed {
				return
			}
			item.Status = status
			wm.save()
			break
		}
	}
}

func (wm *WaitlistManager) SetGlobalWorkflow(wf *agent.WorkflowDefinition) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.globalWorkflow = wf
}

func (wm *WaitlistManager) SetCompilerDef(def *agent.WorkflowDefinition) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.compilerDef = def
}

func (wm *WaitlistManager) load() {
	data, err := os.ReadFile(wm.filePath)
	if err == nil {
		if err := json.Unmarshal(data, &wm.items); err != nil {
			wm.items = []*WaitlistItem{}
			wm.orchestrator.Logger.Error("Queue could not be decoded; refusing to overwrite it", "error", err)
			// Preserve corrupt evidence; a new queue uses a separate file.
			wm.filePath += fmt.Sprintf(".recovered-%d", time.Now().UnixNano())
			return
		}
		dirty := false
		valid := make([]*WaitlistItem, 0, len(wm.items))
		for _, item := range wm.items {
			if item == nil {
				dirty = true
				continue
			}
			if strings.ContainsAny(item.ID, "\\/:|.") || !strings.HasPrefix(item.ID, "exec-") {
				dirty = true
				continue
			}
			valid = append(valid, item)
			var num int
			if _, err := fmt.Sscanf(item.ID, "exec-%d", &num); err == nil && num >= wm.nextID {
				wm.nextID = num + 1
			}
			// Reset tasks that were running during previous crash
			if item.Status == StatusRunning || item.Status == StatusPaused {
				item.Status = StatusFailed
				dirty = true
			}
		}
		wm.items = valid
		if dirty {
			wm.save()
		}
	}
}

func (wm *WaitlistManager) save() (err error) {
	defer func() {
		if err != nil && wm.orchestrator != nil {
			wm.orchestrator.Logger.Error("Queue persistence failed", "error", err)
		}
	}()
	data, err := json.MarshalIndent(wm.items, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(wm.filePath), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(wm.filePath), ".waitlist-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), wm.filePath); err != nil {
		return err
	}

	var snapshot []*WaitlistItem
	json.Unmarshal(data, &snapshot)

	runningTotal := 0
	for _, item := range wm.items {
		if item.Status == StatusRunning || item.Status == StatusPaused {
			runningTotal++
		}
	}

	payload := WaitlistPayload{
		Items:          snapshot,
		MaxWorkers:     wm.maxWorkers,
		RunningWorkers: runningTotal,
		LockedKeys:     routing.LockedKeys,
	}

	// Broadcast waitlist to UI
	if wm.orchestrator != nil && wm.orchestrator.Bus != nil {
		wm.orchestrator.Bus.Publish(events.EventType("WaitlistUpdated"), events.Component("waitlist"), payload)
	}
	return nil
}

func (wm *WaitlistManager) Pump() {
	wm.mu.Lock()
	if wm.runtimeFailed {
		wm.mu.Unlock()
		return
	}

	// Count running total and running per group
	runningTotal := 0
	runningGroups := make(map[string]int)

	for _, item := range wm.items {
		if item.Status == StatusRunning || item.Status == StatusPaused {
			runningTotal++
			if item.Group != "" {
				runningGroups[item.Group]++
			}
		}
	}

queueLoop:
	for _, item := range wm.items {
		if runningTotal >= wm.maxWorkers {
			break
		}

		if item.Status == StatusPending {
			// Check grouping constraints
			if item.Group != "" && item.Mode == ModeSequential {
				if count, exists := runningGroups[item.Group]; exists && count > 0 {
					// Sequential group already has a running task, skip this one
					continue
				}
			}

			// Safe to launch
			item.Status = StatusRunning
			runningTotal++
			if item.Group != "" {
				runningGroups[item.Group]++
			}

			if err := wm.save(); err != nil {
				item.Status = StatusPending
				runningTotal--
				if item.Group != "" {
					runningGroups[item.Group]--
				}
				break
			}

			// Build prompt history and find previous execution for file inheritance
			var promptHistory string
			var prevExecID string
			historyLines := []string{}
			for i := 0; i < len(wm.items); i++ {
				prev := wm.items[i]
				if prev.ID == item.ID {
					break
				}
				if item.Group != "" && prev.Group == item.Group && (prev.Status == StatusCompleted || prev.Status == StatusFailed) {
					historyLines = append(historyLines, fmt.Sprintf("- Iteration %s: %s", prev.ID, prev.Prompt))
					prevExecID = prev.ID
				}
			}
			if len(historyLines) > 0 {
				promptHistory = strings.Join(historyLines, "\n")
			}

			// Inherit checked project files only within the selected group.
			if prevExecID != "" {
				base := filepath.Join(os.Getenv("RETICLE_ROOT"), ".reticle", "sessions")
				if err := copySession(filepath.Join(base, prevExecID), filepath.Join(base, item.ID)); err != nil {
					wm.orchestrator.Logger.Error("Session inheritance failed", "error", err)
					item.Status = StatusFailed
					runningTotal--
					runningGroups[item.Group]--
					wm.save()
					continue queueLoop
				}
			}

			// Required launch context is committed as one acknowledged batch before
			// workflow submission. A compiler never starts with partial controls.
			memoryEntries := make([]memory.MemoryEntry, 0, 12)
			addMemory := func(key string, value any) {
				for _, scopeID := range []string{item.ID, "compile-" + item.ID} {
					memoryEntries = append(memoryEntries, memory.MemoryEntry{
						Scope: memory.ScopeExecution, ScopeID: scopeID, Key: key, Value: value, Owner: "waitlist",
					})
				}
			}
			addMemory("user_prompt", item.Prompt)

			if item.IDEContext != "" {
				addMemory("ide_context", item.IDEContext)
			}

			if promptHistory != "" {
				addMemory("prompt_history", promptHistory)
			}

			// Inject workspace_dir for this execution (and its compiler phase)
			isolatedWorkspacePath, err := filepath.Abs(filepath.Join(os.Getenv("RETICLE_ROOT"), ".reticle", "sessions", item.ID))
			if err != nil {
				wm.orchestrator.Logger.Error("Workspace path resolution failed", "execution", item.ID, "error", err)
				item.Status = StatusFailed
				runningTotal--
				if item.Group != "" {
					runningGroups[item.Group]--
				}
				_ = wm.save()
				continue queueLoop
			}
			if err := os.MkdirAll(isolatedWorkspacePath, 0755); err != nil {
				wm.orchestrator.Logger.Error("Workspace creation failed", "execution", item.ID, "path", isolatedWorkspacePath, "error", err)
				item.Status = StatusFailed
				runningTotal--
				if item.Group != "" {
					runningGroups[item.Group]--
				}
				_ = wm.save()
				continue queueLoop
			}

			// Process Attachments
			if len(item.Attachments) > 0 {
				var promptAttachments []map[string]any
				for _, att := range item.Attachments {
					dstPath, err := moveAttachment(filepath.Join(filepath.Dir(filepath.Dir(wm.envManager.BaseDir)), ".reticle", "waitlist_staging"), isolatedWorkspacePath, att)
					if err != nil {
						item.Status = StatusFailed
						wm.save()
						runningTotal--
						if item.Group != "" {
							runningGroups[item.Group]--
						}
						continue queueLoop
					}

					if strings.HasPrefix(att.MimeType, "image/") {
						b, err := os.ReadFile(dstPath)
						var imageURL map[string]string
						if err == nil {
							encoded := base64.StdEncoding.EncodeToString(b)
							imageURL = map[string]string{"url": fmt.Sprintf("data:%s;base64,%s", att.MimeType, encoded)}
						}

						promptAttachments = append(promptAttachments, map[string]any{
							"type":      "image_url",
							"mime_type": att.MimeType,
							"image_url": imageURL,
							"filename":  att.Filename,
							"path":      dstPath,
						})
					} else {
						promptAttachments = append(promptAttachments, map[string]any{
							"type":      "file",
							"mime_type": att.MimeType,
							"filename":  att.Filename,
							"path":      dstPath,
						})
					}
				}

				if len(promptAttachments) > 0 {
					attJSON, _ := json.Marshal(promptAttachments)
					addMemory("prompt_attachments", string(attJSON))
				}
			}

			addMemory("workspace_dir", isolatedWorkspacePath)

			if item.Effort != "" && item.Effort != "auto" {
				addMemory("global_effort", item.Effort)
			}

			if wm.orchestrator.Memory == nil {
				wm.orchestrator.Logger.Error("Execution memory manager is unavailable", "execution", item.ID)
				item.Status = StatusFailed
				runningTotal--
				if item.Group != "" {
					runningGroups[item.Group]--
				}
				_ = wm.save()
				continue queueLoop
			}
			if err := wm.orchestrator.Memory.WriteBatch(memoryEntries); err != nil {
				wm.orchestrator.Logger.Error("Execution context persistence failed", "execution", item.ID, "error", err)
				item.Status = StatusFailed
				runningTotal--
				if item.Group != "" {
					runningGroups[item.Group]--
				}
				_ = wm.save()
				continue queueLoop
			}

			// Capture immutable launch data after the required memory batch commits.
			definition, executionID := wm.globalWorkflow, item.ID
			if definition == nil {
				definition = wm.compilerDef
				executionID = "compile-" + item.ID
			}
			go func(id, graphID string, def *agent.WorkflowDefinition) {
				if err := wm.graphEngine.SubmitWorkflow(def, graphID); err != nil {
					wm.orchestrator.Logger.Error("Workflow submission failed", "execution", id, "error", err)
					wm.updateStatus(id, StatusFailed)
					wm.Pump()
				}
			}(item.ID, executionID, definition)

		}
	}
	wm.mu.Unlock()
}

func copySession(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "agents", "workflows", "workers", ".tmpenv", "waitlist.json":
			continue
		}
		if err = copyDir(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in session output")
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if existing, err := os.Lstat(target); err == nil && existing.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink output target")
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular session output")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		syncErr := out.Sync()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	})
}

func moveAttachment(staging, workspace string, att Attachment) (string, error) {
	if filepath.Base(att.ID) != att.ID || filepath.Base(att.Filename) != att.Filename {
		return "", fmt.Errorf("invalid attachment name")
	}
	src, err := telemetry.SafePath(staging, att.ID)
	if err != nil {
		return "", err
	}
	dst, err := telemetry.SafePath(workspace, att.Filename)
	if err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("invalid upload")
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(dst)
		return "", fmt.Errorf("attachment copy failed")
	}
	in.Close()
	if err = os.Remove(src); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

func cloneWaitlist(items []*WaitlistItem) []*WaitlistItem {
	data, _ := json.Marshal(items)
	var result []*WaitlistItem
	json.Unmarshal(data, &result)
	return result
}

// Compilation provisioning runs off the event bus. Pause and kill remain responsive.
func (wm *WaitlistManager) awaitRunnable(id string) bool {
	for {
		wm.mu.Lock()
		status := StatusFailed
		for _, item := range wm.items {
			if item.ID == id {
				status = item.Status
				break
			}
		}
		wm.mu.Unlock()
		if status == StatusRunning {
			return true
		}
		if status != StatusPaused {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
