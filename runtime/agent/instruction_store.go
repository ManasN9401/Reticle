package agent

import (
	"sync"
)

type InstructionScope string

const (
	ScopeGlobal   InstructionScope = "global"
	ScopeWorkflow InstructionScope = "workflow"
	ScopeAgent    InstructionScope = "agent"
)

type Instruction struct {
	ID      string
	Scope   InstructionScope
	Target  string // workflow_id or agent_id. Empty if global.
	Content string
}

type InstructionStore struct {
	mu           sync.RWMutex
	instructions map[string]Instruction
}

func NewInstructionStore() *InstructionStore {
	return &InstructionStore{
		instructions: make(map[string]Instruction),
	}
}

func (s *InstructionStore) Add(inst Instruction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instructions[inst.ID] = inst
}

func (s *InstructionStore) GetForTask(agentID string, workflowID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var global []string
	var workflow []string
	var agent []string

	for _, inst := range s.instructions {
		if inst.Scope == ScopeGlobal {
			global = append(global, inst.Content)
		} else if inst.Scope == ScopeWorkflow && inst.Target == workflowID {
			workflow = append(workflow, inst.Content)
		} else if inst.Scope == ScopeAgent && inst.Target == agentID {
			agent = append(agent, inst.Content)
		}
	}

	// Order: Global -> Workflow -> Agent
	var result []string
	result = append(result, global...)
	result = append(result, workflow...)
	result = append(result, agent...)

	return result
}
