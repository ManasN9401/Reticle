package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBundleBuildsBoundedContributions(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "worker.py", "print('ok')\n")
	writeFixture(t, root, "tool.py", "print('{}')\n")
	writeFixture(t, root, "skill.yaml", "id: fixture-skill\nname: Fixture\nversion: 1.0.0\ndescription: Test skill\ndependency_policy: profile\n")
	writeFixture(t, root, "agent.yaml", "id: fixture-agent\nname: Fixture Agent\ndescription: Test agent\nversion: 1.0.0\nruntime: python\nentrypoint: worker.py\ninputs: []\noutputs: []\nskills: [fixture-skill]\ncapabilities: [workspace.read]\n")
	writeFixture(t, root, ManifestFilename, `{
  "id":"fixture-plugin","version":"1.2.3","description":"Fixture",
  "agents":["agent.yaml"],"skills":["skill.yaml"],
  "tools":[{"id":"inspect","name":"fixture_inspect","description":"Inspect safely","schema":{"type":"object"},"requiredCapability":"workspace.read","command":"python","args":["{pluginRoot}/tool.py"],"effect":"no_effect"}],
  "compatibility":{"reticleVersion":"^0.1.0"}
}`)
	bundle, err := loadBundle(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.agents) != 1 || len(bundle.skills) != 1 || len(bundle.descriptors) != 1 {
		t.Fatalf("missing contributions: %#v", bundle)
	}
	descriptor := bundle.descriptors[0]
	if descriptor.ID != "plugin.fixture-plugin.inspect" || descriptor.RequiredPolicy != "plugin:fixture-plugin" {
		t.Fatalf("unscoped plugin descriptor: %#v", descriptor)
	}
	definition := bundle.agents["fixture-agent"]
	if len(definition.BrokerPolicies) != 1 || definition.BrokerPolicies[0] != "plugin:fixture-plugin" {
		t.Fatalf("plugin agent lacks broker policy: %#v", definition)
	}
}

func TestLoadBundleRejectsEscapeAndIncompatibleVersion(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "plugin")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, parent, "outside.yaml", "id: outside\n")
	writeFixture(t, root, ManifestFilename, `{"id":"bad","version":"1.0.0","skills":["../outside.yaml"],"compatibility":{"reticleVersion":"*"}}`)
	if _, err := loadBundle(root); err == nil {
		t.Fatal("bundle path escape was accepted")
	}
	writeFixture(t, root, ManifestFilename, `{"id":"bad","version":"1.0.0","compatibility":{"reticleVersion":">=9.0.0"}}`)
	if _, err := loadBundle(root); err == nil {
		t.Fatal("incompatible bundle was accepted")
	}
}

func TestVersionRanges(t *testing.T) {
	for _, test := range []struct {
		version, constraint string
		want                bool
	}{
		{"0.1.0", "^0.1.0", true}, {"0.1.9", "^0.1.0", true}, {"0.2.0", "^0.1.0", false},
		{"1.3.0", "~1.2.0", false}, {"1.2.9", ">=1.2.0 <2.0.0", true},
	} {
		if got := satisfies(test.version, test.constraint); got != test.want {
			t.Fatalf("satisfies(%q, %q)=%v want %v", test.version, test.constraint, got, test.want)
		}
	}
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
