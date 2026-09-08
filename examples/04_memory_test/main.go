package main

import (
	"flag"
	"fmt"
	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/orchestrator"
	"github.com/reticle/runtime/routing"
	"github.com/reticle/runtime/telemetry"
	"sync"
	"time"
)

func main() {
	debugMode := flag.Bool("debug", false, "Enable debug mode to show full artifact payloads in logs")
	guiMode := flag.Bool("gui", false, "Start telemetry GUI and wait for browser connection before running")
	flag.Parse()

	orch := orchestrator.New()
	if *debugMode {
		orch.Logger.DebugMode = true
	}
	orch.Start()

	if *guiMode {
		telemetryServer := telemetry.NewServer(orch.Bus, ":8080", "../..")
		telemetryServer.Start()
		telemetryServer.WaitForClient()
	}

	graphEngine := agent.NewGraphEngine(orch.Logger, orch.Bus)
	graphEngine.Start()

	registry := agent.NewRegistry()
	if err := registry.LoadAgents("./agents"); err != nil {
		orch.Logger.Error("Failed to load agents", "error", err)
		return
	}

	if err := registry.LoadWorkflows("./workflows"); err != nil {
		orch.Logger.Error("Failed to load workflows", "error", err)
		return
	}

	envManager := agent.NewEnvironmentManager(orch.Logger, "../../")
	workers := registry.BuildWorkers(orch.Logger, orch.Bus, envManager)

	instructionStore := agent.NewInstructionStore()
	router := routing.NewRouter(orch.Logger, orch.Bus, false)

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

	wf, ok := registry.Workflows["memory-test"]
	if !ok {
		orch.Logger.Error("Workflow memory-test not found")
		return
	}

	fmt.Println("\n--- STARTING MEMORY TEST WORKFLOW ---")

	wg := &sync.WaitGroup{}
	wg.Add(1)

	orch.Bus.Subscribe(events.EventType("WorkflowCompleted"), func(e events.RuntimeEvent) {
		fmt.Println("\n✅ WORKFLOW COMPLETED SUCCESSFULLY!")
		wg.Done()
	})
	orch.Bus.Subscribe(events.EventType("WorkflowFailed"), func(e events.RuntimeEvent) {
		fmt.Printf("\n❌ WORKFLOW FAILED: %+v\n", e.Payload)
		wg.Done()
	})

	err := graphEngine.SubmitWorkflow(wf, "exec-test-001")
	if err != nil {
		orch.Logger.Error("Failed to submit workflow", "error", err)
	}

	wg.Wait()
	time.Sleep(3 * time.Second)

	orch.Shutdown()
}
