package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"gopkg.in/yaml.v3"
)

// RuntimeType defines the execution environment required by an agent.
type RuntimeType string

const (
	RuntimePython RuntimeType = "python"
	RuntimeGo     RuntimeType = "go"
	RuntimeBinary RuntimeType = "binary"
)

// AgentDefinition represents the parsed configuration of an agent.
type AgentDefinition struct {
	ID          WorkerID `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`

	Runtime    RuntimeType `yaml:"runtime"`
	Entrypoint string      `yaml:"entrypoint"`

	Inputs         []string     `yaml:"inputs"`  // ArtifactTypes
	Outputs        []string     `yaml:"outputs"` // ArtifactTypes
	Skills         []string     `yaml:"skills"`  // Skill IDs
	Memory         []string     `yaml:"memory"`  // Required shared memory keys
	Capabilities   []Capability `yaml:"capabilities"`
	MCPServers     []string     `yaml:"mcp_servers"`
	BrokerPolicies []string     `yaml:"-"`

	Subscriptions []SubscriptionYAML `yaml:"subscriptions"`
}

// SkillDefinition defines a shared capability, toolset, or dependency group that can be attached to an agent.
type SkillDefinition struct {
	ID               string   `yaml:"id"`
	Name             string   `yaml:"name"`
	Version          string   `yaml:"version"`
	Description      string   `yaml:"description"`
	DependencyPolicy string   `yaml:"dependency_policy"`
	Dependencies     []string `yaml:"dependencies"`
	EnvVars          []string `yaml:"env_vars"`
}

// SubscriptionYAML defines an event subscription for an agent.
type SubscriptionYAML struct {
	ID      string            `yaml:"id"`
	Event   string            `yaml:"event"`
	Filters map[string]string `yaml:"filters"`
}

// Registry manages the collection of all loaded agents, workflows, and skills.
type Registry struct {
	mu          sync.RWMutex
	Definitions map[WorkerID]AgentDefinition
	Workflows   map[string]*WorkflowDefinition
	Skills      map[string]SkillDefinition
}

// CatalogSnapshot returns detached maps for readers that must build an
// authoritative catalogue while plugins may be enabled or disabled.
func (r *Registry) CatalogSnapshot() (map[WorkerID]AgentDefinition, map[string]SkillDefinition) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agents := make(map[WorkerID]AgentDefinition, len(r.Definitions))
	for id, definition := range r.Definitions {
		definition.Inputs = append([]string(nil), definition.Inputs...)
		definition.Outputs = append([]string(nil), definition.Outputs...)
		definition.Skills = append([]string(nil), definition.Skills...)
		definition.Capabilities = append([]Capability(nil), definition.Capabilities...)
		definition.MCPServers = append([]string(nil), definition.MCPServers...)
		agents[id] = definition
	}
	skills := make(map[string]SkillDefinition, len(r.Skills))
	for id, definition := range r.Skills {
		definition.Dependencies = append([]string(nil), definition.Dependencies...)
		definition.EnvVars = append([]string(nil), definition.EnvVars...)
		skills[id] = definition
	}
	return agents, skills
}

var agentIDPattern = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{0,119}$")

// NewRegistry initializes and returns a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		Definitions: make(map[WorkerID]AgentDefinition),
		Workflows:   make(map[string]*WorkflowDefinition),
		Skills:      make(map[string]SkillDefinition),
	}
}

// LoadAgents recursively scans a directory for agent YAML definitions and loads them into the registry.
func (r *Registry) LoadAgents(directory string) error {
	return filepath.WalkDir(directory, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || (filepath.Ext(d.Name()) != ".yaml" && filepath.Ext(d.Name()) != ".yml") {
			return nil
		}

		def, err := ReadAgentDefinition(path)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.Definitions[def.ID] = def
		r.mu.Unlock()
		return nil
	})
}

// LoadSkills scans a directory for skill YAML definitions and loads them into the registry.
func (r *Registry) LoadSkills(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
			continue
		}

		path := filepath.Join(directory, entry.Name())
		def, err := ReadSkillDefinition(path)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.Skills[def.ID] = def
		r.mu.Unlock()
	}

	return nil
}

