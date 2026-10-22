package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
)

func TestExplicitEmptyCapabilitiesRemainEmpty(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "worker.py"), []byte("pass"), 0600)
	path := filepath.Join(dir, "definition.yml")
	base := "id: fixture\nname: Fixture\nversion: 1\nruntime: python\nentrypoint: worker.py\n"
	for _, explicit := range []bool{false, true} {
		content := base
		if explicit {
			content += "capabilities: []\n"
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		def, err := ReadAgentDefinition(path)
		if err != nil {
			t.Fatal(err)
		}
		if (len(def.Capabilities) == 0) != explicit {
			t.Fatalf("explicit=%v grants=%v", explicit, def.Capabilities)
		}
	}
	for _, caps := range [][]Capability{nil, {}} {
		data, err := json.Marshal(Task{ID: "fixture", Capabilities: caps})
		if err != nil || !strings.Contains(string(data), `"capabilities":[]`) {
			t.Fatalf("empty grants lost: %s %v", data, err)
		}
	}
	if err := os.WriteFile(path, []byte(base+"capabilities: null\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAgentDefinition(path); err == nil {
		t.Fatal("explicit null restored compatibility grants")
	}
}

func TestDelegationCannotIncreaseCapabilitiesOrBrokerGrants(t *testing.T) {
	d := &Dispatcher{Workers: map[WorkerID]*Worker{
		"source": {Capabilities: []Capability{CapabilityGraphDelegate, CapabilityWorkspaceRead, CapabilityMCPCall}, MCPServers: []string{"allowed"}},
		"reader": {Capabilities: []Capability{CapabilityWorkspaceRead}},
		"writer": {Capabilities: []Capability{CapabilityWorkspaceWrite}},
		"mcp":    {Capabilities: []Capability{CapabilityMCPCall}, MCPServers: []string{"secret"}},
		"plugin": {Capabilities: []Capability{CapabilityWorkspaceRead}, BrokerPolicies: []string{"plugin:secret"}},
	}}
	if !d.CanDelegate("run", "source", "reader") {
		t.Fatal("subset delegation rejected")
	}
	for _, target := range []string{"writer", "mcp", "plugin", "missing"} {
		if d.CanDelegate("run", "source", target) {
			t.Errorf("privilege increase allowed: %s", target)
		}
	}
	d.Workers["run__reader"] = &Worker{Capabilities: []Capability{CapabilityCloudApply}}
	if d.CanDelegate("run", "source", "reader") {
		t.Fatal("execution overlay bypassed admission")
	}
}

func TestFinalResponseMutationsRequireCapabilities(t *testing.T) {
	for _, mode := range []string{"produce", "delegate", "delegate-with-memory"} {
		t.Run(mode, func(t *testing.T) {
			bus := events.NewBus("authorization")
			defer bus.Close()
			mutations := make(chan bool, 2)
			for _, name := range []events.EventType{"TaskResultCommitRequested", "GraphMutationRequested"} {
				bus.Subscribe(name, func(events.RuntimeEvent) { mutations <- true })
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			worker := NewWorker("fixture", exe, []string{"-test.run=^TestWorkerChild$"}, []string{"RETICLE_TEST_CHILD=" + mode}, &logger.Logger{}, bus)
			grants := []Capability{CapabilityWorkspaceRead}
			if mode == "delegate-with-memory" {
				grants = append(grants, CapabilityMemoryExecution, CapabilityGraphDelegate)
				worker.AuthorizeDelegation = func(_, _, _ string) bool { return false }
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, failure := worker.Execute(ctx, Task{ID: "fixture", Capabilities: grants})
			if failure == nil || failure.Reason != WorkerProtocolError {
				t.Fatalf("unauthorized response accepted: %#v", failure)
			}
			select {
			case <-mutations:
				t.Fatal("unauthorized response emitted a mutation")
			default:
			}
		})
	}
}
