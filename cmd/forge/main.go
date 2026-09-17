package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/orchestrator"
	"github.com/reticle/runtime/routing"
	"github.com/reticle/runtime/telemetry"
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

func getLastWorkspace(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var newest string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "forge_workspace_") {
			if newest == "" || e.Name() > newest {
				newest = e.Name()
			}
		}
	}
	if newest != "" {
		return filepath.Join(dir, newest)
	}
	return ""
}

func main() {
	// Hack to allow -workspace without arguments
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-workspace" {
			if i == len(os.Args)-1 || strings.HasPrefix(os.Args[i+1], "-") {
				// Insert "last" as the argument for -workspace
				newArgs := make([]string, 0, len(os.Args)+1)
				newArgs = append(newArgs, os.Args[:i+1]...)
				newArgs = append(newArgs, "last")
				newArgs = append(newArgs, os.Args[i+1:]...)
				os.Args = newArgs
				break
			}
		}
	}

	isolatedFlag := flag.Bool("isolated", true, "Use isolated session workspaces (dynamic compilation per prompt)")
	freshFlag := flag.Bool("fresh", false, "Clear all previous isolated sessions on boot")
	batchSize := flag.Int("batch", 5, "Number of concurrent workflows to run in a batch")
	workspaceFlag := flag.String("workspace", "", "Path to an existing compiled workspace to run (skips compilation Phase 1)")
	portFlag := flag.Int("port", 8080, "Port to run the UI telemetry server on")
	legacyFlag := flag.Bool("legacy", false, "Use legacy terminal UI (no web UI)")
	nativeFlag := flag.Bool("native", false, "Run worker terminal commands natively on the host instead of in a Docker container")
	allModelsFlag := flag.Bool("all-models", false, "Load all available models (instead of just premium tier)")
	retriesFlag := flag.Int("retries", 3, "Maximum attempts per node; only transient provider failures are retried")
	flag.Parse()
	if *batchSize < 1 || *batchSize > 16 || *retriesFlag < 1 || *retriesFlag > 15 || *portFlag < 1 || *portFlag > 65535 {
		fmt.Fprintln(os.Stderr, "Invalid batch, retry or port configuration")
		return
	}

	args := flag.Args()
	var userPrompt string
	if len(args) > 0 {
		userPrompt = strings.Join(args, " ")
	}

	if !*nativeFlag {
		if err := exec.Command("docker", "info").Run(); err != nil {
			fmt.Fprintln(os.Stderr, "Docker is unavailable. Start Docker, or explicitly choose -native for trusted host execution.")
			return
		}
	}

	if *workspaceFlag == "last" || (*workspaceFlag == "" && *freshFlag) {
		lastWs := getLastWorkspace("workspaces")
		if lastWs != "" {
			*workspaceFlag = lastWs
		} else if *workspaceFlag == "last" {
			*workspaceFlag = "" // fallback to creating a new one if none exist
		}
	}

	fmt.Println("==================================================")
	fmt.Println("             Reticle Forge                  ")
	fmt.Println("==================================================")
	if *workspaceFlag != "" {
		fmt.Printf("Loading Workspace: %s\n\n", *workspaceFlag)
	} else {
		fmt.Printf("Prompt: %s\n\n", userPrompt)
	}

	// Clean up old workspaces
	// Workspaces are retained until explicitly removed by their owner.

	rootDir, _ := filepath.Abs("../../")
	if _, err := os.Stat(filepath.Join(rootDir, "runtime", "go.mod")); err != nil {
		fmt.Fprintln(os.Stderr, "Start Forge from the Reticle cmd/forge directory")
		return
	}
	os.Setenv("RETICLE_ROOT", rootDir)

	if *freshFlag {
		*workspaceFlag = ""
	}

	timestamp := time.Now().Format("20060102_150405.000000000")
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
	loadEnv(rootDir)

	orch := orchestrator.New()
	orch.Start()

	// Inject max_retries config into the global memory scope via event bus
	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("system"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "max_retries",
		Value:   *retriesFlag,
		Owner:   "system",
	})

	defer orch.Shutdown()

	telemetryServer := telemetry.NewServer(orch.Bus, fmt.Sprintf(":%d", *portFlag), rootDir)
	if err := telemetryServer.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	if !*legacyFlag {
		fmt.Printf("[UI] Telemetry running on http://localhost:%d\n", *portFlag)
	} else {
		fmt.Printf("[UI] Legacy Telemetry running on http://localhost:%d\n", *portFlag)
	}

	graphPath := filepath.Join(rootDir, ".reticle", "executions", "state.json")
	graphEngine, err := agent.NewPersistentGraphEngine(orch.Logger, orch.Bus, graphPath)
	if err != nil {
		orch.Logger.Error("Execution restart recovery failed", "error", err)
		return
	}
	effectManager, err := agent.NewPersistentEffectManager(filepath.Join(rootDir, ".reticle", "effects", "state.json"))
	if err != nil {
		orch.Logger.Error("External-effect recovery failed", "error", err)
		return
	}
	effectManager.Start(orch.Bus)

	registry := agent.NewRegistry()
	compilerDir, _ := filepath.Abs(filepath.Join("compiler"))

	// Load Global Skills and Agents
	if err := registry.LoadSkills(filepath.Join(rootDir, "skills")); err != nil {
		orch.Logger.Error("Skill registry failed", "error", err)
		return
	}
	if err := registry.LoadAgents(filepath.Join(rootDir, "agents")); err != nil && !os.IsNotExist(err) {
		orch.Logger.Error("Agent registry failed", "error", err)
		return
	}

	// Build Available Agents prompt dynamically

	// Load Compiler Agents
	if err := registry.LoadSkills(filepath.Join(compilerDir, "skills")); err != nil {
		orch.Logger.Error("Compiler skills failed", "error", err)
		return
	}
	if err := registry.LoadAgents(filepath.Join(compilerDir, "agents")); err != nil {
		orch.Logger.Error("Compiler agents failed", "error", err)
		return
	}
	if err := registry.LoadWorkflows(filepath.Join(compilerDir, "workflows")); err != nil {
		orch.Logger.Error("Compiler workflows failed", "error", err)
		return
	}
	var sb strings.Builder
	for id, agentDef := range registry.Definitions {
		if id == "architect-agent" || id == "coder-agent" || id == "scaffolder-agent" || id == "writer-agent" || id == "hermes-coder-agent" || id == "quant-agent" || id == "osint-agent" || id == "browser-agent" || id == "mock_stress_tester" {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s (%s)\n", id, agentDef.Description))
	}

	availableAgents := sb.String()

	// Load .env keys securely
	loadEnv(rootDir)

	envManager := agent.NewEnvironmentManager(orch.Logger, rootDir, orch.Bus)
	workers := registry.BuildWorkers(orch.Logger, orch.Bus, envManager)

	instructionStore := agent.NewInstructionStore()
	router := routing.NewRouter(orch.Logger, orch.Bus, *allModelsFlag)

	subManager := agent.NewSubscriptionManager(orch.Logger, orch.Bus)
	dispatcher := agent.NewDispatcher(orch.Logger, orch.Bus, instructionStore, router, orch.RuntimeState)
	graphEngine.SetWorkerValidator(dispatcher.HasWorker)
	graphEngine.Start()

	waitlistPath := filepath.Join(workspaceDir, "waitlist.json")
	wm := NewWaitlistManager(waitlistPath, *batchSize, graphEngine, orch, registry, dispatcher, envManager)

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

	allowNative := "false"
	if *nativeFlag {
		allowNative = "true"
	}
	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "allow_native_execution",
		Value:   allowNative,
		Owner:   "forge",
	})

	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "llm_num_ctx",
		Value:   orch.Settings.NumCtx,
		Owner:   "forge",
	})
	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "llm_max_tokens",
		Value:   orch.Settings.MaxTokens,
		Owner:   "forge",
	})
	orch.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("forge"), memory.MemoryEntry{
		Scope:   memory.ScopeGlobal,
		ScopeID: "global",
		Key:     "llm_temperature",
		Value:   orch.Settings.Temperature,
		Owner:   "forge",
	})

	time.Sleep(500 * time.Millisecond) // Let memory propagate

	// Pass the Compiler DAG to the Waitlist Manager so it can build sessions dynamically
	compilerWf := registry.Workflows["forge-compiler"]
	wm.SetCompilerDef(compilerWf)

	if *isolatedFlag {
		fmt.Println("\n[INFO] Orchestrator running in Session Isolation Mode")
		fmt.Println("[INFO] Workspaces will be dynamically generated in .reticle/sessions/")
	} else {
		// Legacy Mode (Global Workspace)
		fmt.Println("\n[INFO] Orchestrator running in Global Workspace Mode")

		if *workspaceFlag == "" {
			fmt.Println("\n[PHASE 1] COMPILATION STARTED")
			err := runWorkflowSync(graphEngine, orch, compilerWf, "compile-001")
			if err != nil {
				fmt.Printf("Compilation Failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("\n[PHASE 1] COMPILATION SUCCESSFUL")
		} else {
			fmt.Println("\n[PHASE 1] SKIPPED (Loading existing workspace)")
		}

		// Load generated agents and workflows
		registry.LoadAgents(filepath.Join(workspaceDir, "agents"))
		registry.LoadWorkflows(filepath.Join(workspaceDir, "workflows"))

		newWorkers := registry.BuildWorkers(orch.Logger, orch.Bus, envManager)
		for _, w := range newWorkers {
			dispatcher.RegisterWorker(w)
		}

		for _, sub := range registry.BuildSubscriptions() {
			subManager.Register(sub)
		}

		// Change working directory to the workspace

		execWf := registry.Workflows["generated-workflow"]
		if execWf == nil && len(registry.Workflows) > 0 {
			for id, wf := range registry.Workflows {
				if id != "forge-compiler" {
					execWf = wf
					break
				}
			}
		}
		wm.SetGlobalWorkflow(execWf)
		wm.SetCompilerDef(nil)
	}

	wm.Pump()
	// Enqueue initial prompt if present
	if userPrompt != "" {
		wm.Enqueue(userPrompt, "", ModeParallel, "", "auto", 5, nil)
	}

	// Start Execution Shell
	reader := bufio.NewScanner(os.Stdin)
	fmt.Println("\n==================================================")
	fmt.Println("             Forge Execution Shell                ")
	fmt.Println("==================================================")
	fmt.Printf("Concurrency Limit: %d\n", *batchSize)
	fmt.Println("Commands:")
	fmt.Println("  exit                - Shutdown Orchestrator")
	fmt.Println("  models list         - List all available models and their status")
	fmt.Println("  models enable <ID>  - Enable a model by its ID or Key")
	fmt.Println("  models disable <ID> - Disable a model by its ID or Key")
	fmt.Println("  @group:NAME [PROMPT]- Queue prompt in a sequential group")
	fmt.Println("  [PROMPT]            - Queue prompt in default parallel mode")
	fmt.Print("> ")

	for reader.Scan() {
		text := strings.TrimSpace(reader.Text())
		if text == "exit" {
			return
		}
		if text == "models list" {
			routing.ModelsMutex.RLock()
			fmt.Println("\nAvailable Models:")
			for _, m := range routing.AvailableModels {
				status := "DISABLED"
				if m.Enabled {
					status = "ENABLED "
				}
				fmt.Printf("  [%s] %-50s (Mod: %-6s, Cap: %4.1f, Cost: %5.2f, Key: %s)\n", status, m.ID, m.Modality, m.Capability, m.Cost, m.APIKeyEnv)
			}
			routing.ModelsMutex.RUnlock()
			fmt.Print("> ")
			continue
		}
		if strings.HasPrefix(text, "models enable ") {
			target := strings.TrimSpace(strings.TrimPrefix(text, "models enable "))
			routing.ModelsMutex.Lock()
			found := false
			for i := range routing.AvailableModels {
				if routing.AvailableModels[i].ID == target || routing.AvailableModels[i].Key() == target {
					routing.AvailableModels[i].Enabled = true
					found = true
				}
			}
			routing.ModelsMutex.Unlock()
			if found {
				fmt.Printf("[INFO] Enabled model: %s\n", target)
			} else {
				fmt.Printf("[ERROR] Model not found: %s\n", target)
			}
			fmt.Print("> ")
			continue
		}
		if strings.HasPrefix(text, "models disable ") {
			target := strings.TrimSpace(strings.TrimPrefix(text, "models disable "))
			routing.ModelsMutex.Lock()
			found := false
			for i := range routing.AvailableModels {
				if routing.AvailableModels[i].ID == target || routing.AvailableModels[i].Key() == target {
					routing.AvailableModels[i].Enabled = false
					found = true
				}
			}
			routing.ModelsMutex.Unlock()
			if found {
				fmt.Printf("[INFO] Disabled model: %s\n", target)
			} else {
				fmt.Printf("[ERROR] Model not found: %s\n", target)
			}
			fmt.Print("> ")
			continue
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

		wm.Enqueue(text, group, mode, "", "auto", 5, nil)
	}

	if err := reader.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading standard input: %v\n", err)
	} else {
		// If we reached EOF (e.g. running in background without stdin), block until SIGINT
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
	}

	fmt.Println("\n[INFO] Shutting down...")
	time.Sleep(2 * time.Second) // Let telemetry flush
}
