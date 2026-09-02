package agent

import (
	"fmt"
	"strings"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
)

type GraphEngine struct {
	Logger     *logger.Logger
	Bus        *events.Bus
	Executions map[string]*WorkflowExecution
}

func NewGraphEngine(l *logger.Logger, b *events.Bus) *GraphEngine {
	return &GraphEngine{
		Logger:     l,
		Bus:        b,
		Executions: make(map[string]*WorkflowExecution),
	}
}

func (we *GraphEngine) Start() {
	we.Bus.Subscribe(events.EventType("GraphMutationRequested"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		taskID := payload["task_id"].(TaskID)
		execID, supervisorNodeID := we.parseTaskID(taskID)
		mutation, _ := payload["mutation"].(*GraphMutation)

		exec, exists := we.Executions[execID]
		if !exists || mutation == nil {
			return
		}

		we.Logger.Info("GraphEngine received mutation request", "exec_id", execID, "action", mutation.Action)

		if mutation.Action == "delegate" {
			iterCount := len(exec.Workflow.Nodes) + 1
			dynamicWorkerNode := fmt.Sprintf("dyn-%s-%d", mutation.TargetAgent, iterCount)

			// Add dynamic worker node to the DAG
			exec.Workflow.Nodes[dynamicWorkerNode] = WorkflowNode{
				ID:       dynamicWorkerNode,
				WorkerID: mutation.TargetAgent,
			}
			exec.NodeStates[dynamicWorkerNode] = NodePending

			// Link: Supervisor -> Worker
			exec.Workflow.Edges = append(exec.Workflow.Edges, WorkflowEdge{From: supervisorNodeID, To: dynamicWorkerNode})
			exec.Workflow.Children[supervisorNodeID] = append(exec.Workflow.Children[supervisorNodeID], dynamicWorkerNode)
			exec.Workflow.Parents[dynamicWorkerNode] = append(exec.Workflow.Parents[dynamicWorkerNode], supervisorNodeID)

			if mutation.ReturnToSupervisor {
				supervisorAgentID := payload["worker_id"].(WorkerID)
				dynamicSuperNode := fmt.Sprintf("dyn-%s-%d", string(supervisorAgentID), iterCount+1)
				exec.Workflow.Nodes[dynamicSuperNode] = WorkflowNode{
					ID:       dynamicSuperNode,
					WorkerID: string(supervisorAgentID),
				}
				exec.NodeStates[dynamicSuperNode] = NodePending

				// Link: Worker -> Supervisor (Iter 2)
				exec.Workflow.Edges = append(exec.Workflow.Edges, WorkflowEdge{From: dynamicWorkerNode, To: dynamicSuperNode})
				exec.Workflow.Children[dynamicWorkerNode] = append(exec.Workflow.Children[dynamicWorkerNode], dynamicSuperNode)
				exec.Workflow.Parents[dynamicSuperNode] = append(exec.Workflow.Parents[dynamicSuperNode], dynamicWorkerNode)
			}

			// Mark supervisor node as done and dispatch the new dynamic worker
			exec.NodeStates[supervisorNodeID] = NodeDone
			we.Logger.Info("GraphEngine dynamically injected sub-graph", "worker_node", dynamicWorkerNode)

			for _, successorID := range exec.Workflow.Children[supervisorNodeID] {
				we.checkAndDispatch(exec, successorID)
			}
		}
	})

	handleArtifact := func(e events.RuntimeEvent) {
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
		we.Logger.Info("GraphEngine node completed", "exec_id", execID, "node_id", nodeID)

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

		// ONLY emit if we are actually done, and not if we are about to mutate
		if allDone {
			// Small heuristic: if there is an active mutation pending, this might false-trigger.
			// But since node IDs are injected dynamically, we assume the graph is only done if all known nodes are NodeDone.
			we.Bus.Publish(events.EventType("WorkflowCompleted"), events.Component("graph_engine"), map[string]any{
				"workflow":  exec.Workflow.ID,
				"execution": exec.ExecutionID,
			})
		}
	}

	we.Bus.Subscribe(events.EventType("ArtifactStored"), handleArtifact)
	we.Bus.Subscribe(events.EventType("ArtifactVersionCreated"), handleArtifact)

	we.Bus.Subscribe(events.EventType("WorkerFailed"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		taskID, ok := payload["task_id"].(string)
		if !ok {
			return
		}
		execID, nodeID := we.parseTaskID(TaskID(taskID))
		if execID == "" || nodeID == "" {
			return
		}

		exec, exists := we.Executions[execID]
		if !exists {
			return
		}

		// 1. Mark node failed
		exec.NodeStates[nodeID] = NodeFailed
		we.Logger.Error("GraphEngine node failed", "exec_id", execID, "node_id", nodeID, "reason", payload["reason"])
		
		// 2. Emit TaskFailed
		we.Bus.Publish(events.EventType("TaskFailed"), events.Component("graph_engine"), map[string]any{
			"task_id":   taskID,
			"node_id":   nodeID,
			"exec_id":   execID,
			"workflow":  exec.Workflow.ID,
		})

		// 3. Simple fail-fast workflow policy
		we.Logger.Error("GraphEngine workflow failed", "exec_id", execID, "workflow_id", exec.Workflow.ID)
		we.Bus.Publish(events.EventType("WorkflowFailed"), events.Component("graph_engine"), map[string]any{
			"execution": execID,
			"workflow":  exec.Workflow.ID,
			"reason":    "node_failure",
			"node_id":   nodeID,
		})
	})

	we.Bus.Subscribe(events.EventType("WorkflowStateRequested"), func(e events.RuntimeEvent) {
		for execID, exec := range we.Executions {
			we.Bus.Publish(events.EventType("WorkflowStarted"), events.Component("graph_engine"), map[string]any{
				"workflow_id": exec.Workflow.ID,
				"exec_id":     execID,
				"edges":       exec.Workflow.Edges,
			})
		}
	})
}

