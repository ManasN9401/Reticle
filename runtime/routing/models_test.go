package routing

import (
	"testing"
	"time"
)

func TestFinalizeModelRecordsMetadataProvenance(t *testing.T) {
	observed := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	model := finalizeModel(Model{ID: "ollama/fixture", Cost: 0, Enabled: true}, observed)
	if model.Provider != "ollama" || model.EndpointEnv != "OLLAMA_HOST" || model.APIKeyEnv != "" || model.MetadataSource != "runtime-probe" || model.ToolSupport != "unknown" || !model.ObservedAt.Equal(observed) {
		t.Fatalf("incomplete explicit metadata: %#v", model)
	}
}

func TestPerMillionPreservesUnknownPrice(t *testing.T) {
	if perMillion(-1) != -1 || perMillion(0.000002) != 2 {
		t.Fatal("price conversion did not preserve units or unknown state")
	}
}

func TestOpenRouterFreeTierIsUsableUntilItsLimit(t *testing.T) {
	limit := 100.0
	freeOnly, unavailable := openRouterKeyAccess(true, &limit, 25)
	if !freeOnly || unavailable {
		t.Fatalf("free-tier key was incorrectly marked unavailable: freeOnly=%v unavailable=%v", freeOnly, unavailable)
	}
	freeOnly, unavailable = openRouterKeyAccess(true, &limit, 100)
	if !freeOnly || !unavailable {
		t.Fatalf("exhausted free-tier key remained available: freeOnly=%v unavailable=%v", freeOnly, unavailable)
	}
}
