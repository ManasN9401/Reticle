package routing

import (
	"testing"
	"time"

	"github.com/reticle/runtime/logger"
)

// The error body OpenRouter returned when a free-tier account ran out of daily requests.
const openRouterDailyQuota = `{"error":{"message":"Rate limit exceeded: free-models-per-day. Add 10 credits to unlock 1000 free model requests per day","code":429,"metadata":{"headers":{"X-RateLimit-Limit":"50","X-RateLimit-Remaining":"0","X-RateLimit-Reset":"1791676800000"},"limit_source":"openrouter_free_tier_daily"}}}`

func TestQuotaResetAtReadsTheProvidersOwnResetTime(t *testing.T) {
	now := time.UnixMilli(1791676800000).Add(-13 * time.Hour)
	want := time.UnixMilli(1791676800000)
	if got := QuotaResetAt(openRouterDailyQuota, now); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := QuotaResetAt(`X-RateLimit-Reset: 1791676800`, now); !got.Equal(want) {
		t.Fatalf("seconds form: got %v", got)
	}
}

func TestQuotaResetAtRejectsUnusableValues(t *testing.T) {
	now := time.UnixMilli(1791676800000).Add(-13 * time.Hour)
	for name, text := range map[string]string{
		"absent":         "RateLimitError: Request too large",
		"already passed": `"X-RateLimit-Reset":"1700000000000"`,
		"too far ahead":  `"X-RateLimit-Reset":"1893456000000"`,
		"not a number":   `"X-RateLimit-Reset":"soon"`,
		"too few digits": `"X-RateLimit-Reset":"1234"`,
	} {
		if got := QuotaResetAt(text, now); !got.IsZero() {
			t.Errorf("%s: accepted %v", name, got)
		}
	}
}

func TestAnExhaustedQuotaStaysBlockedUntilTheReportedReset(t *testing.T) {
	ModelsMutex.Lock()
	old, health := AvailableModels, KeyHealthStates
	AvailableModels = []Model{{ID: "openrouter/free-model:free", Provider: "openrouter", APIKeyEnv: "OPENROUTER_KEY", Enabled: true, Modality: "text"}}
	KeyHealthStates = nil
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels, KeyHealthStates = old, health; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}

	// Without a reported reset the key is probed again after 15 minutes.
	r.PenalizeProviderFor("agent", "OPENROUTER_KEY", "provider_account_quota", "quota")
	if retry := KeyHealthStates[0].RetryAt; retry.After(time.Now().Add(16*time.Minute)) || retry.Before(time.Now().Add(14*time.Minute)) {
		t.Fatalf("default probe time changed: %v", retry)
	}

	reset := time.Now().Add(13 * time.Hour)
	r.PenalizeProviderForUntil("agent", "OPENROUTER_KEY", "provider_account_quota", "quota", reset)
	if got := KeyHealthStates[0].RetryAt; !got.Equal(reset) {
		t.Fatalf("key retries at %v, want the reported reset %v", got, reset)
	}
	r.PenalizeProviderFamilyUntil("openrouter", reset)
	if got := KeyHealthStates[0].RetryAt; !got.Equal(reset) {
		t.Fatalf("family penalty retries at %v, want %v", got, reset)
	}
	// A reset sooner than the default never shortens the wait.
	r.PenalizeProviderForUntil("agent", "OPENROUTER_KEY", "provider_account_quota", "quota", time.Now().Add(time.Minute))
	if KeyHealthStates[0].RetryAt.Before(time.Now().Add(14 * time.Minute)) {
		t.Fatal("a near reset shortened the default 15 minute block")
	}
	if got := r.SelectModel("task", "agent", 0, .9, "text"); got != nil {
		t.Fatalf("an exhausted free route was selected: %#v", got)
	}
}
