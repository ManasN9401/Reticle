package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
)

type GraphEngine struct {
	mu         sync.Mutex
	Logger     *logger.Logger
	Bus        *events.Bus
	Executions map[string]*WorkflowExecution
	cancelled  map[string]bool
	paused     map[string]bool
}

func NewGraphEngine(l *logger.Logger, b *events.Bus) *GraphEngine {
	return &GraphEngine{
		Logger:     l,
		Bus:        b,
		Executions: make(map[string]*WorkflowExecution),
		cancelled:  make(map[string]bool),
		paused:     make(map[string]bool),
	}
}

func (we *GraphEngine) Start() {
	we.subscribe("RuntimeOverloaded", func(events.RuntimeEvent) {
		for id, execution := range we.Executions {
			if execution.Status == ExecutionRunning || execution.Status == ExecutionPaused {
				execution.Status = ExecutionFailed
				we.cancelled[id] = true
				for node, state := range execution.NodeStates {
					if state != NodeDone {
						execution.NodeStates[node] = NodeFailed
					}
				}
			}
		}
	})
	we.subscribe(events.EventType("GraphMutationRequested"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		taskID := TaskID(fmt.Sprint(payload["task_id"]))
		execID, supervisorNodeID := we.parseTaskID(taskID)
		mutation, _ := payload["mutation"].(*GraphMutation)

		exec, exists := we.Executions[execID]
		if !exists || mutation == nil || exec.Status != ExecutionRunning || len(exec.Workflow.Nodes) > 126 {
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
				supervisorAgentID := WorkerID(fmt.Sprint(payload["worker_id"]))
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

			// Completion is handled only after all result mutations are committed.

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

		if exec.Status != ExecutionRunning && exec.Status != ExecutionPaused {
			return
		}
		exec.Artifacts[nodeID] = artifact
	}

	we.subscribe(events.EventType("ArtifactsProduced"), handleArtifact)
	we.subscribe(events.EventType("WorkerCompleted"), func(e events.RuntimeEvent) {
		p, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		execID, nodeID := we.parseTaskID(TaskID(fmt.Sprint(p["task_id"])))
		ex := we.Executions[execID]
		if ex == nil || (ex.Status != ExecutionRunning && ex.Status != ExecutionPaused) || ex.NodeStates[nodeID] != NodeRunning {
			return
		}
		ex.NodeStates[nodeID] = NodeDone
		for _, id := range ex.Workflow.Children[nodeID] {
			we.checkAndDispatch(ex, id)
		}
		for _, state := range ex.NodeStates {
			if state != NodeDone {
				return
			}
		}
		ex.Status = ExecutionSucceeded
		we.Bus.Publish("WorkflowCompleted", "graph_engine", map[string]any{"workflow": ex.Workflow.ID, "execution": execID})
	})

	we.subscribe(events.EventType("WorkerFailed"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		taskID := fmt.Sprint(payload["task_id"])
		execID, nodeID := we.parseTaskID(TaskID(taskID))
		if execID == "" || nodeID == "" {
			return
		}

		exec, exists := we.Executions[execID]
		if !exists {
			return
		}

		if exec.Status != ExecutionRunning && exec.Status != ExecutionPaused {
			return
		}
		exec.Status = ExecutionFailed
		we.Bus.Publish("ExecutionKilled", "graph_engine", map[string]string{"execution": execID})
		// 1. Mark node failed
		exec.NodeStates[nodeID] = NodeFailed
		we.Logger.Error("GraphEngine node failed", "exec_id", execID, "node_id", nodeID, "reason", payload["reason"])

		// 2. Emit TaskFailed
		we.Bus.Publish(events.EventType("TaskFailed"), events.Component("graph_engine"), map[string]any{
			"task_id":  taskID,
			"node_id":  nodeID,
			"exec_id":  execID,
			"workflow": exec.Workflow.ID,
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

	we.subscribe(events.EventType("WorkflowStateRequested"), func(e events.RuntimeEvent) {
		for execID, exec := range we.Executions {
			states := make(map[string]NodeState)
			for id, state := range exec.NodeStates {
				states[id] = state
			}
			we.Bus.Publish(events.EventType("WorkflowSnapshot"), events.Component("graph_engine"), map[string]any{
				"workflow_id": exec.Workflow.ID,
				"exec_id":     execID,
				"edges":       exec.Workflow.Edges,
				"status":      exec.Status,
				"nodes":       states,
			})
		}
	})

	we.subscribe(events.EventType("ExecutionKilled"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]string); ok {
			if execID, ok := payload["execution"]; ok {
				if !strings.HasPrefix(execID, "compile-") {
					we.Bus.Publish("ExecutionKilled", "graph_engine", map[string]string{"execution": "compile-" + execID})
				}

				we.cancelled[execID] = true
				we.cancelled["compile-"+execID] = true
				if exec, exists := we.Executions[execID]; exists {
					if exec.Status == ExecutionRunning || exec.Status == ExecutionPaused {
						exec.Status = ExecutionCancelled
					}
					we.Logger.Info("GraphEngine marked execution as cancelled", "exec_id", execID)
				}
			}
		}
	})

	we.subscribe(events.EventType("ExecutionPaused"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]string); ok {
			if execID, ok := payload["execution"]; ok {
				if !strings.HasPrefix(execID, "compile-") {
					we.Bus.Publish("ExecutionPaused", "graph_engine", map[string]string{"execution": "compile-" + execID})
				}
				we.paused[execID] = true

				if exec, exists := we.Executions[execID]; exists {
					if exec.Status == ExecutionRunning {
						exec.Status = ExecutionPaused
					}
					we.Logger.Info("GraphEngine marked execution as paused", "exec_id", execID)
				}
			}
		}
	})

	we.subscribe(events.EventType("ExecutionResumed"), func(e events.RuntimeEvent) {
		if payload, ok := e.Payload.(map[string]string); ok {
			if execID, ok := payload["execution"]; ok {
				if !strings.HasPrefix(execID, "compile-") {
					we.Bus.Publish("ExecutionResumed", "graph_engine", map[string]string{"execution": "compile-" + execID})
				}
				we.paused[execID] = false

				if exec, exists := we.Executions[execID]; exists {
					if exec.Status != ExecutionPaused {
						return
					}
					exec.Status = ExecutionRunning
					we.Logger.Info("GraphEngine marked execution as resumed", "exec_id", execID)
					// Kickstart any pending nodes that were waiting for resume
					for _, rootID := range exec.Workflow.Roots {
						we.checkAndDispatch(exec, rootID)
					}
					// Also kickstart all children of completed nodes
					for nodeID, state := range exec.NodeStates {
						if state == NodeDone {
							for _, successorID := range exec.Workflow.Children[nodeID] {
								we.checkAndDispatch(exec, successorID)
							}
						}
					}
				}
			}
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
	we.mu.Lock()
	defer we.mu.Unlock()
	if !we.Bus.Accepting() {
		return fmt.Errorf("runtime is closed or overloaded; restart required")
	}
	if we.cancelled[executionID] {
		return fmt.Errorf("execution was cancelled: %s", executionID)
	}
	if wf == nil || executionID == "" || len(wf.Nodes) == 0 || len(wf.Nodes) > 128 {
		return fmt.Errorf("workflow and execution ID required")
	}
	if _, exists := we.Executions[executionID]; exists {
		return fmt.Errorf("execution already exists: %s", executionID)
	}
	exec := NewWorkflowExecution(executionID, wf)
	if we.paused[executionID] {
		exec.Status = ExecutionPaused
	}
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
	if exec.Status != ExecutionRunning {
		return // Execution is paused or cancelled, do not dispatch new nodes
	}

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
		"node_id":  nodeID,
		"exec_id":  exec.ExecutionID,
		"workflow": exec.Workflow.ID,
	})

	task := Task{
		ID:          TaskID(fmt.Sprintf("%s|%s", exec.ExecutionID, node.ID)),
		AgentID:     node.WorkerID,
		Inputs:      inputs,
		Parameters:  node.Parameters,
		Modality:    node.Modality,
		Workflow:    exec.Workflow.ID,
		ExecutionID: exec.ExecutionID,
	}

	we.Bus.Publish(events.EventType("TaskCreated"), events.Component("graph_engine"), task)
}

func (we *GraphEngine) subscribe(t events.EventType, h events.Handler) {
	we.Bus.Subscribe(t, func(e events.RuntimeEvent) { we.mu.Lock(); defer we.mu.Unlock(); h(e) })
}
