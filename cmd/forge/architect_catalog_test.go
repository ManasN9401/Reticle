package main

import (
	"encoding/json"
	"testing"

	"github.com/reticle/runtime/agent"
)

func TestArchitectCatalogReflectsLoadedRegistry(t *testing.T) {
	registry := agent.NewRegistry()
	if err := registry.LoadSkills("../../skills"); err != nil {
		t.Fatal(err)
	}
	if err := registry.LoadSkills("compiler/skills"); err != nil {
		t.Fatal(err)
	}
	if err := registry.LoadAgents("compiler/agents"); err != nil {
		t.Fatal(err)
	}

	raw, err := buildArchitectCatalog(registry)
	if err != nil {
		t.Fatal(err)
	}
	var catalog architectCatalog
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		t.Fatal(err)
	}

	agents := map[string]bool{}
	for _, entry := range catalog.Agents {
		agents[entry.ID] = true
	}
	for _, id := range []string{"browser-agent", "hermes-coder-agent", "osint-agent", "quant-agent", "rag-agent"} {
		if !agents[id] {
			t.Errorf("dispatchable specialist %s missing from architect catalog", id)
		}
	}
	for _, id := range []string{"architect-agent", "coder-agent", "scaffolder-agent", "writer-agent", "mock_stress_tester"} {
		if agents[id] {
			t.Errorf("compiler service %s leaked into architect catalog", id)
		}
	}

	skills := map[string]bool{}
	for _, entry := range catalog.Skills {
		skills[entry.ID] = true
	}
	if !skills["llm-worker"] {
		t.Error("compiler skill manifest ID missing from architect catalog")
	}
	if !skills["coding-standards"] {
		t.Error("project skill missing from architect catalog")
	}
	if len(catalog.Tools) == 0 || len(catalog.Capabilities) == 0 {
		t.Error("tool/capability contract missing from architect catalog")
	}
}
