package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
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
}

type Registry struct {
	Definitions map[WorkerID]AgentDefinition
	Workflows   map[string]*WorkflowDefinition
}

func NewRegistry() *Registry {
	return &Registry{
		Definitions: make(map[WorkerID]AgentDefinition),
		Workflows:   make(map[string]*WorkflowDefinition),
	}
}

func (r *Registry) LoadAgents(directory string) error {
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

		var def AgentDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		if def.ID == "" {
			return fmt.Errorf("agent definition in %s is missing ID", path)
		}

		r.Definitions[def.ID] = def
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
				ID:       nodeYAML.ID,
				WorkerID: nodeYAML.Agent,
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
		for nodeID := range def.Nodes {
			if len(def.Parents[nodeID]) == 0 {
				def.Roots = append(def.Roots, nodeID)
			}
		}

		if len(def.Roots) == 0 {
			return fmt.Errorf("workflow %s has no roots (cycle detected or empty graph)", y.ID)
		}

		r.Workflows[y.ID] = def
	}

	return nil
}

func (r *Registry) BuildWorkers(l *logger.Logger, b *events.Bus) map[WorkerID]*Worker {
	workers := make(map[WorkerID]*Worker)

	for id, def := range r.Definitions {
		var executable string
		var args []string

		switch def.Runtime {
		case RuntimePython:
			executable = "python"
			args = []string{def.Entrypoint}
		case RuntimeBinary:
			executable = def.Entrypoint
			args = []string{}
		default:
			l.Error("Unsupported runtime type", "worker_id", id, "runtime", def.Runtime)
			continue
		}

		workers[id] = NewWorker(id, executable, args, l, b)
	}

	return workers
}