func ReadAgentDefinition(path string) (AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AgentDefinition{}, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var def AgentDefinition
	if err := decodeDefinition(data, &def); err != nil {
		return AgentDefinition{}, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if !agentIDPattern.MatchString(string(def.ID)) {
		return AgentDefinition{}, fmt.Errorf("agent definition in %s has an invalid ID", path)
	}
	if strings.TrimSpace(def.Name) == "" || strings.TrimSpace(def.Version) == "" {
		return AgentDefinition{}, fmt.Errorf("agent definition in %s requires name and version", path)
	}
	if def.Entrypoint == "" || (def.Runtime != RuntimePython && def.Runtime != RuntimeBinary && def.Runtime != RuntimeGo) {
		return AgentDefinition{}, fmt.Errorf("%s: valid runtime and entrypoint required", path)
	}
	if len(def.Capabilities) == 0 {
		def.Capabilities = defaultWorkerCapabilities()
	}
	if err := validateCapabilities(def.Capabilities); err != nil {
		return AgentDefinition{}, fmt.Errorf("%s: %w", path, err)
	}
	seenMCP := make(map[string]bool, len(def.MCPServers))
	for _, serverID := range def.MCPServers {
		if !agentIDPattern.MatchString(serverID) || seenMCP[serverID] {
			return AgentDefinition{}, fmt.Errorf("%s: invalid or duplicate MCP server %q", path, serverID)
		}
		seenMCP[serverID] = true
	}
	if len(def.MCPServers) > 0 && !hasCapability(def.Capabilities, CapabilityMCPCall) {
		return AgentDefinition{}, fmt.Errorf("%s: mcp_servers requires capability %s", path, CapabilityMCPCall)
	}
	if !filepath.IsAbs(def.Entrypoint) {
		def.Entrypoint = filepath.Join(filepath.Dir(path), def.Entrypoint)
	}
	if info, err := os.Stat(def.Entrypoint); err != nil || info.IsDir() {
		return AgentDefinition{}, fmt.Errorf("%s: entrypoint does not exist", path)
	}
	return def, nil
}

func ReadSkillDefinition(path string) (SkillDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillDefinition{}, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var def SkillDefinition
	if err := decodeDefinition(data, &def); err != nil {
		return SkillDefinition{}, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if !agentIDPattern.MatchString(def.ID) {
		return SkillDefinition{}, fmt.Errorf("skill definition in %s has an invalid ID", path)
	}
	if err := validateSkillDependencies(&def); err != nil {
		return SkillDefinition{}, fmt.Errorf("%s: %w", path, err)
	}
	return def, nil
}

// RegisterBundle publishes a fully validated plugin contribution atomically.
func (r *Registry) RegisterBundle(agents map[WorkerID]AgentDefinition, skills map[string]SkillDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range agents {
		if _, exists := r.Definitions[id]; exists {
			return fmt.Errorf("agent %s is already registered", id)
		}
	}
	for id := range skills {
		if _, exists := r.Skills[id]; exists {
			return fmt.Errorf("skill %s is already registered", id)
		}
	}
	for id, def := range agents {
		for _, skillID := range def.Skills {
			if _, exists := r.Skills[skillID]; !exists {
				if _, bundled := skills[skillID]; !bundled {
					return fmt.Errorf("agent %s references unknown skill %s", id, skillID)
				}
			}
		}
	}
	for id, skill := range skills {
		r.Skills[id] = skill
	}
	for id, def := range agents {
		r.Definitions[id] = def
	}
	return nil
}

func (r *Registry) UnregisterBundle(agentIDs []WorkerID, skillIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range agentIDs {
		delete(r.Definitions, id)
	}
	for _, id := range skillIDs {
		delete(r.Skills, id)
	}
}

var pinnedPythonDependency = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(\[[A-Za-z0-9_,.-]+\])?==[A-Za-z0-9][A-Za-z0-9.!+_-]*$`)
var safePythonDependency = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(\[[A-Za-z0-9_,.-]+\])?(==[A-Za-z0-9][A-Za-z0-9.!+_-]*)?$`)

func isPinnedPythonDependency(dependency string) bool {
	return pinnedPythonDependency.MatchString(strings.TrimSpace(dependency))
}

func validateSkillDependencies(def *SkillDefinition) error {
	policy := strings.ToLower(strings.TrimSpace(def.DependencyPolicy))
	if policy == "" {
		// Existing third-party and project skills predate dependency policies.
		// Keep them loadable while making their non-reproducible behavior explicit
		// to callers that inspect the parsed definition.
		policy = "floating"
		def.DependencyPolicy = policy
	}

	switch policy {
	case "profile":
		if len(def.Dependencies) != 0 {
			return fmt.Errorf("skill %s uses dependency_policy profile and must not declare Python dependencies", def.ID)
		}
		return nil
	case "floating", "locked":
	default:
		return fmt.Errorf("skill %s has unsupported dependency_policy %q", def.ID, def.DependencyPolicy)
	}

	for _, dependency := range def.Dependencies {
		dependency = strings.TrimSpace(dependency)
		if !safePythonDependency.MatchString(dependency) {
			return fmt.Errorf("skill %s dependency %q must be a package name or exact name==version pin", def.ID, dependency)
		}
		if policy == "locked" && !isPinnedPythonDependency(dependency) {
			return fmt.Errorf("skill %s dependency %q must use an exact name==version pin under the locked policy", def.ID, dependency)
		}
	}
	return nil
}

// LoadWorkflows scans a directory for workflow YAML definitions and loads them into the registry.
func (r *Registry) LoadWorkflows(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
			continue
		}

		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		var y WorkflowYAML
		if err := decodeDefinition(data, &y); err != nil {
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

// BuildWorkers instantiates executable Worker instances for all agents currently in the registry.
func (r *Registry) BuildWorkers(l *logger.Logger, b *events.Bus, em *EnvironmentManager) map[WorkerID]*Worker {
	workers := make(map[WorkerID]*Worker)

	for id, def := range r.Definitions {
		var executable string
		var args []string
		var envVars []string
		var prepare func(context.Context, Task) (string, []string, error)

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

			// Resolve dependencies only when this worker is executed. A missing
			// environment fails that task, not startup or unrelated specialists.
			ownID, ownSkills := id, append([]SkillDefinition(nil), activeSkills...)
			prepare = func(ctx context.Context, task Task) (string, []string, error) {
				return em.ProvisionContext(ctx, ownID, ownSkills, task)
			}
			args = []string{def.Entrypoint}

		case RuntimeGo, RuntimeBinary:
			executable = def.Entrypoint
			args = []string{}
		default:
			l.Error("Unsupported runtime type", "worker_id", id, "runtime", def.Runtime)
			continue
		}

		w := NewWorker(id, executable, args, envVars, l, b)
		w.RequiredMemory = def.Memory
		w.Capabilities = append([]Capability(nil), def.Capabilities...)
		w.MCPServers = append([]string(nil), def.MCPServers...)
		w.BrokerPolicies = append([]string(nil), def.BrokerPolicies...)
		w.Prepare = prepare
		workers[id] = w
	}

	return workers
}

// BuildSubscriptions generates a list of Event Bus Subscriptions from all agents in the registry.
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

// LoadAgentsForExecution loads a compiled registry under execution-specific keys. Public agent IDs stay local to the graph.
func (r *Registry) LoadAgentsForExecution(dir, execution string) error {
	// A compiled workflow may reuse only maintained agents. In that case the
	// compiler intentionally emits no execution-local agents directory; that is
	// a valid empty overlay, not a failed compilation.
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	local := NewRegistry()
	if err := local.LoadAgents(dir); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, def := range local.Definitions {
		key := WorkerID(execution + "__" + string(id))
		def.ID = key
		r.Definitions[key] = def
	}
	return nil
}

// LoadWorkflowsForExecution loads workflows and remaps agent references to use execution-scoped keys.
func (r *Registry) LoadWorkflowsForExecution(dir, execution string) error {
	r.mu.Lock()
	local := NewRegistry()
	for id, def := range r.Definitions {
		if strings.HasPrefix(string(id), execution+"__") {
			local.Definitions[WorkerID(strings.TrimPrefix(string(id), execution+"__"))] = def
		} else if !strings.Contains(string(id), "__") {
			local.Definitions[id] = def
		}
	}
	r.mu.Unlock()
	if err := local.LoadWorkflows(dir); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, wf := range local.Workflows {
		r.Workflows[id] = wf
	}
	return nil
}

// BuildWorkersForExecution instantiates workers only for agents specific to a given execution.
func (r *Registry) BuildWorkersForExecution(execution string, l *logger.Logger, b *events.Bus, em *EnvironmentManager) map[WorkerID]*Worker {
	r.mu.Lock()
	local := NewRegistry()
	for id, def := range r.Definitions {
		if strings.HasPrefix(string(id), execution+"__") {
			local.Definitions[id] = def
		}
	}
	for id, skill := range r.Skills {
		local.Skills[id] = skill
	}
	r.mu.Unlock()
	return local.BuildWorkers(l, b, em)
}

func (r *Registry) BuildWorkersByID(ids []WorkerID, l *logger.Logger, b *events.Bus, em *EnvironmentManager) map[WorkerID]*Worker {
	r.mu.Lock()
	local := NewRegistry()
	for _, id := range ids {
		if def, exists := r.Definitions[id]; exists {
			local.Definitions[id] = def
		}
	}
	for id, skill := range r.Skills {
		local.Skills[id] = skill
	}
	r.mu.Unlock()
	return local.BuildWorkers(l, b, em)
}

func decodeDefinition(data []byte, value any) error {
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	return d.Decode(value)
}

// GetWorkflow retrieves a parsed workflow definition by its ID.
func (r *Registry) GetWorkflow(id string) (*WorkflowDefinition, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wf, ok := r.Workflows[id]
	return wf, ok
}
