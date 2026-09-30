package routing

import (
	"errors"
	"net/http"
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

func TestDiscoveryHealthDoesNotCallTransportFailureRateLimited(t *testing.T) {
	observed := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	transport := keyHealthForDiscovery("KEY", 0, errors.New("socket unavailable"), observed)
	if transport.Status != KeyDiscoveryUnreachable || healthLocksKey(transport.Status) {
		t.Fatalf("transport failure misclassified: %#v", transport)
	}
	auth := keyHealthForDiscovery("KEY", http.StatusUnauthorized, nil, observed)
	if auth.Status != KeyAuthenticationFailed || !healthLocksKey(auth.Status) {
		t.Fatalf("authentication failure misclassified: %#v", auth)
	}
	rateLimit := keyHealthForDiscovery("KEY", http.StatusTooManyRequests, nil, observed)
	if rateLimit.Status != KeyRateLimited || healthLocksKey(rateLimit.Status) || rateLimit.RetryAt.IsZero() {
		t.Fatalf("temporary discovery limit misclassified: %#v", rateLimit)
	}
}

func TestProviderSlotSeparatesLocalRuntimes(t *testing.T) {
	ollama := finalizeModel(Model{ID: "ollama/qwen", Enabled: true}, time.Now())
	comfy := finalizeModel(Model{ID: "comfyui/image", Enabled: true}, time.Now())
	if ProviderSlot(ollama) == ProviderSlot(comfy) {
		t.Fatalf("local providers share congestion slot %q", ProviderSlot(ollama))
	}
}

func TestQuotaHealthAllowsAProbeAfterRetryWindow(t *testing.T) {
	now := time.Now()
	if !healthCurrentlyLocksKey(KeyHealth{Status: KeyQuotaExhausted, RetryAt: now.Add(time.Minute)}, now) {
		t.Fatal("active quota cooldown was not enforced")
	}
	if healthCurrentlyLocksKey(KeyHealth{Status: KeyQuotaExhausted, RetryAt: now.Add(-time.Second)}, now) {
		t.Fatal("expired quota cooldown prevented a recovery probe")
	}
	if !healthCurrentlyLocksKey(KeyHealth{Status: KeyAuthenticationFailed}, now) {
		t.Fatal("authentication failure unexpectedly expired")
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
