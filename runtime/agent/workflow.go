package agent

type WorkflowYAML struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Version string `yaml:"version"`

	Nodes []struct {
		ID    string `yaml:"id"`
		Agent string `yaml:"agent"`
	} `yaml:"nodes"`

	Edges []struct {
		From string `yaml:"from"`
		To   string `yaml:"to"`
	} `yaml:"edges"`
}

type WorkflowEdge struct {
	From string
	To   string
}

type WorkflowNode struct {
	ID       string
	WorkerID string
}

type WorkflowDefinition struct {
	ID       string
	Name     string
	Version  string
	Nodes    map[string]WorkflowNode
	Edges    []WorkflowEdge
	Roots    []string
	Parents  map[string][]string
	Children map[string][]string
}