func (we *GraphEngine) parseTaskID(taskID TaskID) (execID string, nodeID string) {
	parts := strings.SplitN(string(taskID), "|", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (we *GraphEngine) SubmitWorkflow(wf *WorkflowDefinition, executionID string) error {
	exec := NewWorkflowExecution(executionID, wf)
	we.Executions[executionID] = exec

	we.Logger.Info("GraphEngine started execution", "workflow_id", wf.ID, "exec_id", executionID)
	
	we.Bus.Publish(events.EventType("WorkflowStarted"), events.Component("graph_engine"), map[string]any{
		"workflow_id": wf.ID,
		"exec_id":     executionID,
		"edges":       wf.Edges,
	})

	for _, rootID := range wf.Roots {
		we.dispatchNode(exec, rootID, nil)
	}
	return nil
}

func (we *GraphEngine) checkAndDispatch(exec *WorkflowExecution, nodeID string) {
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

func (we *GraphEngine) dispatchNode(exec *WorkflowExecution, nodeID string, inputs []TaskInput) {
	node := exec.Workflow.Nodes[nodeID]
	exec.NodeStates[nodeID] = NodeRunning

	we.Bus.Publish(events.EventType("NodeReady"), events.Component("graph_engine"), map[string]any{
		"node_id":   nodeID,
		"exec_id":   exec.ExecutionID,
		"workflow":  exec.Workflow.ID,
	})

	task := Task{
		ID:          TaskID(fmt.Sprintf("%s|%s", exec.ExecutionID, node.ID)),
		AgentID:     node.WorkerID,
		Inputs:      inputs,
		Parameters:  node.Parameters,
		Workflow:    exec.Workflow.ID,
		ExecutionID: exec.ExecutionID,
	}

	we.Bus.Publish(events.EventType("TaskCreated"), events.Component("graph_engine"), task)
}
