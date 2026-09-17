package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
)

type GraphEngine struct {
	mu           sync.Mutex
	Logger       *logger.Logger
	Bus          *events.Bus
	Executions   map[string]*WorkflowExecution
	cancelled    map[string]bool
	paused       map[string]bool
	store        *ExecutionStore
	workerExists func(string, string) bool
	maxNodes     int
}

func NewGraphEngine(l *logger.Logger, b *events.Bus) *GraphEngine {
	maxNodes := 64
	if configured, err := strconv.Atoi(os.Getenv("RETICLE_MAX_GRAPH_NODES")); err == nil && configured > 0 {
		maxNodes = configured
	}
	if maxNodes > 128 {
		maxNodes = 128
	}
	return &GraphEngine{
		Logger:     l,
		Bus:        b,
		Executions: make(map[string]*WorkflowExecution),
		cancelled:  make(map[string]bool),
		paused:     make(map[string]bool),
		maxNodes:   maxNodes,
	}
}

func NewPersistentGraphEngine(l *logger.Logger, b *events.Bus, path string) (*GraphEngine, error) {
	engine := NewGraphEngine(l, b)
	engine.store = NewExecutionStore(path)
	snapshot, err := engine.store.Load()
	if err != nil {
		return nil, err
	}
	engine.Executions = snapshot.Executions
	engine.cancelled = snapshot.Cancelled
	engine.paused = snapshot.Paused
	recovered := false
	for _, execution := range engine.Executions {
		executionRecovered := false
		if execution.Attempts == nil {
			execution.Attempts = make(map[string][]AttemptRecord)
		}
		if execution.ActiveAttempt == nil {
			execution.ActiveAttempt = make(map[string]string)
		}
		if execution.MaxNodes == 0 {
			execution.MaxNodes = engine.maxNodes
		}
		if execution.Workflow.Parents == nil {
			execution.Workflow.Parents = make(map[string][]string)
		}
		if execution.Workflow.Children == nil {
			execution.Workflow.Children = make(map[string][]string)
		}
		if execution.Status == ExecutionRunning {
			execution.Status = ExecutionInterrupted
			executionRecovered = true
		}
		for nodeID, state := range execution.NodeStates {
			if state == NodeRunning {
				execution.NodeStates[nodeID] = NodeInterrupted
				executionRecovered = true
			}
		}
		for nodeID, id := range execution.ActiveAttempt {
			attempts := execution.Attempts[nodeID]
			for i := range attempts {
				if attempts[i].ID == id && attempts[i].State == AttemptRunning {
					attempts[i].State = AttemptInterrupted
					attempts[i].Reason = "runtime_restart"
					attempts[i].EndedAt = time.Now().UTC()
				}
			}
			execution.Attempts[nodeID] = attempts
		}
		if executionRecovered {
			execution.UpdatedAt = time.Now().UTC()
			recovered = true
		}
	}
	if recovered {
		if err := engine.persistLocked(); err != nil {
			return nil, err
		}
	}
	return engine, nil
}

// SetWorkerValidator installs the registry/dispatcher admission check used by
// dynamic graph mutations. Static workflow loading already validates workers.
func (we *GraphEngine) SetWorkerValidator(validate func(execution, worker string) bool) {
	we.mu.Lock()
	defer we.mu.Unlock()
	we.workerExists = validate
}

func (we *GraphEngine) persistLocked() error {
	if we.store == nil {
		return nil
	}
	return we.store.Save(we.Executions, we.cancelled, we.paused)
}

func (we *GraphEngine) persistReported() bool {
	if err := we.persistLocked(); err != nil {
		we.Logger.Error("Execution state persistence failed; active work interrupted", "error", err)
		for id, execution := range we.Executions {
			if execution.Status == ExecutionRunning || execution.Status == ExecutionPaused {
				execution.Status = ExecutionInterrupted
				we.cancelled[id] = true
			}
		}
		we.Bus.Publish("RuntimePersistenceFailed", "graph_engine", map[string]any{"error": err.Error(), "restart_required": true})
		return false
	}
	return true
}

