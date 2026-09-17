package agent

import "fmt"

// Capability is an enforceable runtime permission, not an instruction to the
// model. Agent definitions declare an upper bound; task admission may remove
// grants but must not add undeclared privileges.
type Capability string

const (
	CapabilityWorkspaceRead    Capability = "workspace.read"
	CapabilityWorkspaceWrite   Capability = "workspace.write"
	CapabilityNetworkPublic    Capability = "network.public"
	CapabilityProcessContainer Capability = "process.container"
	CapabilityProcessNative    Capability = "process.native"
	CapabilityMemoryExecution  Capability = "memory.execution"
	CapabilityGraphDelegate    Capability = "graph.delegate"
	CapabilityImageLocal       Capability = "image.local"
	CapabilityRAGLocal         Capability = "rag.local"
	CapabilityCloudPlan        Capability = "cloud.plan"
	CapabilityCloudApply       Capability = "cloud.apply"
	CapabilitySecurityActive   Capability = "security.active"
	CapabilityGPUUse           Capability = "gpu.use"
)

var knownCapabilities = map[Capability]struct{}{
	CapabilityWorkspaceRead: {}, CapabilityWorkspaceWrite: {}, CapabilityNetworkPublic: {},
	CapabilityProcessContainer: {}, CapabilityProcessNative: {}, CapabilityMemoryExecution: {},
	CapabilityGraphDelegate: {}, CapabilityImageLocal: {}, CapabilityRAGLocal: {},
	CapabilityCloudPlan: {}, CapabilityCloudApply: {}, CapabilitySecurityActive: {}, CapabilityGPUUse: {},
}

func validateCapabilities(capabilities []Capability) error {
	for _, capability := range capabilities {
		if _, ok := knownCapabilities[capability]; !ok {
			return fmt.Errorf("unknown capability %q", capability)
		}
	}
	return nil
}

func defaultWorkerCapabilities() []Capability {
	// Compatibility profile for existing v1 manifests. New manifests should be
	// explicit. Native execution still requires the separate user setting.
	return []Capability{
		CapabilityWorkspaceRead, CapabilityWorkspaceWrite, CapabilityNetworkPublic,
		CapabilityProcessContainer, CapabilityProcessNative, CapabilityMemoryExecution,
		CapabilityGraphDelegate, CapabilityImageLocal, CapabilityRAGLocal,
	}
}

func hasCapability(capabilities []Capability, required Capability) bool {
	for _, capability := range capabilities {
		if capability == required {
			return true
		}
	}
	return false
}
