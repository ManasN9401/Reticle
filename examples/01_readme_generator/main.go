package main

import (
	"flag"
	"fmt"
	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/memory"
	"github.com/reticle/runtime/orchestrator"
	"github.com/reticle/runtime/routing"
	"github.com/reticle/runtime/telemetry"
	"time"
)

func main() {
	debugMode := flag.Bool("debug", false, "Enable debug mode to show full artifact payloads in logs")
	guiMode := flag.Bool("gui", false, "Start telemetry GUI and wait for browser connection before running")
	flag.Parse()

	// Phase 1: Bootstrapping the runtime environment
	orch := orchestrator.New()
	if *debugMode {
		orch.Logger.DebugMode = true
	}
	orch.Start()

	// Start Telemetry UI if requested
	if *guiMode {
		telemetryServer := telemetry.NewServer(orch.Bus, ":8080")
		telemetryServer.Start()
		telemetryServer.WaitForClient()
	}

	graphEngine := agent.NewGraphEngine(orch.Logger, orch.Bus)
	graphEngine.Start()

	// Phase 2: Agent Discovery & Registration
	registry := agent.NewRegistry()
	if err := registry.LoadAgents("./agents"); err != nil {
		orch.Logger.Error("Failed to load agents", "error", err)
		return
	}
	
	if err := registry.LoadSkills("./skills"); err != nil {
		orch.Logger.Error("Failed to load skills", "error", err)
		return
	}
	
	if err := registry.LoadWorkflows("./workflows"); err != nil {
		orch.Logger.Error("Failed to load workflows", "error", err)
		return
	}
	
	envManager := agent.NewEnvironmentManager(orch.Logger, "../../")
	workers := registry.BuildWorkers(orch.Logger, orch.Bus, envManager)
	
	// Phase 3: Define Subscriptions (JIT Dispatching) & Runtime Instructions
	instructionStore := agent.NewInstructionStore()
	instructionStore.Add(agent.Instruction{
		ID:      "inst-1",
		Scope:   agent.ScopeGlobal,
		Content: "Always use GitHub Flavored Markdown.",
	})
	instructionStore.Add(agent.Instruction{
		ID:      "inst-2",
		Scope:   agent.ScopeAgent,
		Target:  "outline-gen",
		Content: "Include a section for 'Contributors'.",
	})

	router := routing.NewRouter(orch.Logger, orch.Bus)

	subManager := agent.NewSubscriptionManager(orch.Logger, orch.Bus)
	dispatcher := agent.NewDispatcher(orch.Logger, orch.Bus, instructionStore, router, orch.RuntimeState)
	
	for _, sub := range registry.BuildSubscriptions() {
		subManager.Register(sub)
	}

	for _, w := range workers {
		dispatcher.RegisterWorker(w)
	}

	// Ad-hoc reactivity is now fully driven by YAML!
	subManager.Start()
	dispatcher.Start()

	// Phase 4: Submit Workflow
	wf, ok := registry.Workflows["readme-generator"]
	if !ok {
		orch.Logger.Error("Workflow readme-generator not found")
		return
	}

	fmt.Println("\n--- STARTING AUTONOMOUS WORKFLOW ---")
	err := graphEngine.SubmitWorkflow(wf, "exec-001")
	if err != nil {
		orch.Logger.Error("Failed to submit workflow", "error", err)
	}

	superWf, ok := registry.Workflows["supervisor-demo"]
	if ok {
		fmt.Println("\n--- TESTING SUPERVISOR GRAPH ---")
		graphEngine.SubmitWorkflow(superWf, "exec-super-001")
		time.Sleep(2 * time.Second)
	}

	// Phase 5: Submit Failing Workflow
	failWf, ok := registry.Workflows["fail-workflow"]
	if ok {
		fmt.Println("\n--- TESTING FAILURE CASCADE ---")
		graphEngine.SubmitWorkflow(failWf, "exec-002")
		time.Sleep(1 * time.Second)
	}

	// Phase 5: Result (Query API)

	// --- NEW QUERY API TESTS ---
	fmt.Println("\n--- TESTING ARTIFACT QUERY API ---")
	
	// Test 1: FindByParent
	children := orch.Artifacts.FindChildren(memory.ArtifactID("readme_outline"))
	fmt.Printf("1. Found %d children for 'readme_outline' (Expected 1)\n", len(children))
	if len(children) > 0 {
		fmt.Printf("   -> Child ID: %s, Producer: %s\n", children[0].ID, children[0].Producer)
	}

	// Test 2: FindByProducer
	outlineArtifacts := orch.Artifacts.FindByProducer("outline-gen")
	fmt.Printf("2. Found %d artifacts produced by 'outline-gen' (Expected 1)\n", len(outlineArtifacts))

	// Test 3: Get (Latest)
	latest, ok := orch.Artifacts.Get(memory.ArtifactID("readme_final"))
	if ok {
		fmt.Printf("3. Get('readme_final') found Version: %d\n", latest.Version)
	}

	// Test 4: UpdateVersion (Append-Only)
	fmt.Println("4. Testing UpdateVersion (Append-Only)...")
	newArt, err := orch.Artifacts.UpdateVersion(memory.ArtifactID("readme_final"), "# Reticle V2\nEven better!")
	if err == nil {
		fmt.Printf("   -> Updated artifact: %s to Version %d\n", newArt.ID, newArt.Version)
		
		// Verify Get picks up the new version!
		latestV2, _ := orch.Artifacts.Get(memory.ArtifactID("readme_final"))
		fmt.Printf("   -> Get('readme_final') now returns Version: %d\n", latestV2.Version)
		
		// Verify GetVersion picks up the old version!
		oldV1, _ := orch.Artifacts.GetVersion(memory.ArtifactID("readme_final"), 1)
		dataStr := oldV1.Data.(string)
		if len(dataStr) > 25 {
			dataStr = dataStr[0:25] + "..."
		}
		fmt.Printf("   -> GetVersion('readme_final', 1) still retains Version: %d data: %v\n", oldV1.Version, dataStr)
	}
	fmt.Println("----------------------------------")

	// Phase 6: Benchmark Model Routing
	fmt.Println("\n--- BENCHMARKING MODELS FOR ROUTING ---")
	for _, model := range routing.AvailableModels {
		benchTaskID := fmt.Sprintf("bench-outline-%s", model.ID)
		fmt.Printf("Benchmarking %s...\n", model.ID)
		
		benchTask := agent.Task{
			ID:          agent.TaskID(benchTaskID),
			AgentID:     "outline-gen",
			ExecutionID: "bench-exec",
			Workflow:    "benchmark-run",
			Parameters: map[string]any{
				"llm_model": model.ID, // Force model
			},
		}
		
		// Fire TaskCreated directly to dispatcher via bus
		orch.Bus.Publish(events.EventType("TaskCreated"), events.Component("benchmark"), benchTask)
		time.Sleep(500 * time.Millisecond) // Give worker time to exit and publish completion
	}

	// Give time for router updates to settle
	time.Sleep(1 * time.Second)
	
	fmt.Println("\n--- ROUTER MATRIX POST-BENCHMARK ---")
	for m, prob := range router.Matrix["outline-gen"] {
		fmt.Printf("Agent: outline-gen | Model: %s | Success Probability: %.2f\n", m, prob)
	}
	
	// Now if we submit a real workflow, the router will use the learned matrix
	fmt.Println("\n--- STARTING OPTIMIZED ROUTED WORKFLOW ---")
	graphEngine.SubmitWorkflow(wf, "exec-003-routed")
	time.Sleep(2 * time.Second)

	fmt.Println("\n--- WORKFLOWS COMPLETED ---")
	if *guiMode {
		fmt.Println("Telemetry Server is still running. Open http://localhost:8080 in your browser.")
		fmt.Println("Waiting 60 seconds before shutting down...")
		time.Sleep(60 * time.Second)
	}

	orch.Shutdown()
}
