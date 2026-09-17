package agent

type WorkflowYAML struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Version string `yaml:"version"`

	Nodes []struct {
		ID         string         `yaml:"id"`
		Agent      string         `yaml:"agent"`
		Modality   string         `yaml:"modality"`
		Parameters map[string]any `yaml:"parameters"`
	} `yaml:"nodes"`

	Edges []struct {
		From string `yaml:"from"`
		To   string `yaml:"to"`
	} `yaml:"edges"`
}

type WorkflowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type WorkflowNode struct {
	ID         string         `json:"id"`
	WorkerID   string         `json:"worker_id"`
	Modality   string         `json:"modality,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type WorkflowDefinition struct {
	ID       string                  `json:"id"`
	Name     string                  `json:"name,omitempty"`
	Version  string                  `json:"version,omitempty"`
	Nodes    map[string]WorkflowNode `json:"nodes"`
	Edges    []WorkflowEdge          `json:"edges,omitempty"`
	Roots    []string                `json:"roots"`
	Parents  map[string][]string     `json:"parents,omitempty"`
	Children map[string][]string     `json:"children,omitempty"`
}
