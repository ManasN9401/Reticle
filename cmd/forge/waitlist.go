package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hyperparallel/runtime/agent"
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/memory"
	"github.com/hyperparallel/runtime/orchestrator"
)

type ExecutionStatus string

const (
	StatusPending   ExecutionStatus = "PENDING"
	StatusRunning   ExecutionStatus = "RUNNING"
	StatusCompleted ExecutionStatus = "COMPLETED"
	StatusFailed    ExecutionStatus = "FAILED"
)

type ExecutionMode string

const (
	ModeParallel   ExecutionMode = "parallel"
	ModeSequential ExecutionMode = "sequential"
)

type WaitlistItem struct {
	ID         string          `json:"id"`
	Prompt     string          `json:"prompt"`
	Status     ExecutionStatus `json:"status"`
	Group      string          `json:"group"`
	Mode       ExecutionMode   `json:"mode"`
	IDEContext string          `json:"ide_context,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

type WaitlistManager struct {
	mu           sync.Mutex
	items        []*WaitlistItem
	nextID       int
	filePath     string
	maxWorkers   int
	graphEngine  *agent.GraphEngine
	orchestrator   *orchestrator.Orchestrator
	registry       *agent.Registry
	dispatcher     *agent.Dispatcher
	envManager     *agent.EnvironmentManager
	compilerDef    *agent.WorkflowDefinition
	globalWorkflow *agent.WorkflowDefinition
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
	
	// Subscribe to events
	orch.Bus.Subscribe(events.EventType("WorkflowCompleted"), func(event events.RuntimeEvent) {
		if payload, ok := event.Payload.(map[string]any); ok {
			execID, ok := payload["execution"].(string)
			if ok {
				if strings.HasPrefix(execID, "compile-") {
					sessionID := strings.TrimPrefix(execID, "compile-")
					
					// Load any dynamically generated agents first
					agentDir, _ := filepath.Abs(filepath.Join("../../", ".hyperparallel", "sessions", sessionID, "agents"))
					wm.registry.LoadAgents(agentDir)
					newWorkers := wm.registry.BuildWorkers(wm.orchestrator.Logger, wm.orchestrator.Bus, wm.envManager)
					for _, w := range newWorkers {
						wm.dispatcher.RegisterWorker(w)
					}
					
					wfDir, _ := filepath.Abs(filepath.Join("../../", ".hyperparallel", "sessions", sessionID, "workflows"))
					loadErr := wm.registry.LoadWorkflows(wfDir)
					if loadErr == nil {
						wfName := "workflow_" + sessionID
						if wf, exists := wm.registry.Workflows[wfName]; exists {
							wm.graphEngine.SubmitWorkflow(wf, sessionID)
						} else {
							wm.orchestrator.Logger.Error("[Waitlist] Missing workflow ID in registry", "wfName", wfName, "wfDir", wfDir)
							wm.updateStatus(sessionID, StatusFailed) 
						}
					} else {
						wm.orchestrator.Logger.Error("[Waitlist] Failed to load workflow", "loadErr", loadErr, "wfDir", wfDir)
						wm.updateStatus(sessionID, StatusFailed) // Compilation failed or missing workflow
					}
				} else {
					wm.updateStatus(execID, StatusCompleted)
					
					// Dump artifacts to output dir per RFC-031
					srcDir, _ := filepath.Abs(filepath.Join("../../", ".hyperparallel", "sessions", execID, "src"))
					dstDir, _ := filepath.Abs(filepath.Join("../../", "outputs", execID))
					if _, err := os.Stat(srcDir); err == nil {
						os.MkdirAll(dstDir, 0755)
						if err := copyDir(srcDir, dstDir); err != nil {
							wm.orchestrator.Logger.Error("[Waitlist] Failed to copy outputs", "execID", execID, "error", err)
						} else {
							wm.orchestrator.Logger.Info("[Waitlist] Successfully dumped artifacts to outputs/", "execID", execID)
						}
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
				mode := ModeParallel
				if modeStr == "sequential" {
					mode = ModeSequential
				}
				wm.Enqueue(prompt, group, mode, ideContext)
			case "remove":
				id, _ := payload["id"].(string)
				wm.Remove(id)
			}
		}
	})

	orch.Bus.Subscribe(events.EventType("WaitlistStateRequested"), func(e events.RuntimeEvent) {
		wm.mu.Lock()
		defer wm.mu.Unlock()
		if wm.orchestrator != nil && wm.orchestrator.Bus != nil {
			wm.orchestrator.Bus.Publish(events.EventType("WaitlistUpdated"), events.Component("waitlist"), wm.items)
		}
	})
	
	return wm
}

func (wm *WaitlistManager) Enqueue(prompt string, group string, mode ExecutionMode, ideContext string) {
	wm.mu.Lock()
	
	id := fmt.Sprintf("exec-%03d", wm.nextID)
	wm.nextID++
	
	if mode == "" {
		mode = ModeParallel
	}
	
	// Truncate IDE context to prevent massive payloads crashing the bus
	if len(ideContext) > 5000 {
		ideContext = ideContext[:5000] + "\n\n[WARNING: IDE Context Truncated. Some lines omitted. Use read_file tool to view full file contents if needed!]"
	}
	
	item := &WaitlistItem{
		ID:         id,
		Prompt:     prompt,
		Status:     StatusPending,
		Group:      group,
		Mode:       mode,
		IDEContext: ideContext,
		CreatedAt:  time.Now(),
	}
	wm.items = append(wm.items, item)
	wm.save()
	wm.mu.Unlock()
	
	fmt.Printf("\n[Waitlist] Queued [%s] in group '%s'\n> ", id, group)
	
	wm.Pump()
}

func (wm *WaitlistManager) Remove(id string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for i, item := range wm.items {
		if item.ID == id {
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
		json.Unmarshal(data, &wm.items)
		dirty := false
		for _, item := range wm.items {
			var num int
			if _, err := fmt.Sscanf(item.ID, "exec-%d", &num); err == nil && num >= wm.nextID {
				wm.nextID = num + 1
			}
			// Reset tasks that were running during previous crash
			if item.Status == StatusRunning {
				item.Status = StatusFailed
				dirty = true
			}
		}
		if dirty {
			wm.save()
		}
	}
}

func (wm *WaitlistManager) save() {
	data, _ := json.MarshalIndent(wm.items, "", "  ")
	os.WriteFile(wm.filePath, data, 0644)
	
	// Broadcast waitlist to UI
	if wm.orchestrator != nil && wm.orchestrator.Bus != nil {
		wm.orchestrator.Bus.Publish(events.EventType("WaitlistUpdated"), events.Component("waitlist"), wm.items)
	}
}

func (wm *WaitlistManager) Pump() {
	wm.mu.Lock()
	
	// Count running total and running per group
	runningTotal := 0
	runningGroups := make(map[string]int)
	
	for _, item := range wm.items {
		if item.Status == StatusRunning {
			runningTotal++
			if item.Group != "" {
				runningGroups[item.Group]++
			}
		}
	}
	
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
			
			wm.save()
			
			// Inject memory for this execution
			wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
				Scope:   memory.ScopeExecution,
				ScopeID: item.ID,
				Key:     "user_prompt",
				Value:   item.Prompt,
				Owner:   "waitlist",
			})
			wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
				Scope:   memory.ScopeExecution,
				ScopeID: "compile-" + item.ID,
				Key:     "user_prompt",
				Value:   item.Prompt,
				Owner:   "waitlist",
			})
			
			if item.IDEContext != "" {
				wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
					Scope:   memory.ScopeExecution,
					ScopeID: item.ID,
					Key:     "ide_context",
					Value:   item.IDEContext,
					Owner:   "waitlist",
				})
				wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
					Scope:   memory.ScopeExecution,
					ScopeID: "compile-" + item.ID,
					Key:     "ide_context",
					Value:   item.IDEContext,
					Owner:   "waitlist",
				})
			}
			
			// Inject workspace_dir for this execution (and its compiler phase)
			isolatedWorkspacePath, _ := filepath.Abs(filepath.Join("../../", ".hyperparallel", "sessions", item.ID))
			wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
				Scope:   memory.ScopeExecution,
				ScopeID: item.ID,
				Key:     "workspace_dir",
				Value:   isolatedWorkspacePath,
				Owner:   "waitlist",
			})
			wm.orchestrator.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
				Scope:   memory.ScopeExecution,
				ScopeID: "compile-" + item.ID,
				Key:     "workspace_dir",
				Value:   isolatedWorkspacePath,
				Owner:   "waitlist",
			})
			
			// Launch workflow
			go func(i *WaitlistItem) {
				// give memory a split second to propagate
				time.Sleep(500 * time.Millisecond)
				
				if wm.globalWorkflow != nil {
					wm.graphEngine.SubmitWorkflow(wm.globalWorkflow, i.ID)
				} else if wm.compilerDef != nil {
					wm.graphEngine.SubmitWorkflow(wm.compilerDef, "compile-"+i.ID)
				}
			}(item)
		}
	}
	wm.mu.Unlock()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		outPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(outPath, info.Mode())
		}
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		dstFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()
		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}
