package agent

type WorkflowEdge struct {
	From string // Source NodeID
	To   string // Target NodeID
}

type WorkflowNode struct {
	ID       string
	WorkerID string
	Type     string
}

type Workflow struct {
	ID    string
	Name  string
	Entry string // The root node ID to kick off execution

	Nodes map[string]WorkflowNode
	Edges []WorkflowEdge
}
