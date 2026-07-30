package main

import (
	"fmt"
	"github.com/hyperparallel/runtime/agent"
	"github.com/hyperparallel/runtime/memory"
	"github.com/hyperparallel/runtime/orchestrator"
	"time"
)

func main() {
	orch := orchestrator.New()
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

	orch.Shutdown()
}