func (we *GraphEngine) touch(execution *WorkflowExecution) {
	execution.UpdatedAt = time.Now().UTC()
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
		we.persistReported()
	})
	we.subscribe(events.EventType("AttemptStarted"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		execID, nodeID := we.parseTaskID(TaskID(fmt.Sprint(payload["task_id"])))
		execution := we.Executions[execID]
		attemptID := fmt.Sprint(payload["attempt_id"])
		if execution == nil || attemptID == "" || execution.Status != ExecutionRunning {
			return
		}
		number, _ := payload["number"].(int)
		execution.Attempts[nodeID] = append(execution.Attempts[nodeID], AttemptRecord{
			ID: attemptID, TaskID: TaskID(fmt.Sprint(payload["task_id"])), NodeID: nodeID,
			Number: number, Model: fmt.Sprint(payload["model"]), State: AttemptRunning, StartedAt: time.Now().UTC(),
		})
		execution.ActiveAttempt[nodeID] = attemptID
		we.touch(execution)
		we.persistReported()
	})
	we.subscribe(events.EventType("AttemptFinished"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		execID, nodeID := we.parseTaskID(TaskID(fmt.Sprint(payload["task_id"])))
		execution := we.Executions[execID]
		if execution == nil {
			return
		}
		attemptID := fmt.Sprint(payload["attempt_id"])
		attempts := execution.Attempts[nodeID]
		for i := range attempts {
			if attempts[i].ID == attemptID && attempts[i].State == AttemptRunning {
				if fmt.Sprint(payload["outcome"]) == "succeeded" {
					attempts[i].State = AttemptSucceeded
				} else {
					attempts[i].State = AttemptFailed
				}
				attempts[i].Reason = fmt.Sprint(payload["reason"])
				attempts[i].EndedAt = time.Now().UTC()
			}
		}
		execution.Attempts[nodeID] = attempts
		we.touch(execution)
		we.persistReported()
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
		attemptID := fmt.Sprint(payload["attempt_id"])
		additionalNodes := 1
		if mutation != nil && mutation.ReturnToSupervisor {
			additionalNodes = 2
		}
		if !exists || mutation == nil || mutation.Action != "delegate" || exec.Status != ExecutionRunning ||
			attemptID == "" || exec.ActiveAttempt[supervisorNodeID] != attemptID ||
			len(exec.Workflow.Nodes)+additionalNodes > exec.MaxNodes ||
			we.workerExists == nil || !we.workerExists(execID, mutation.TargetAgent) {
			we.Logger.Error("Graph mutation rejected", "exec_id", execID, "target_agent", func() string {
				if mutation == nil {
					return ""
				}
				return mutation.TargetAgent
			}())
			return
		}
		before := cloneWorkflowExecution(exec)

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
			exec.GraphRevision++
			we.touch(exec)
			if err := we.persistLocked(); err != nil {
				we.Executions[execID] = before
				we.Logger.Error("Graph mutation persistence failed", "exec_id", execID, "error", err)
				return
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
		we.touch(exec)
		we.persistReported()
	}

	we.subscribe(events.EventType("ArtifactStored"), handleArtifact)
	we.subscribe(events.EventType("ArtifactVersionCreated"), handleArtifact)
	we.subscribe(events.EventType("WorkerCompleted"), func(e events.RuntimeEvent) {
		p, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		execID, nodeID := we.parseTaskID(TaskID(fmt.Sprint(p["task_id"])))
		ex := we.Executions[execID]
		attemptID := fmt.Sprint(p["attempt_id"])
		if ex == nil || (ex.Status != ExecutionRunning && ex.Status != ExecutionPaused) || ex.NodeStates[nodeID] != NodeRunning || (attemptID != "" && ex.ActiveAttempt[nodeID] != attemptID) {
			return
		}
		ex.NodeStates[nodeID] = NodeDone
		delete(ex.ActiveAttempt, nodeID)
		we.touch(ex)
		if err := we.persistLocked(); err != nil {
			ex.NodeStates[nodeID] = NodeFailed
			ex.Status = ExecutionFailed
			we.Logger.Error("Execution persistence failed", "exec_id", execID, "error", err)
			return
		}
		for _, id := range ex.Workflow.Children[nodeID] {
			we.checkAndDispatch(ex, id)
		}
		for _, state := range ex.NodeStates {
			if state != NodeDone {
				return
			}
		}
		ex.Status = ExecutionSucceeded
		we.touch(ex)
		we.persistReported()
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
		delete(exec.ActiveAttempt, nodeID)
		for pendingNode, state := range exec.NodeStates {
			if state == NodePending {
				exec.NodeStates[pendingNode] = NodeBlocked
			}
		}
		we.touch(exec)
		we.persistReported()
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
				"workflow_id":    exec.Workflow.ID,
				"exec_id":        execID,
				"edges":          exec.Workflow.Edges,
				"status":         exec.Status,
				"nodes":          states,
				"attempts":       exec.Attempts,
				"graph_revision": exec.GraphRevision,
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
					for node, state := range exec.NodeStates {
						if state == NodePending || state == NodeInterrupted {
							exec.NodeStates[node] = NodeBlocked
						}
					}
					we.touch(exec)
					we.Logger.Info("GraphEngine marked execution as cancelled", "exec_id", execID)
				}
				we.persistReported()
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
					we.touch(exec)
					we.Logger.Info("GraphEngine marked execution as paused", "exec_id", execID)
				}
				we.persistReported()
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
					we.touch(exec)
					if err := we.persistLocked(); err != nil {
						exec.Status = ExecutionPaused
						return
					}
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
	we.subscribe(events.EventType("ExecutionReconciled"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]string)
		if !ok {
			return
		}
		exec := we.Executions[payload["execution"]]
		if exec == nil || exec.Status != ExecutionInterrupted {
			return
		}
		switch payload["action"] {
		case "retry":
			exec.Status = ExecutionRunning
			for node, state := range exec.NodeStates {
				if state == NodeInterrupted {
					exec.NodeStates[node] = NodePending
					delete(exec.ActiveAttempt, node)
				}
			}
			we.touch(exec)
			if we.persistLocked() != nil {
				exec.Status = ExecutionInterrupted
				return
			}
			for node, state := range exec.NodeStates {
				if state == NodePending {
					we.checkAndDispatch(exec, node)
				}
			}
		case "fail", "cancel":
			if payload["action"] == "cancel" {
				exec.Status = ExecutionCancelled
			} else {
				exec.Status = ExecutionFailed
			}
			for node, state := range exec.NodeStates {
				if state == NodePending || state == NodeInterrupted {
					exec.NodeStates[node] = NodeBlocked
				}
			}
			we.touch(exec)
			we.persistReported()
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
	if wf == nil || executionID == "" || len(wf.Nodes) == 0 || len(wf.Nodes) > we.maxNodes {
		return fmt.Errorf("workflow and execution ID required")
	}
	if _, exists := we.Executions[executionID]; exists {
		return fmt.Errorf("execution already exists: %s", executionID)
	}
	exec := NewWorkflowExecution(executionID, wf)
	exec.MaxNodes = we.maxNodes
	if we.paused[executionID] {
		exec.Status = ExecutionPaused
	}
	we.Executions[executionID] = exec
	if err := we.persistLocked(); err != nil {
		delete(we.Executions, executionID)
		return fmt.Errorf("persist workflow admission: %w", err)
	}

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
	we.touch(exec)
	if err := we.persistLocked(); err != nil {
		exec.NodeStates[nodeID] = NodeFailed
		exec.Status = ExecutionFailed
		we.Logger.Error("Could not persist node admission", "exec_id", exec.ExecutionID, "node_id", nodeID, "error", err)
		return
	}

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

func cloneWorkflowExecution(execution *WorkflowExecution) *WorkflowExecution {
	data, err := json.Marshal(execution)
	if err != nil {
		panic(err)
	}
	var cloned WorkflowExecution
	if err := json.Unmarshal(data, &cloned); err != nil {
		panic(err)
	}
	return &cloned
}
