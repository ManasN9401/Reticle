package main

import (
	"encoding/json"
	"sort"

	"github.com/reticle/runtime/agent"
)

type architectAgentCatalogEntry struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Runtime      agent.RuntimeType  `json:"runtime"`
	Inputs       []string           `json:"inputs"`
	Outputs      []string           `json:"outputs"`
	Skills       []string           `json:"skills"`
	Capabilities []agent.Capability `json:"capabilities"`
}

type architectSkillCatalogEntry struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	DependencyPolicy string   `json:"dependency_policy"`
	Dependencies     []string `json:"dependencies"`
	EnvVars          []string `json:"env_vars"`
}

type architectToolCatalogEntry struct {
	ID          string `json:"id"`
	Capability  string `json:"capability,omitempty"`
	Description string `json:"description"`
	Constraint  string `json:"constraint,omitempty"`
}

type architectCatalog struct {
	Agents       []architectAgentCatalogEntry `json:"agents"`
	Skills       []architectSkillCatalogEntry `json:"skills"`
	Capabilities []string                     `json:"capabilities"`
	Tools        []architectToolCatalogEntry  `json:"tools"`
}

var compilerServiceAgents = map[agent.WorkerID]struct{}{
	"architect-agent": {}, "coder-agent": {}, "scaffolder-agent": {},
	"writer-agent": {}, "mock_stress_tester": {},
}

// buildArchitectCatalog serializes the registry the runtime actually loaded.
// The architect must not maintain a second, filesystem-derived view of agents
// and skills because that view inevitably drifts from dispatch reality.
func buildArchitectCatalog(reg *agent.Registry) (string, error) {
	catalog := architectCatalog{
		Capabilities: []string{
			"workspace.read", "workspace.write", "network.public", "process.container",
			"process.native", "memory.execution", "graph.delegate", "image.local",
			"rag.local", "cloud.plan", "cloud.apply", "security.active", "gpu.use",
		},
		Tools: []architectToolCatalogEntry{
			{ID: "read_file", Capability: "workspace.read", Description: "Read a workspace text file."},
			{ID: "list_dir", Capability: "workspace.read", Description: "List a workspace directory."},
			{ID: "search_codebase", Capability: "workspace.read", Description: "Search workspace text."},
			{ID: "write_file", Capability: "workspace.write", Description: "Create a workspace text file."},
			{ID: "replace_file_content", Capability: "workspace.write", Description: "Replace previously read file content."},
			{ID: "read_url", Capability: "network.public", Description: "Read public HTTP documentation."},
			{ID: "execute_terminal_command", Capability: "process.container", Description: "Run a bounded command in the workspace.", Constraint: "Uses a container unless both process.native and the user's native-execution setting are enabled."},
			{ID: "remember", Capability: "memory.execution", Description: "Store execution-scoped JSON memory."},
			{ID: "remember_if_version", Capability: "memory.execution", Description: "Compare-and-set execution memory."},
			{ID: "delegate", Capability: "graph.delegate", Description: "Request work from a registered agent."},
			{ID: "generate_local_asset", Capability: "image.local", Description: "Generate an image through local ComfyUI."},
			{ID: "index_directory", Capability: "rag.local", Description: "Index workspace files into local RAG.", Constraint: "Available through the maintained rag-agent worker, not a generic generated coding worker."},
			{ID: "query_knowledge", Capability: "rag.local", Description: "Query the local RAG index.", Constraint: "Available through the maintained rag-agent worker, not a generic generated coding worker."},
			{ID: "remove_path_from_index", Capability: "rag.local", Description: "Remove a path from local RAG.", Constraint: "Available through the maintained rag-agent worker, not a generic generated coding worker."},
			{ID: "mark_task_complete", Description: "Finish only after declared outputs are written and verified."},
		},
	}

	for id, def := range reg.Definitions {
		if _, internal := compilerServiceAgents[id]; internal {
			continue
		}
		catalog.Agents = append(catalog.Agents, architectAgentCatalogEntry{
			ID: string(id), Name: def.Name, Description: def.Description, Runtime: def.Runtime,
			Inputs: def.Inputs, Outputs: def.Outputs, Skills: def.Skills, Capabilities: def.Capabilities,
		})
	}
	for _, def := range reg.Skills {
		catalog.Skills = append(catalog.Skills, architectSkillCatalogEntry{
			ID: def.ID, Name: def.Name, Description: def.Description,
			DependencyPolicy: def.DependencyPolicy, Dependencies: def.Dependencies, EnvVars: def.EnvVars,
		})
	}
	sort.Slice(catalog.Agents, func(i, j int) bool { return catalog.Agents[i].ID < catalog.Agents[j].ID })
	sort.Slice(catalog.Skills, func(i, j int) bool { return catalog.Skills[i].ID < catalog.Skills[j].ID })
	sort.Strings(catalog.Capabilities)
	sort.Slice(catalog.Tools, func(i, j int) bool { return catalog.Tools[i].ID < catalog.Tools[j].ID })
	data, err := json.Marshal(catalog)
	return string(data), err
}
