package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"gopkg.in/yaml.v3"
)

type RuntimeType string

const (
	RuntimePython RuntimeType = "python"
	RuntimeGo     RuntimeType = "go"
	RuntimeBinary RuntimeType = "binary"
)

type AgentDefinition struct {
	ID          WorkerID    `yaml:"id"`
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Version     string      `yaml:"version"`

	Runtime     RuntimeType `yaml:"runtime"`
	Entrypoint  string      `yaml:"entrypoint"`

	Inputs      []string    `yaml:"inputs"`  // ArtifactTypes
	Outputs     []string    `yaml:"outputs"` // ArtifactTypes
	Skills      []string    `yaml:"skills"`  // Skill IDs
	Memory      []string    `yaml:"memory"`  // Required shared memory keys

	Subscriptions []SubscriptionYAML `yaml:"subscriptions"`
}

type SkillDefinition struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Dependencies []string `yaml:"dependencies"`
	EnvVars      []string `yaml:"env_vars"`
}

type SubscriptionYAML struct {
	ID      string            `yaml:"id"`
	Event   string            `yaml:"event"`
	Filters map[string]string `yaml:"filters"`
}

type Registry struct {
	Definitions map[WorkerID]AgentDefinition
	Workflows   map[string]*WorkflowDefinition
	Skills      map[string]SkillDefinition
}

func NewRegistry() *Registry {
	return &Registry{
		Definitions: make(map[WorkerID]AgentDefinition),
		Workflows:   make(map[string]*WorkflowDefinition),
		Skills:      make(map[string]SkillDefinition),
	}
}

func (r *Registry) LoadAgents(directory string) error {
	return filepath.WalkDir(directory, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		if d.IsDir() || filepath.Ext(d.Name()) != ".yaml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		var def AgentDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		if def.ID == "" {
			return fmt.Errorf("agent definition in %s is missing ID", path)
		}

		if def.Entrypoint != "" && !filepath.IsAbs(def.Entrypoint) {
			// Resolve relative to the directory containing the YAML file
			yamlDir := filepath.Dir(path)
			def.Entrypoint = filepath.Join(yamlDir, def.Entrypoint)
		}

		r.Definitions[def.ID] = def
		return nil
	})
}

func (r *Registry) LoadSkills(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		var def SkillDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		if def.ID == "" {
			return fmt.Errorf("skill definition in %s is missing ID", path)
		}

		r.Skills[def.ID] = def
	}

	return nil
}

