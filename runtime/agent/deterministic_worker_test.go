package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A registered worker that runs the shared model loop must be routed a model. Listing one as
// deterministic made it start without llm_model and fail every attempt.
func TestWorkersThatRunTheModelLoopAreRouted(t *testing.T) {
	agentsDir := filepath.Join("..", "..", "cmd", "forge", "compiler", "agents")
	definitions, err := filepath.Glob(filepath.Join(agentsDir, "*", "definition.yml"))
	if err != nil || len(definitions) == 0 {
		t.Fatalf("no agent definitions under %s: %v", agentsDir, err)
	}
	idLine := regexp.MustCompile(`(?m)^id:\s*"?([A-Za-z0-9_-]+)"?\s*$`)
	runsModelLoop := regexp.MustCompile(`(?m)^from worker_sdk import .*\brun\b`)
	checked := 0
	for _, definition := range definitions {
		data, err := os.ReadFile(definition)
		if err != nil {
			t.Fatal(err)
		}
		match := idLine.FindSubmatch(data)
		if match == nil {
			t.Fatalf("%s has no id", definition)
		}
		id := WorkerID(match[1])
		scripts, _ := filepath.Glob(filepath.Join(filepath.Dir(definition), "workers", "*.py"))
		for _, script := range scripts {
			source, err := os.ReadFile(script)
			if err != nil {
				t.Fatal(err)
			}
			if runsModelLoop.Match(source) {
				checked++
				if deterministicWorker(id) {
					t.Errorf("%s runs worker_sdk.run in %s but is listed as deterministic, so it is never routed a model", id, filepath.Base(script))
				}
			}
		}
	}
	if checked < 4 {
		t.Fatalf("found only %d model-loop workers; the scan no longer matches the worker layout", checked)
	}
	for _, id := range []WorkerID{"coder-agent", "scaffolder-agent", "hitl-agent"} {
		if !deterministicWorker(id) {
			t.Errorf("%s calls no model and must stay deterministic", id)
		}
	}
}
