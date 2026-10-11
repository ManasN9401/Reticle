package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reticle/runtime/agent"
)

// Skill instruction files that are deliberately not assignable by the architect. Each is either
// a reference for another host (it targets Claude Code sessions, not a Reticle worker) or far
// larger than a worker's prompt can carry. Adding a SKILL.md without a manifest, or listing one
// here without a reason, fails TestEverySkillIsAssignableOrExcluded.
var unassignableSkills = map[string]string{
	"continuous-learning":        "reference hook for a compatible host; not a worker instruction",
	"eval-harness":               "describes Claude Code session evals, not a Reticle worker's tools",
	"graphify":                   "39 KB, larger than a worker's prompt can carry; used through the graphify tooling",
	"project-guidelines-example": "a template for writing project skills, not guidance for any task",
	"strategic-compact":          "targets a host's manual /compact, which Reticle workers do not have",
}

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

func loadedSkillRegistry(t *testing.T) *agent.Registry {
	t.Helper()
	registry := agent.NewRegistry()
	for _, directory := range []string{"../../skills", "compiler/skills"} {
		if err := registry.LoadSkills(directory); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.LoadAgents("compiler/agents"); err != nil {
		t.Fatal(err)
	}
	return registry
}

// The architect can only choose well among skills it can see and tell apart. Observed: every
// generated agent in a three.js gallery received the same two React-oriented skills.
func TestSkillsTheArchitectSeesSayWhenToUseThem(t *testing.T) {
	raw, err := buildArchitectCatalog(loadedSkillRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	var catalog architectCatalog
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		t.Fatal(err)
	}
	byID := map[string]architectSkillCatalogEntry{}
	for _, skill := range catalog.Skills {
		byID[skill.ID] = skill
		if skill.UseWhen == "" {
			t.Errorf("skill %s has no use_when, so the architect cannot tell when it applies", skill.ID)
		}
	}
	// The cost of a skill is visible, and the instruction-less bundle reports zero.
	if byID["frontend-uiux"].InstructionsBytes == 0 || byID["llm-worker"].InstructionsBytes != 0 {
		t.Errorf("instruction sizes are wrong: uiux=%d llm-worker=%d", byID["frontend-uiux"].InstructionsBytes, byID["llm-worker"].InstructionsBytes)
	}
	// The two skills a visual node must be able to tell apart.
	if !strings.Contains(byID["frontend-uiux"].UseWhen, "three.js") || !strings.Contains(byID["frontend-patterns"].UseWhen, "Not for vanilla JavaScript") {
		t.Error("frontend skills do not say which kind of page they are for")
	}
	if _, ok := byID["verification-loop"]; !ok {
		t.Error("verification-loop is not assignable")
	}
}

func TestEverySkillIsAssignableOrExcluded(t *testing.T) {
	registry := loadedSkillRegistry(t)
	_, skills := registry.CatalogSnapshot()
	entries, err := os.ReadDir("../../skills")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if _, err := os.Stat(filepath.Join("../../skills", id, "SKILL.md")); err != nil {
			continue
		}
		_, assignable := skills[id]
		reason, excluded := unassignableSkills[id]
		switch {
		case assignable && excluded:
			t.Errorf("skill %s has a manifest but is also listed as unassignable (%s)", id, reason)
		case !assignable && !excluded:
			t.Errorf("skills/%s/SKILL.md has no manifest, so no agent can be given it; add skills/%s.yaml or list it as unassignable with a reason", id, id)
		}
	}
	// A manifest that adds no dependencies must carry instructions, or assigning it does nothing.
	for id, skill := range skills {
		if len(skill.Dependencies) == 0 && skill.InstructionsBytes == 0 {
			t.Errorf("skill %s declares no dependencies and has no SKILL.md; assigning it would change nothing", id)
		}
	}
}

func TestMaintainedAgentsOnlyListSkillsThatExist(t *testing.T) {
	registry := loadedSkillRegistry(t)
	agents, skills := registry.CatalogSnapshot()
	for id, definition := range agents {
		for _, skill := range definition.Skills {
			if _, ok := skills[skill]; !ok {
				t.Errorf("agent %s lists skill %s, which has no manifest", id, skill)
			}
		}
	}
}
