package main

import (
	"bufio"
	"flag"
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
	batchSize := flag.Int("batch", 5, "Number of concurrent workflows to run in a batch")
	workspaceFlag := flag.String("workspace", "", "Path to an existing compiled workspace to run (skips compilation Phase 1)")
	portFlag := flag.Int("port", 8080, "Port to run the UI telemetry server on")
	legacyFlag := flag.Bool("legacy", false, "Use legacy terminal UI (no web UI)")
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

	if !*legacyFlag {
		fmt.Printf("[UI] Telemetry running on http://localhost:%d\n", *portFlag)
	} else {
		fmt.Printf("[UI] Legacy Telemetry running on http://localhost:%d\n", *portFlag)
	}

	graphEngine := agent.NewGraphEngine(orch.Logger, orch.Bus)
	graphEngine.Start()

	waitlistPath := filepath.Join(workspaceDir, "waitlist.json")
	registry := agent.NewRegistry()
	wm := NewWaitlistManager(waitlistPath, *batchSize, graphEngine, orch, registry)
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

	// Pass the Compiler DAG to the Waitlist Manager so it can build sessions dynamically
	compilerWf := registry.Workflows["forge-compiler"]
	wm.SetCompilerDef(compilerWf)

	fmt.Println("\n[INFO] Orchestrator running in Session Isolation Mode")
	fmt.Println("[INFO] Workspaces will be dynamically generated in .hyperparallel/sessions/")

	// Enqueue initial prompt if present
	if userPrompt != "" {
		wm.Enqueue(userPrompt, "", ModeParallel, "")
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

		wm.Enqueue(text, group, mode, "")
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
