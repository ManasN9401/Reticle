package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hyperparallel/runtime/agent"
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/memory"
	"github.com/hyperparallel/runtime/orchestrator"
	"github.com/hyperparallel/runtime/routing"
	"github.com/hyperparallel/runtime/telemetry"
)

func runWorkflowSync(graphEngine *agent.GraphEngine, orch *orchestrator.Orchestrator, wf *agent.WorkflowDefinition, execId string) error {
	var wg sync.WaitGroup
	wg.Add(1)
	
	var execErr error
	var done sync.Once
	
	orch.Bus.Subscribe(events.EventType("WorkflowCompleted"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if payload["execution"] == execId {
				done.Do(func() { wg.Done() })
			}
		}
	})
	orch.Bus.Subscribe(events.EventType("WorkflowFailed"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if payload["execution"] == execId {
				execErr = fmt.Errorf("workflow failed: %v", e.Payload)
				done.Do(func() { wg.Done() })
			}
		}
	})

	err := graphEngine.SubmitWorkflow(wf, execId)
	if err != nil {
		return fmt.Errorf("failed to submit workflow: %w", err)
	}

	wg.Wait()
	return execErr
}

func main() {
	autoApprove := flag.Bool("auto-approve", false, "Execute the compiled workflow automatically without prompting")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("Usage: forge [flags] \"<prompt>\"")
		os.Exit(1)
	}
	userPrompt := strings.Join(args, " ")

	fmt.Println("==================================================")
	fmt.Println("             HyperParallel Forge                  ")
	fmt.Println("==================================================")
	fmt.Printf("Prompt: %s\n\n", userPrompt)

	timestamp := time.Now().Format("20060102_150405")
	workspaceDir, _ := filepath.Abs(filepath.Join("workspaces", fmt.Sprintf("forge_workspace_%s", timestamp)))
	
	availableAgents := `
- web-search-agent (Searches the web)
- markdown-writer-agent (Writes markdown docs)
- python-runner-agent (Executes Python code securely)
- auditor-agent (Reviews code/text)
`

	// 1. Boot Runtime
	orch := orchestrator.New()
	orch.Start()
	defer orch.Shutdown()

	telemetryServer := telemetry.NewServer(orch.Bus, ":8080")
	go telemetryServer.Start()
	fmt.Println("[UI] Telemetry running on http://localhost:8080")

	graphEngine := agent.NewGraphEngine(orch.Logger, orch.Bus)
	graphEngine.Start()

	registry := agent.NewRegistry()
	compilerDir, _ := filepath.Abs(filepath.Join("compiler"))
	_ = registry.LoadSkills(filepath.Join(compilerDir, "skills"))
	_ = registry.LoadAgents(filepath.Join(compilerDir, "agents"))
	_ = registry.LoadWorkflows(filepath.Join(compilerDir, "workflows"))
	
	rootDir, _ := filepath.Abs("../../")
	envManager := agent.NewEnvironmentManager(orch.Logger, rootDir)
	workers := registry.BuildWorkers(orch.Logger, orch.Bus, envManager)
	
	instructionStore := agent.NewInstructionStore()
	router := routing.NewRouter(orch.Logger, orch.Bus)

	subManager := agent.NewSubscriptionManager(orch.Logger, orch.Bus)
	dispatcher := agent.NewDispatcher(orch.Logger, orch.Bus, instructionStore, router, orch.RuntimeState)
	
	for _, sub := range registry.BuildSubscriptions() {
		subManager.Register(sub)
	}
	for _, w := range workers {
		dispatcher.RegisterWorker(w)
	}

	subManager.Start()
	dispatcher.Start()

	// Inject Memory
	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "user_prompt",
		Value:   userPrompt,
		Owner:   "forge",
	})
	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "available_agents",
		Value:   availableAgents,
		Owner:   "forge",
	})
	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "workspace_dir",
		Value:   workspaceDir,
		Owner:   "forge",
	})
	time.Sleep(500 * time.Millisecond) // Let memory propagate

	// PHASE 1
	wf := registry.Workflows["forge-compiler"]
	fmt.Println("\n[PHASE 1] COMPILATION STARTED")
	
	err := runWorkflowSync(graphEngine, orch, wf, "compile-001")
	if err != nil {
		log.Fatalf("Compilation Failed: %v", err)
	}
	fmt.Println("\n[PHASE 1] COMPILATION SUCCESSFUL")

	// 4. Ask for Approval
	if !*autoApprove {
		fmt.Printf("\nForge has compiled the workspace to: %s\n", workspaceDir)
		fmt.Print("Do you want to execute it now? (y/n): ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Execution aborted. The workspace has been saved.")
			os.Exit(0)
		}
	} else {
		fmt.Println("\n[INFO] Auto-Approve enabled. Proceeding to execution immediately.")
	}

	// 5. Hot-Load new agents
	if err := registry.LoadAgents(filepath.Join(workspaceDir, "agents")); err != nil {
		log.Fatalf("Failed to load agents: %v", err)
	}
	if err := registry.LoadWorkflows(filepath.Join(workspaceDir, "workflows")); err != nil {
		log.Fatalf("Failed to load workflows: %v", err)
	}
	
	newWorkers := registry.BuildWorkers(orch.Logger, orch.Bus, envManager)
	for _, w := range newWorkers {
		dispatcher.RegisterWorker(w) 
	}
	
	for _, sub := range registry.BuildSubscriptions() {
		subManager.Register(sub) // Manager should ignore duplicates
	}

	fmt.Println("\n[PHASE 2] EXECUTION STARTED")
	
	// Change working directory to the workspace so relative paths resolve correctly
	if err := os.Chdir(workspaceDir); err != nil {
		log.Fatalf("Failed to chdir to workspace: %v", err)
	}
	
	execWf := registry.Workflows["generated-workflow"]
	if execWf == nil {
		log.Fatalf("Execution Failed: generated-workflow not found in registry")
	}
	err = runWorkflowSync(graphEngine, orch, execWf, "exec-001")
	if err != nil {
		log.Fatalf("Execution Failed: %v", err)
	}
	fmt.Println("\n[PHASE 2] EXECUTION SUCCESSFUL")
	
	time.Sleep(2 * time.Second) // Let telemetry flush
}
