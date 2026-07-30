package main

import (
	"flag"
	"fmt"
	"github.com/hyperparallel/runtime/agent"
	"github.com/hyperparallel/runtime/memory"
	"github.com/hyperparallel/runtime/orchestrator"
	"time"
)

func main() {
	debugMode := flag.Bool("debug", false, "Enable debug mode to show full artifact payloads in logs")
	flag.Parse()

	// Phase 1: Bootstrapping the runtime environment
	orch := orchestrator.New()
	if *debugMode {
		orch.Logger.DebugMode = true
	}
	orch.Start()

	sup := agent.NewSupervisor(orch.Logger, orch.Bus)

	outlineWorker := agent.NewWorker(agent.WorkerID("outline-gen"), "python", []string{"outline_agent.py"}, orch.Logger, orch.Bus)
	wordingWorker := agent.NewWorker(agent.WorkerID("wording-imp"), "python", []string{"wording_agent.py"}, orch.Logger, orch.Bus)

	err := sup.AssignTask(outlineWorker, agent.TaskRequest{
		ID:        agent.TaskID("task-1"),
		SessionID: orch.SessionState.SessionID,
		Workflow:  "readme-generator",
		Type:      "generate_outline",
		Payload:   "create an outline for a software project",
	}, memory.MemoryKey("readme_outline"))
	
	if err != nil {
		orch.Logger.Error("Failed to generate outline: %v", err)
		return
	}
	
	// Wait for async memory manager to process the event
	time.Sleep(50 * time.Millisecond)

	rawOutline, ok := orch.Artifacts.FindByID(memory.ArtifactID("readme_outline"))
	if !ok {
		orch.Logger.Error("Failed to find readme_outline artifact")
		return
	}
	outlineStr := rawOutline.Data.(string)

	err = sup.AssignTask(wordingWorker, agent.TaskRequest{
		ID:        agent.TaskID("task-2"),
		SessionID: orch.SessionState.SessionID,
		Workflow:  "readme-generator",
		Type:      "improve_wording",
		Payload:   outlineStr,
	}, memory.MemoryKey("readme_final"))

	if err != nil {
		orch.Logger.Error("Failed to improve wording: %v", err)
		return
	}
	
	// Wait for async memory manager to process the event
	time.Sleep(50 * time.Millisecond)

	final, ok := orch.Artifacts.FindByID(memory.ArtifactID("readme_final"))
	if !ok {
		orch.Logger.Error("Failed to find readme_final artifact")
		return
	}
	
	fmt.Println("======================================")
	fmt.Println("FINAL README:")
	fmt.Println("======================================")
	fmt.Println(final.Data.(string))
	fmt.Println("======================================")

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
	newArt, err := orch.Artifacts.UpdateVersion(memory.ArtifactID("readme_final"), "# HyperParallel V2\nEven better!")
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

	orch.Shutdown()
}
