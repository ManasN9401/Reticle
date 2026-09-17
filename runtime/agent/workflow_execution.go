package agent

import (
	"encoding/json"
	"github.com/reticle/runtime/memory"
	"time"
)

type NodeState string

const (
	NodePending     NodeState = "pending"
	NodeRunning     NodeState = "running"
	NodeDone        NodeState = "done"
	NodeFailed      NodeState = "failed"
	NodeBlocked     NodeState = "blocked"
	NodeInterrupted NodeState = "interrupted"
)

type ExecutionState string

const (
	ExecutionSucceeded   ExecutionState = "completed"
	ExecutionFailed      ExecutionState = "failed"
	ExecutionRunning     ExecutionState = "running"
	ExecutionPaused      ExecutionState = "paused"
	ExecutionCancelled   ExecutionState = "cancelled"
	ExecutionInterrupted ExecutionState = "interrupted"
)

type AttemptState string

const (
	AttemptRunning     AttemptState = "running"
	AttemptSucceeded   AttemptState = "succeeded"
	AttemptFailed      AttemptState = "failed"
	AttemptInterrupted AttemptState = "interrupted"
)

type AttemptRecord struct {
	ID        string       `json:"id"`
	TaskID    TaskID       `json:"task_id"`
	NodeID    string       `json:"node_id"`
	Number    int          `json:"number"`
	Model     string       `json:"model,omitempty"`
	State     AttemptState `json:"state"`
	Reason    string       `json:"reason,omitempty"`
	StartedAt time.Time    `json:"started_at"`
	EndedAt   time.Time    `json:"ended_at,omitempty"`
}

type WorkflowExecution struct {
	ExecutionID   string                      `json:"execution_id"`
	Workflow      *WorkflowDefinition         `json:"workflow"`
	Status        ExecutionState              `json:"status"`
	NodeStates    map[string]NodeState        `json:"node_states"`
	Artifacts     map[string]*memory.Artifact `json:"artifacts,omitempty"`
	Attempts      map[string][]AttemptRecord  `json:"attempts,omitempty"`
	ActiveAttempt map[string]string           `json:"active_attempt,omitempty"`
	GraphRevision uint64                      `json:"graph_revision"`
	MaxNodes      int                         `json:"max_nodes"`
	CreatedAt     time.Time                   `json:"created_at"`
	UpdatedAt     time.Time                   `json:"updated_at"`
}

func NewWorkflowExecution(id string, wf *WorkflowDefinition) *WorkflowExecution {
	var own WorkflowDefinition
	data, err := json.Marshal(wf)
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(data, &own); err != nil {
		panic(err)
	}
	if own.Parents == nil {
		own.Parents = make(map[string][]string)
	}
	if own.Children == nil {
		own.Children = make(map[string][]string)
	}
	if own.Edges == nil {
		own.Edges = make([]WorkflowEdge, 0)
	}
	wf = &own
	states := make(map[string]NodeState)
	for nodeID := range wf.Nodes {
		states[nodeID] = NodePending
	}
	return &WorkflowExecution{
		ExecutionID:   id,
		Workflow:      wf,
		Status:        ExecutionRunning,
		NodeStates:    states,
		Artifacts:     make(map[string]*memory.Artifact),
		Attempts:      make(map[string][]AttemptRecord),
		ActiveAttempt: make(map[string]string),
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}
