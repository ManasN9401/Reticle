package main

import (
	"fmt"
	"github.com/hyperparallel/runtime/agent"
	"github.com/hyperparallel/runtime/orchestrator"
)

func main() {
	orch := orchestrator.New()
	orch.Start()

	sup := agent.NewSupervisor(orch.Logger, orch.Bus, orch.Memory)

	outlineWorker := agent.NewWorker("outline-gen", "python", []string{"outline_agent.py"}, orch.Logger, orch.Bus)
	wordingWorker := agent.NewWorker("wording-imp", "python", []string{"wording_agent.py"}, orch.Logger, orch.Bus)

	err := sup.AssignTask(outlineWorker, agent.TaskRequest{
		ID:      "task-1",
		Type:    "generate_outline",
		Payload: "create an outline for a software project",
	}, "readme_outline")
	
	if err != nil {
		orch.Logger.Error("Failed to generate outline: %v", err)
		return
	}

	rawOutline, _ := orch.Memory.Get("readme_outline")
	outlineStr := rawOutline.(string)

	err = sup.AssignTask(wordingWorker, agent.TaskRequest{
		ID:      "task-2",
		Type:    "improve_wording",
		Payload: outlineStr,
	}, "readme_final")

	if err != nil {
		orch.Logger.Error("Failed to improve wording: %v", err)
		return
	}

	final, _ := orch.Memory.Get("readme_final")
	
	fmt.Println("======================================")
	fmt.Println("FINAL README:")
	fmt.Println("======================================")
	fmt.Println(final.(string))
	fmt.Println("======================================")

	orch.Shutdown()
}
