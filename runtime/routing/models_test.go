package routing

import (
	"testing"
	"time"
)

func TestFinalizeModelRecordsMetadataProvenance(t *testing.T) {
	observed := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	model := finalizeModel(Model{ID: "ollama/fixture", APIKeyEnv: "OLLAMA_HOST", Cost: 0, Enabled: true}, observed)
	if model.Provider != "ollama" || model.EndpointEnv != "OLLAMA_HOST" || model.MetadataSource != "runtime-probe" || model.ToolSupport != "unknown" || !model.ObservedAt.Equal(observed) {
		t.Fatalf("incomplete explicit metadata: %#v", model)
	}
}

func TestPerMillionPreservesUnknownPrice(t *testing.T) {
	if perMillion(-1) != -1 || perMillion(0.000002) != 2 {
		t.Fatal("price conversion did not preserve units or unknown state")
	}
}
