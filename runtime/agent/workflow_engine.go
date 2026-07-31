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
		art, ok := e.Payload.(*memory.Artifact)
		if !ok {
			return
		}

		// Map the artifact's original task ID back to a Workflow Execution and Node
		// We format our workflow tasks as "executionID|nodeID"
		parts := strings.Split(art.Task, "|")
		if len(parts) != 2 {
			return // Not a workflow-generated task (likely a standalone subscription)
		}
		
		execID := parts[0]
		nodeID := parts[1]

		exec, ok := we.Executions[execID]
		if !ok {
			return // Execution not found or already archived
		}

		// 1. Mark node complete and cache the artifact for downstream nodes
		exec.NodeStates[nodeID] = NodeDone
		exec.Artifacts[nodeID] = art
		we.Logger.Info("WorkflowEngine node completed", "exec_id", execID, "node_id", nodeID)

		// 2. Check all successors
		for _, edge := range exec.Workflow.Edges {
			if edge.From == nodeID {
				successorID := edge.To
				we.checkAndDispatch(exec, successorID)
			}
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
}

func (we *WorkflowEngine) SubmitWorkflow(wf *Workflow, executionID string) error {
	exec := NewWorkflowExecution(executionID, wf)
	we.Executions[executionID] = exec

	we.Logger.Info("WorkflowEngine started execution", "workflow_id", wf.ID, "exec_id", executionID)

	entryNode, ok := wf.Nodes[wf.Entry]
	if !ok {
		return fmt.Errorf("entry node %s not found in workflow", wf.Entry)
	}

	we.dispatchNode(exec, entryNode.ID, nil)
	return nil
}

func (we *WorkflowEngine) checkAndDispatch(exec *WorkflowExecution, nodeID string) {
	if exec.NodeStates[nodeID] != NodePending {
		return
	}

	var inputs []TaskInput
	for _, edge := range exec.Workflow.Edges {
		if edge.To == nodeID {
			if exec.NodeStates[edge.From] != NodeDone {
				return // Still waiting for this incoming dependency
			}
			
			// Plumb the artifact produced by the dependency into the task input
			if art, exists := exec.Artifacts[edge.From]; exists {
				inputs = append(inputs, TaskInput{
					ArtifactID: string(art.ID),
					Version:    int(art.Version),
					Name:       art.Name,
					Data:       art.Data,
				})
			}
		}
	}

	we.dispatchNode(exec, nodeID, inputs)
}

func (we *WorkflowEngine) dispatchNode(exec *WorkflowExecution, nodeID string, inputs []TaskInput) {
	node := exec.Workflow.Nodes[nodeID]
	exec.NodeStates[nodeID] = NodeRunning

	task := Task{
		ID:          TaskID(fmt.Sprintf("%s|%s", exec.ExecutionID, node.ID)),
		AgentID:     node.WorkerID,
		Type:        node.Type,
		Inputs:      inputs,
		Workflow:    exec.Workflow.ID,
		ExecutionID: exec.ExecutionID,
	}

	we.Bus.Publish(events.EventType("TaskReady"), events.Component("workflow_engine"), task)
}
