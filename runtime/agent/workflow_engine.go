package agent

import (
	"fmt"
	"strings"

	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
	"github.com/hyperparallel/runtime/memory"
)

type WorkflowEngine struct {
	Logger     *logger.Logger
	Bus        *events.Bus
	Executions map[string]*WorkflowExecution
}

func NewWorkflowEngine(l *logger.Logger, b *events.Bus) *WorkflowEngine {
	return &WorkflowEngine{
		Logger:     l,
		Bus:        b,
		Executions: make(map[string]*WorkflowExecution),
	}
}

func (we *WorkflowEngine) Start() {
	we.Bus.Subscribe(events.EventType("ArtifactStored"), func(e events.RuntimeEvent) {
		artifact, ok := e.Payload.(*memory.Artifact)
		if !ok || artifact.Task == "" {
			return
		}

		taskID := TaskID(artifact.Task)
		execID, nodeID := we.parseTaskID(taskID)
		if execID == "" || nodeID == "" {
			return
		}

		exec, exists := we.Executions[execID]
		if !exists {
			return // Execution not found or already archived
		}

		// 1. Mark node complete and cache the artifact for downstream nodes
		exec.NodeStates[nodeID] = NodeDone
		exec.Artifacts[nodeID] = artifact
		we.Logger.Info("WorkflowEngine node completed", "exec_id", execID, "node_id", nodeID)

		// 2. Check all successors using optimized children map
		for _, successorID := range exec.Workflow.Children[nodeID] {
			we.checkAndDispatch(exec, successorID)
		}

		// 3. Check if workflow is complete
		allDone := true
		for _, state := range exec.NodeStates {
			if state != NodeDone {
				allDone = false
				break
			}
		}

		if allDone {
			we.Bus.Publish(events.EventType("WorkflowCompleted"), events.Component("workflow_engine"), map[string]any{
				"workflow":  exec.Workflow.ID,
				"execution": exec.ExecutionID,
			})
		}
	})

	we.Bus.Subscribe(events.EventType("WorkerFailed"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		var taskID TaskID
		switch v := payload["task_id"].(type) {
		case string:
			taskID = TaskID(v)
		case TaskID:
			taskID = v
		default:
			return
		}

		execID, nodeID := we.parseTaskID(taskID)
		if execID == "" || nodeID == "" {
			return
		}

		exec, exists := we.Executions[execID]
		if !exists {
			return
		}

		// 1. Mark node failed
		exec.NodeStates[nodeID] = NodeFailed
		we.Logger.Error("WorkflowEngine node failed", "exec_id", execID, "node_id", nodeID, "reason", payload["reason"])
		
		// 2. Emit TaskFailed
		we.Bus.Publish(events.EventType("TaskFailed"), events.Component("workflow_engine"), map[string]any{
			"task_id":   taskID,
			"node_id":   nodeID,
			"exec_id":   execID,
			"workflow":  exec.Workflow.ID,
		})

		// 3. Simple fail-fast workflow policy
		we.Logger.Error("WorkflowEngine workflow failed", "exec_id", execID, "workflow_id", exec.Workflow.ID)
		we.Bus.Publish(events.EventType("WorkflowFailed"), events.Component("workflow_engine"), map[string]any{
			"execution": execID,
			"workflow":  exec.Workflow.ID,
			"reason":    "node_failure",
			"node_id":   nodeID,
		})
	})
}

func (we *WorkflowEngine) parseTaskID(taskID TaskID) (execID string, nodeID string) {
	parts := strings.SplitN(string(taskID), "|", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (we *WorkflowEngine) SubmitWorkflow(wf *WorkflowDefinition, executionID string) error {
	exec := NewWorkflowExecution(executionID, wf)
	we.Executions[executionID] = exec

	we.Logger.Info("WorkflowEngine started execution", "workflow_id", wf.ID, "exec_id", executionID)

	for _, rootID := range wf.Roots {
		we.dispatchNode(exec, rootID, nil)
	}
	return nil
}

func (we *WorkflowEngine) checkAndDispatch(exec *WorkflowExecution, nodeID string) {
	if exec.NodeStates[nodeID] != NodePending {
		return
	}

	var inputs []TaskInput
	for _, parentID := range exec.Workflow.Parents[nodeID] {
		if exec.NodeStates[parentID] != NodeDone {
			return // Still waiting for this incoming dependency
		}
		
		// Plumb the artifact produced by the dependency into the task input
		if art, exists := exec.Artifacts[parentID]; exists {
			inputs = append(inputs, TaskInput{
				ArtifactID: string(art.ID),
				Version:    int(art.Version),
				Name:       art.Name,
				Data:       art.Data,
			})
		}
	}

	we.dispatchNode(exec, nodeID, inputs)
}

func (we *WorkflowEngine) dispatchNode(exec *WorkflowExecution, nodeID string, inputs []TaskInput) {
	node := exec.Workflow.Nodes[nodeID]
	exec.NodeStates[nodeID] = NodeRunning

	we.Bus.Publish(events.EventType("NodeReady"), events.Component("workflow_engine"), map[string]any{
		"node_id":   nodeID,
		"exec_id":   exec.ExecutionID,
		"workflow":  exec.Workflow.ID,
	})

	task := Task{
		ID:          TaskID(fmt.Sprintf("%s|%s", exec.ExecutionID, node.ID)),
		AgentID:     node.WorkerID,
		Inputs:      inputs,
		Workflow:    exec.Workflow.ID,
		ExecutionID: exec.ExecutionID,
	}

	we.Bus.Publish(events.EventType("TaskCreated"), events.Component("workflow_engine"), task)
}
