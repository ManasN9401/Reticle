package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"github.com/hyperparallel/runtime/agent"
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/memory"
	"github.com/hyperparallel/runtime/orchestrator"
	"github.com/hyperparallel/runtime/routing"
	"github.com/hyperparallel/runtime/telemetry"
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
	
	// Phase 3: Define Subscriptions (JIT Dispatching)
	instructionStore := agent.NewInstructionStore()
	router := routing.NewRouter(orch.Logger, orch.Bus)

	subManager := agent.NewSubscriptionManager(orch.Logger, orch.Bus)
	dispatcher := agent.NewDispatcher(orch.Logger, orch.Bus, instructionStore, router)
	
	for _, sub := range registry.BuildSubscriptions() {
		subManager.Register(sub)
	}

	for _, w := range workers {
		dispatcher.RegisterWorker(w)
	}

	subManager.Start()
	dispatcher.Start()

	// Phase 4: Submit Workflow
	wf, ok := registry.Workflows["book-creator"]
	if !ok {
		orch.Logger.Error("Workflow book-creator not found")
		return
	}

	fmt.Println("\n--- STARTING BOOK CREATOR WORKFLOW (DIAMOND GRAPH) ---")
	
	wg := &sync.WaitGroup{}
	wg.Add(1)
	
	orch.Bus.Subscribe(events.EventType("WorkflowCompleted"), func(e events.RuntimeEvent) {
		fmt.Println("\n✅ WORKFLOW COMPLETED SUCCESSFULLY!")
		wg.Done()
	})
	orch.Bus.Subscribe(events.EventType("WorkflowFailed"), func(e events.RuntimeEvent) {
		fmt.Printf("\n❌ WORKFLOW FAILED: %+v\n", e.Payload)
		wg.Done() // also exit on fail
	})

	// Add subscriber to save the final book to disk
	saveBook := func(e events.RuntimeEvent) {
		artifact, ok := e.Payload.(*memory.Artifact)
		if ok && artifact.ID == "book_final" {
			if dataStr, isStr := artifact.Data.(string); isStr {
				_ = os.WriteFile("book.md", []byte(dataStr), 0644)
				fmt.Println("\n✅ Book successfully written to book.md!")
			}
		}
	}
	orch.Bus.Subscribe(events.EventType("ArtifactStored"), saveBook)
	orch.Bus.Subscribe(events.EventType("ArtifactVersionCreated"), saveBook)

	err := graphEngine.SubmitWorkflow(wf, "exec-book-001")
	if err != nil {
		orch.Logger.Error("Failed to submit workflow", "error", err)
	}

	if *guiMode {
		fmt.Println("Telemetry Server is still running. Open http://localhost:8080 in your browser.")
		fmt.Println("Waiting for workflow to complete...")
	}
	
	wg.Wait()
	time.Sleep(3 * time.Second) // Grace period for telemetry to flush

	orch.Shutdown()
}
