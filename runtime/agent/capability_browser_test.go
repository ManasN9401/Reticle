package agent

import "testing"

// browser.run lets generated code execute in a browser, so it is explicit: known to the runtime,
// validated like every other capability, and never part of the compatibility default profile.
func TestBrowserRunCapabilityIsExplicit(t *testing.T) {
	if !IsKnownCapability(CapabilityBrowserRun) {
		t.Fatal("browser.run is not a known capability")
	}
	if err := validateCapabilities([]Capability{CapabilityWorkspaceRead, CapabilityBrowserRun}); err != nil {
		t.Fatalf("a declared browser.run was rejected: %v", err)
	}
	if hasCapability(defaultWorkerCapabilities(), CapabilityBrowserRun) {
		t.Fatal("legacy manifests without capabilities must not gain a browser")
	}
	if string(CapabilityBrowserRun) != "browser.run" {
		t.Fatalf("capability name changed: %q", CapabilityBrowserRun)
	}
}
