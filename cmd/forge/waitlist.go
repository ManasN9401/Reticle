package main

import (
	"encoding/json"
	"fmt"
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
	ID        string          `json:"id"`
	Prompt    string          `json:"prompt"`
	Status    ExecutionStatus `json:"status"`
	Group     string          `json:"group"`
	Mode      ExecutionMode   `json:"mode"`
	CreatedAt time.Time       `json:"created_at"`
}

type WaitlistManager struct {
	mu           sync.Mutex
	items        []*WaitlistItem
	nextID       int
	filePath     string
	maxWorkers   int
	graphEngine  *agent.GraphEngine
	orchestrator *orchestrator.Orchestrator
	workflowDef  *agent.WorkflowDefinition
}

func NewWaitlistManager(filePath string, maxWorkers int, engine *agent.GraphEngine, orch *orchestrator.Orchestrator) *WaitlistManager {
	wm := &WaitlistManager{
		filePath:     filePath,
		maxWorkers:   maxWorkers,
		graphEngine:  engine,
		orchestrator: orch,
		items:        make([]*WaitlistItem, 0),
		nextID:       1,
	}
	wm.load()
	
	// Subscribe to events
	orch.Bus.Subscribe(events.EventType("WorkflowCompleted"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if execId, ok := payload["execution"].(string); ok {
				wm.updateStatus(execId, StatusCompleted)
				wm.Pump()
				
				// Dump Artifacts
				artifacts := orch.Artifacts.GetByExecution(execId)
				if len(artifacts) > 0 {
					outDir := filepath.Join(filepath.Dir(filePath), "outputs", execId)
					os.MkdirAll(outDir, 0755)
					
					var readmeContent string
					readmeContent += fmt.Sprintf("# Execution %s Outputs\n\n", execId)
					
					for _, art := range artifacts {
						if art.Data != nil {
							// Try to cast to string or bytes
							var content []byte
							switch v := art.Data.(type) {
							case string:
								content = []byte(v)
							case []byte:
								content = v
							default:
								b, _ := json.MarshalIndent(v, "", "  ")
								content = b
							}
							
							fileName := string(art.ID)
							
							// Add extension if not present in ID
							if !strings.Contains(fileName, ".") {
								if strings.Contains(string(art.Type), "code") || strings.Contains(string(art.Type), "python") {
									fileName += ".py"
								} else if strings.Contains(string(art.Type), "text") || strings.Contains(string(art.Type), "markdown") {
									fileName += ".md"
								} else {
									fileName += ".json"
								}
							}
							
							// Just write it out
							artPath := filepath.Join(outDir, fileName)
							os.WriteFile(artPath, content, 0644)
							readmeContent += fmt.Sprintf("- `%s` (Producer: %s, Type: %s)\n", fileName, art.Producer, art.Type)
						}
					}
					os.WriteFile(filepath.Join(outDir, "README.md"), []byte(readmeContent), 0644)
					fmt.Printf("\n[INFO] Dumped %d artifacts to %s\n> ", len(artifacts), outDir)
				}
			}
		}
	})
	
	orch.Bus.Subscribe(events.EventType("WorkflowFailed"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if execId, ok := payload["execution"].(string); ok {
				wm.updateStatus(execId, StatusFailed)
				wm.Pump()
			}
		}
	})
	
	orch.Bus.Subscribe(events.EventType("WaitlistCommand"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]any); ok {
			action, _ := payload["action"].(string)
			if action == "enqueue" {
				prompt, _ := payload["prompt"].(string)
				group, _ := payload["group"].(string)
				modeStr, _ := payload["mode"].(string)
				mode := ModeParallel
				if modeStr == "sequential" {
					mode = ModeSequential
				}
				wm.Enqueue(prompt, group, mode)
			} else if action == "remove" {
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

func (wm *WaitlistManager) Enqueue(prompt string, group string, mode ExecutionMode) {
	wm.mu.Lock()
	
	id := fmt.Sprintf("exec-%03d", wm.nextID)
	wm.nextID++
	
	if mode == "" {
		mode = ModeParallel
	}
	
	item := &WaitlistItem{
		ID:        id,
		Prompt:    prompt,
		Status:    StatusPending,
		Group:     group,
		Mode:      mode,
		CreatedAt: time.Now(),
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

func (wm *WaitlistManager) SetWorkflow(wf *agent.WorkflowDefinition) {
	wm.mu.Lock()
	wm.workflowDef = wf
	wm.mu.Unlock()
	wm.Pump() // Start processing if we have pending items
}

func (wm *WaitlistManager) load() {
	data, err := os.ReadFile(wm.filePath)
	if err == nil {
		json.Unmarshal(data, &wm.items)
		for _, item := range wm.items {
			var num int
			if _, err := fmt.Sscanf(item.ID, "exec-%d", &num); err == nil && num >= wm.nextID {
				wm.nextID = num + 1
			}
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
	defer wm.mu.Unlock()
	
	if wm.workflowDef == nil {
		return // Do not process queue until workflow is loaded
	}

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
			
			// Launch workflow
			go func(i *WaitlistItem) {
				// give memory a split second to propagate
				time.Sleep(100 * time.Millisecond) 
				err := wm.graphEngine.SubmitWorkflow(wm.workflowDef, i.ID)
				if err != nil {
					wm.updateStatus(i.ID, StatusFailed)
					wm.Pump()
				}
			}(item)
		}
	}
}
