package plugin

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/mcp"
	"github.com/reticle/runtime/toolbroker"
)

func TestPluginInstallEnableDisableDeleteLifecycle(t *testing.T) {
	root := t.TempDir()
	source := t.TempDir()
	writeFixture(t, source, ManifestFilename, `{
  "id":"lifecycle-plugin","version":"1.0.0",
  "tools":[{"id":"inspect","name":"lifecycle_inspect","description":"Inspect","schema":{"type":"object"},"requiredCapability":"workspace.read","command":"python","effect":"no_effect"}],
  "compatibility":{"reticleVersion":"*"}
}`)
	bus := events.NewBus("plugin-lifecycle")
	broker, err := toolbroker.New(bus)
	if err != nil {
		t.Fatal(err)
	}
	mcpRegistry, err := mcp.NewRegistry(root, filepath.Join(root, ".reticle", "mcp", "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	mcpManager, err := mcp.NewManager(mcpRegistry, broker, bus, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(root, agent.NewRegistry(), nil, nil, nil, mcpManager, broker, bus, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = broker.Close(ctx)
		bus.Close()
	})
	installed, err := manager.Install(source)
	if err != nil || installed.Enabled {
		t.Fatalf("install: %#v %v", installed, err)
	}
	enabled, err := manager.Enable(context.Background(), installed.ID)
	if err != nil || !enabled.Enabled {
		t.Fatalf("enable: %#v %v", enabled, err)
	}
	descriptors := broker.Descriptors()
	if len(descriptors) != 1 || descriptors[0].RequiredPolicy != "plugin:lifecycle-plugin" {
		t.Fatalf("tool not registered: %#v", descriptors)
	}
	disabled, err := manager.Disable(installed.ID)
	if err != nil || disabled.Enabled || len(broker.Descriptors()) != 0 {
		t.Fatalf("disable: %#v %v", disabled, err)
	}
	if err := manager.Delete(installed.ID); err != nil {
		t.Fatal(err)
	}
	if len(manager.List()) != 0 {
		t.Fatal("deleted plugin remains installed")
	}
}
