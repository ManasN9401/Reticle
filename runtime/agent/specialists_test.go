package agent

import (
	"path/filepath"
	"testing"
)

// The four specialist agents once shipped as shells that ran the generic code generator while their
// descriptions promised Playwright, yfinance, Google search and a REPL. The definitions are what the
// registry loads, so the contract is checked on the real files.
func TestSpecialistAgentsDeclareTheirRealToolsAndLeastPrivilege(t *testing.T) {
	registry := NewRegistry()
	if err := registry.LoadAgents(filepath.Join("..", "..", "cmd", "forge", "compiler", "agents")); err != nil {
		t.Fatalf("agent definitions do not load: %v", err)
	}
	if err := registry.LoadSkills(filepath.Join("..", "..", "skills")); err != nil {
		t.Fatalf("skills do not load: %v", err)
	}
	cases := map[string]struct {
		skill string
		has   []Capability
		not   []Capability
	}{
		"browser-agent":      {"browser-automation", []Capability{CapabilityWorkspaceRead, CapabilityWorkspaceWrite, CapabilityNetworkPublic, CapabilityBrowserRun}, []Capability{CapabilityProcessContainer, CapabilityProcessNative}},
		"osint-agent":        {"osint-investigation", []Capability{CapabilityWorkspaceRead, CapabilityWorkspaceWrite, CapabilityNetworkPublic}, []Capability{CapabilityProcessContainer, CapabilityProcessNative, CapabilityBrowserRun}},
		"quant-agent":        {"quant-finance", []Capability{CapabilityWorkspaceWrite, CapabilityNetworkPublic, CapabilityProcessContainer}, []Capability{CapabilityProcessNative}},
		"hermes-coder-agent": {"hermes-repl", []Capability{CapabilityWorkspaceWrite, CapabilityProcessContainer}, []Capability{CapabilityBrowserRun}},
	}
	for id, want := range cases {
		definition, ok := registry.Definitions[WorkerID(id)]
		if !ok {
			t.Fatalf("%s is not defined", id)
		}
		if len(definition.Skills) != 1 || definition.Skills[0] != want.skill {
			t.Errorf("%s skills = %v, want [%s]", id, definition.Skills, want.skill)
		}
		if _, ok := registry.Skills[want.skill]; !ok {
			t.Errorf("%s names skill %s, which does not exist", id, want.skill)
		}
		have := map[Capability]bool{}
		for _, capability := range definition.Capabilities {
			have[capability] = true
		}
		for _, capability := range want.has {
			if !have[capability] {
				t.Errorf("%s lacks %s", id, capability)
			}
		}
		for _, capability := range want.not {
			if have[capability] {
				t.Errorf("%s was granted %s, which it does not need", id, capability)
			}
		}
	}
	quant := registry.Skills["quant-finance"]
	if quant.DependencyPolicy != "locked" || len(quant.Dependencies) != 3 {
		t.Errorf("quant-finance must pin its market-data packages: %+v", quant)
	}
}
