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

func loadEnv(rootDir string) {
	envPath := filepath.Join(rootDir, ".env")
	data, err := os.ReadFile(envPath)
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
				}
			}
		}
	}
}

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
	batchSize := flag.Int("batch", 1, "Number of concurrent executions for the generated workflow in Phase 2")
	workspaceFlag := flag.String("workspace", "", "Path to an existing compiled workspace to run (skips compilation Phase 1)")
	portFlag := flag.Int("port", 8080, "Port to run the UI telemetry server on")
	flag.Parse()

	args := flag.Args()
	var userPrompt string
	if len(args) > 0 {
		userPrompt = strings.Join(args, " ")
	}

	fmt.Println("==================================================")
	fmt.Println("             HyperParallel Forge                  ")
	fmt.Println("==================================================")
	if *workspaceFlag != "" {
		fmt.Printf("Loading Workspace: %s\n\n", *workspaceFlag)
	} else {
		fmt.Printf("Prompt: %s\n\n", userPrompt)
	}

	// Clean up old workspaces
	cleanupWorkspaces("workspaces", 3)

	timestamp := time.Now().Format("20060102_150405")
	var workspaceDir string
	if *workspaceFlag != "" {
		workspaceDir, _ = filepath.Abs(*workspaceFlag)
	} else {
		workspaceDir, _ = filepath.Abs(filepath.Join("workspaces", fmt.Sprintf("forge_workspace_%s", timestamp)))
		if userPrompt == "" {
			fmt.Printf("[INFO] No prompt provided. Creating empty workspace: %s\n", filepath.Base(workspaceDir))
			os.MkdirAll(workspaceDir, 0755)
			os.MkdirAll(filepath.Join(workspaceDir, "agents"), 0755)
			os.MkdirAll(filepath.Join(workspaceDir, "workflows"), 0755)
			*workspaceFlag = workspaceDir // Trick Phase 1 into skipping
		}
	}
	rootDir, _ := filepath.Abs("../../")
	loadEnv(rootDir)

	// 1. Boot Runtime
	orch := orchestrator.New()
	orch.Start()
	defer orch.Shutdown()

	telemetryServer := telemetry.NewServer(orch.Bus, fmt.Sprintf(":%d", *portFlag))
	go telemetryServer.Start()
	fmt.Printf("[UI] Telemetry running on http://localhost:%d\n", *portFlag)

	graphEngine := agent.NewGraphEngine(orch.Logger, orch.Bus)
	graphEngine.Start()

	waitlistPath := filepath.Join(workspaceDir, "waitlist.json")
	wm := NewWaitlistManager(waitlistPath, *batchSize, graphEngine, orch)

	registry := agent.NewRegistry()
	compilerDir, _ := filepath.Abs(filepath.Join("compiler"))
	
	// Load Global Skills and Agents
	_ = registry.LoadSkills(filepath.Join(rootDir, "skills"))
	_ = registry.LoadAgents(filepath.Join(rootDir, "agents"))
	
	// Build Available Agents prompt dynamically
	var sb strings.Builder
	for id, agentDef := range registry.Definitions {
		sb.WriteString(fmt.Sprintf("- %s (%s)\n", id, agentDef.Description))
	}
	availableAgents := sb.String()
	
	// Load Compiler Agents
	_ = registry.LoadSkills(filepath.Join(compilerDir, "skills"))
	_ = registry.LoadAgents(filepath.Join(compilerDir, "agents"))
	_ = registry.LoadWorkflows(filepath.Join(compilerDir, "workflows"))
	
	// Load .env keys securely
	loadEnv(rootDir)
	
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
	if *workspaceFlag == "" {
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
				fmt.Println("Execution of initial prompt skipped. Entering Execution Shell...")
				userPrompt = "" // Prevent it from being enqueued
			}
		} else {
			fmt.Println("\n[INFO] Auto-Approve enabled. Proceeding to execution immediately.")
		}
	} else {
		fmt.Println("\n[PHASE 1] SKIPPED (Loading existing workspace)")
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
	wm.SetWorkflow(execWf)
	
	// Enqueue initial prompt if present
	if userPrompt != "" {
		wm.Enqueue(userPrompt, "", ModeParallel)
	}

	// Start Execution Shell
	reader := bufio.NewScanner(os.Stdin)
	fmt.Println("\n==================================================")
	fmt.Println("             Forge Execution Shell                ")
	fmt.Println("==================================================")
	fmt.Printf("Concurrency Limit: %d\n", *batchSize)
	fmt.Println("Commands:")
	fmt.Println("  exit                - Shutdown Orchestrator")
	fmt.Println("  @group:NAME [PROMPT]- Queue prompt in a sequential group")
	fmt.Println("  [PROMPT]            - Queue prompt in default parallel mode")
	fmt.Print("> ")
	
	for reader.Scan() {
		text := strings.TrimSpace(reader.Text())
		if text == "exit" {
			break
		}
		if text == "" {
			fmt.Print("> ")
			continue
		}
	
		if execWf == nil {
			fmt.Println("[ERROR] No workflow loaded in this workspace. You cannot queue executions.")
			fmt.Println("[HINT] To compile a new workflow, restart forge with your prompt as an argument: .\\forge.exe \"your prompt here\"")
			fmt.Print("> ")
			continue
		}

		group := ""
		mode := ModeParallel
		if strings.HasPrefix(text, "@group:") {
			parts := strings.SplitN(text, " ", 2)
			group = strings.TrimPrefix(parts[0], "@group:")
			if len(parts) > 1 {
				text = parts[1]
			} else {
				text = ""
			}
			mode = ModeSequential
		}
	
		wm.Enqueue(text, group, mode)
	}
	
	if err := reader.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading standard input: %v\n", err)
	}
	
	fmt.Println("\n[INFO] Shutting down...")
	time.Sleep(2 * time.Second) // Let telemetry flush
}

func cleanupWorkspaces(workspacesDir string, keepCount int) {
	entries, err := os.ReadDir(workspacesDir)
	if err != nil {
		return
	}

	var dirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "forge_workspace_") {
			dirs = append(dirs, entry)
		}
	}

	// Sort oldest to newest (assuming lexicographical timestamp naming)
	if len(dirs) <= keepCount {
		return
	}

	for i := 0; i < len(dirs)-keepCount; i++ {
		dirPath := filepath.Join(workspacesDir, dirs[i].Name())
		fmt.Printf("[INFO] Cleaning up old workspace: %s\n", dirs[i].Name())
		os.RemoveAll(dirPath)
	}
}
