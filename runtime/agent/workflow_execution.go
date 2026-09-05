package agent

import "github.com/reticle/runtime/memory"

type NodeState string

const (
	NodePending NodeState = "pending"
	NodeRunning NodeState = "running"
	NodeDone    NodeState = "done"
	NodeFailed  NodeState = "failed"
)

type ExecutionState string

const (
	ExecutionRunning   ExecutionState = "running"
	ExecutionPaused    ExecutionState = "paused"
	ExecutionCancelled ExecutionState = "cancelled"
)

type WorkflowExecution struct {
	ExecutionID string
	Workflow    *WorkflowDefinition
	Status      ExecutionState
	NodeStates  map[string]NodeState
	Artifacts   map[string]*memory.Artifact // Caches artifacts produced by each node for passing to successors
}

func NewWorkflowExecution(id string, wf *WorkflowDefinition) *WorkflowExecution {
	states := make(map[string]NodeState)
	for nodeID := range wf.Nodes {
		states[nodeID] = NodePending
	}
	return &WorkflowExecution{
		ExecutionID: id,
		Workflow:    wf,
		Status:      ExecutionRunning,
		NodeStates:  states,
		Artifacts:   make(map[string]*memory.Artifact),
	}
}