func (r *Registry) LoadWorkflows(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		var y WorkflowYAML
		if err := yaml.Unmarshal(data, &y); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		if y.ID == "" {
			return fmt.Errorf("workflow definition in %s is missing ID", path)
		}
		if _, exists := r.Workflows[y.ID]; exists {
			return fmt.Errorf("duplicate workflow ID %s in %s", y.ID, path)
		}

		def := &WorkflowDefinition{
			ID:       y.ID,
			Name:     y.Name,
			Version:  y.Version,
			Nodes:    make(map[string]WorkflowNode),
			Edges:    make([]WorkflowEdge, 0),
			Parents:  make(map[string][]string),
			Children: make(map[string][]string),
			Roots:    make([]string, 0),
		}

		// Process Nodes
		for _, nodeYAML := range y.Nodes {
			if nodeYAML.ID == "" {
				return fmt.Errorf("workflow %s has a node with no ID", y.ID)
			}
			if _, exists := def.Nodes[nodeYAML.ID]; exists {
				return fmt.Errorf("workflow %s has duplicate node ID %s", y.ID, nodeYAML.ID)
			}
			if _, exists := r.Definitions[WorkerID(nodeYAML.Agent)]; !exists {
				return fmt.Errorf("workflow %s references unknown agent %s", y.ID, nodeYAML.Agent)
			}
			
			def.Nodes[nodeYAML.ID] = WorkflowNode{
				ID:         nodeYAML.ID,
				WorkerID:   nodeYAML.Agent,
				Modality:   nodeYAML.Modality,
				Parameters: nodeYAML.Parameters,
			}
		}

		// Process Edges
		for _, edgeYAML := range y.Edges {
			if _, exists := def.Nodes[edgeYAML.From]; !exists {
				return fmt.Errorf("workflow %s edge references unknown From node %s", y.ID, edgeYAML.From)
			}
			if _, exists := def.Nodes[edgeYAML.To]; !exists {
				return fmt.Errorf("workflow %s edge references unknown To node %s", y.ID, edgeYAML.To)
			}

			def.Edges = append(def.Edges, WorkflowEdge{
				From: edgeYAML.From,
				To:   edgeYAML.To,
			})

			def.Parents[edgeYAML.To] = append(def.Parents[edgeYAML.To], edgeYAML.From)
			def.Children[edgeYAML.From] = append(def.Children[edgeYAML.From], edgeYAML.To)
		}

		// Find Roots
		inDegree := make(map[string]int)
		for nodeID := range def.Nodes {
			inDegree[nodeID] = len(def.Parents[nodeID])
			if inDegree[nodeID] == 0 {
				def.Roots = append(def.Roots, nodeID)
			}
		}

		if len(def.Roots) == 0 {
			return fmt.Errorf("workflow %s has no roots (cycle detected or empty graph)", y.ID)
		}

		// Check for cycles using Kahn's algorithm
		queue := make([]string, len(def.Roots))
		copy(queue, def.Roots)
		visitedCount := 0

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			visitedCount++

			for _, child := range def.Children[curr] {
				inDegree[child]--
				if inDegree[child] == 0 {
					queue = append(queue, child)
				}
			}
		}

		if visitedCount != len(def.Nodes) {
			return fmt.Errorf("workflow %s contains a cycle (Directed Acyclic Graph requirement violated)", y.ID)
		}

		r.Workflows[y.ID] = def
	}

	return nil
}

func (r *Registry) BuildWorkers(l *logger.Logger, b *events.Bus, em *EnvironmentManager) map[WorkerID]*Worker {
	workers := make(map[WorkerID]*Worker)

	for id, def := range r.Definitions {
		var executable string
		var args []string
		var envVars []string

		switch def.Runtime {
		case RuntimePython:
			// Fetch all inherited skills
			var activeSkills []SkillDefinition
			for _, skillID := range def.Skills {
				if skill, exists := r.Skills[skillID]; exists {
					activeSkills = append(activeSkills, skill)
				} else {
					l.Error("Agent requested unknown skill", "agent_id", id, "skill_id", skillID)
				}
			}

			// Provision Virtual Environment
			pythonExe, injectedEnvVars, err := em.Provision(id, activeSkills)
			if err != nil {
				l.Error("Failed to provision environment", "agent_id", id, "error", err)
				continue
			}

			executable = pythonExe
			args = []string{def.Entrypoint}
			envVars = injectedEnvVars

		case RuntimeBinary:
			executable = def.Entrypoint
			args = []string{}
		default:
			l.Error("Unsupported runtime type", "worker_id", id, "runtime", def.Runtime)
			continue
		}

		w := NewWorker(id, executable, args, envVars, l, b)
		w.RequiredMemory = def.Memory
		workers[id] = w
	}

	return workers
}

func (r *Registry) BuildSubscriptions() []*Subscription {
	var subs []*Subscription

	for id, def := range r.Definitions {
		for i, subYAML := range def.Subscriptions {
			subID := subYAML.ID
			if subID == "" {
				subID = fmt.Sprintf("sub-%s-%d", id, i)
			}
			sub := &Subscription{
				ID:        subID,
				WorkerID:  id,
				EventType: subYAML.Event,
				Filters:   subYAML.Filters,
			}
			subs = append(subs, sub)
		}
	}

	return subs
}
