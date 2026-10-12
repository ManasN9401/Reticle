package routing

import (
	"testing"
	"time"

	"github.com/reticle/runtime/logger"
)

func learningRouter(t *testing.T) *ModelRouter {
	t.Helper()
	ModelsMutex.Lock()
	previous, health, locked := AvailableModels, KeyHealthStates, LockedKeys
	KeyHealthStates, LockedKeys = nil, nil
	AvailableModels = []Model{
		{ID: "small/a", Provider: "small", APIKeyEnv: "LEARN_KEY_A", Enabled: true, Modality: "text", Capability: 40},
		{ID: "small/a", Provider: "small", APIKeyEnv: "LEARN_KEY_B", Enabled: true, Modality: "text", Capability: 40},
		{ID: "big/a", Provider: "big", APIKeyEnv: "LEARN_KEY_C", Enabled: true, Modality: "text", Capability: 40},
	}
	ModelsMutex.Unlock()
	t.Cleanup(func() {
		ModelsMutex.Lock()
		AvailableModels, KeyHealthStates, LockedKeys = previous, health, locked
		ModelsMutex.Unlock()
	})
	return &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}
}

func selectedKeys(r *ModelRouter, policy RoutePolicy) map[string]bool {
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		if m := r.SelectModelWithPolicy("t", "agent", 2, 0, "text", policy); m != nil {
			seen[m.Key()] = true
		}
	}
	return seen
}

func TestRequestLimitOnlyKeepsOutTasksThatCannotFit(t *testing.T) {
	r := learningRouter(t)
	r.LearnRequestLimit("small/a|LEARN_KEY_A", 8000)
	r.LearnRequestLimit("small/a|LEARN_KEY_A", 12000) // a larger report never raises a learned cap
	if got := selectedKeys(r, RoutePolicy{MinRequestTokens: 3000}); !got["small/a|LEARN_KEY_A"] {
		t.Fatalf("a task that fits was kept off the route: %v", got)
	}
	if got := selectedKeys(r, RoutePolicy{MinRequestTokens: 11000}); got["small/a|LEARN_KEY_A"] {
		t.Fatalf("a task that cannot fit used the route: %v", got)
	}
	// The cap belongs to one credential; an unreported sibling is still tried (and learns on failure).
	if got := selectedKeys(r, RoutePolicy{MinRequestTokens: 11000}); !got["small/a|LEARN_KEY_B"] {
		t.Fatalf("a credential that never reported a limit was excluded: %v", got)
	}
	// An unknown size never excludes anything.
	if got := selectedKeys(r, RoutePolicy{}); !got["small/a|LEARN_KEY_A"] {
		t.Fatalf("a task of unknown size was excluded: %v", got)
	}
}

func TestUnavailableRouteReturnsWhenItsTimeIsUp(t *testing.T) {
	r := learningRouter(t)
	r.MarkRouteUnavailable("big/a|LEARN_KEY_C", time.Now().Add(time.Hour), "not found for account")
	if got := selectedKeys(r, RoutePolicy{}); got["big/a|LEARN_KEY_C"] {
		t.Fatalf("a route marked unavailable was selected: %v", got)
	}
	if availability := r.RouteAvailability("text", RoutePolicy{}); availability.Potential != 2 {
		t.Fatalf("diagnostics still count the dead route: %+v", availability)
	}
	r.MarkRouteUnavailable("big/a|LEARN_KEY_C", time.Now().Add(-time.Second), "ignored: not later than the current mark")
	r.mu.Lock()
	r.unavailable["big/a|LEARN_KEY_C"] = time.Now().Add(-time.Second)
	r.mu.Unlock()
	if got := selectedKeys(r, RoutePolicy{}); !got["big/a|LEARN_KEY_C"] {
		t.Fatalf("a route stayed unavailable after its time: %v", got)
	}
}

func TestParseRequestLimit(t *testing.T) {
	for _, test := range []struct {
		name, text string
		want       int
	}{
		{"groq per minute", "Request too large for model `openai/gpt-oss-120b` on tokens per minute (TPM): Limit 8000, Requested 11399, please reduce", 8000},
		{"already partly used is not a size cap", "Rate limit reached on tokens per day (TPD): Limit 100000, Used 99000, Requested 5000", 0},
		{"within the limit", "Limit 8000, Requested 500", 0},
		{"context window", "This model's maximum context length is 8192 tokens. However, your messages resulted in 9100 tokens", 8192},
		{"unrelated", "connection reset by peer", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ParseRequestLimit(test.text); got != test.want {
				t.Fatalf("got %d, want %d", got, test.want)
			}
		})
	}
}

func TestParseRateWindow(t *testing.T) {
	for _, test := range []struct {
		name, text string
		ok         bool
		limit      int
		used       int
		requested  int
		wait       time.Duration
	}{
		{"seconds", "on tokens per minute (TPM): Limit 8000, Used 6424, Requested 6635. Please try again in 4.5s.", true, 8000, 6424, 6635, 4500 * time.Millisecond},
		{"minutes and seconds", "Limit 8000, Used 7000, Requested 3000, please try again in 1m3.2s", true, 8000, 7000, 3000, 63200 * time.Millisecond},
		{"minutes only", "Limit 100000, Used 99000, Requested 5000. Please try again in 14m", true, 100000, 99000, 5000, 14 * time.Minute},
		{"milliseconds", "Limit 8000, Used 6000, Requested 3000. Please try again in 250ms", true, 8000, 6000, 3000, 250 * time.Millisecond},
		{"no wait given", "Limit 8000, Used 6000, Requested 3000", true, 8000, 6000, 3000, 0},
		{"a size cap has no Used", "Limit 8000, Requested 8201, please reduce your message size", false, 0, 0, 0, 0},
		{"unrelated", "connection reset by peer", false, 0, 0, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			window, ok := ParseRateWindow(test.text)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			if !ok {
				return
			}
			if window.Limit != test.limit || window.Used != test.used || window.Requested != test.requested || window.RetryAfter != test.wait {
				t.Fatalf("got %+v", window)
			}
		})
	}
}

func TestRateWindowCooldownAndCap(t *testing.T) {
	if got := (RateWindow{}).Cooldown(); got != 30*time.Second {
		t.Fatalf("unknown wait: %v", got)
	}
	if got := (RateWindow{RetryAfter: 400 * time.Millisecond}).Cooldown(); got != 5*time.Second {
		t.Fatalf("a sub-second wait still needs a floor: %v", got)
	}
	if got := (RateWindow{RetryAfter: 3 * time.Hour}).Cooldown(); got != 15*time.Minute {
		t.Fatalf("a daily window is capped: %v", got)
	}
	// A request that takes most of the window is kept off the route; a small one just waits.
	if got := (RateWindow{Limit: 8000, Used: 6424, Requested: 6635}).EffectiveRequestCap(); got != 4800 {
		t.Fatalf("cap = %d", got)
	}
	if got := (RateWindow{Limit: 8000, Used: 7000, Requested: 1500}).EffectiveRequestCap(); got != 0 {
		t.Fatalf("a small request should only wait, cap = %d", got)
	}
	if !IsPerMinute("on tokens per minute (TPM)") || IsPerMinute("on tokens per day (TPD)") {
		t.Fatal("per-minute detection")
	}
}

func TestProvenRoutesAreFavouredOnlyWhereProviderKeepsReturningNotFound(t *testing.T) {
	r := learningRouter(t)
	ModelsMutex.Lock()
	AvailableModels = []Model{
		{ID: "nv/dead-1", Provider: "nv", APIKeyEnv: "NV_KEY", Enabled: true, Modality: "text", Capability: 40},
		{ID: "nv/dead-2", Provider: "nv", APIKeyEnv: "NV_KEY", Enabled: true, Modality: "text", Capability: 40},
		{ID: "nv/dead-3", Provider: "nv", APIKeyEnv: "NV_KEY", Enabled: true, Modality: "text", Capability: 40},
		{ID: "nv/fresh", Provider: "nv", APIKeyEnv: "NV_KEY", Enabled: true, Modality: "text", Capability: 40},
		{ID: "nv/proven", Provider: "nv", APIKeyEnv: "NV_KEY", Enabled: true, Modality: "text", Capability: 40},
		{ID: "other/a", Provider: "other", APIKeyEnv: "OTHER_KEY", Enabled: true, Modality: "text", Capability: 40},
	}
	models := append([]Model(nil), AvailableModels...)
	ModelsMutex.Unlock()
	r.mu.Lock()
	r.succeeded = map[string]int{"nv/proven|NV_KEY": 2}
	r.mu.Unlock()

	pick := func() map[string]bool {
		seen := map[string]bool{}
		for tier := 0; tier <= 5; tier++ {
			if m := r.SelectModelWithPolicy("t", "agent", tier, 0, "text", RoutePolicy{}); m != nil {
				seen[m.Key()] = true
			}
		}
		return seen
	}
	// Two dead routes are not yet a pattern: an unused route is still a fair choice.
	r.MarkRouteDead(models[0], time.Now().Add(time.Hour), "not found")
	r.MarkRouteDead(models[1], time.Now().Add(time.Hour), "not found")
	// (selection results vary with tier; only the exclusion rule below is asserted)
	r.MarkRouteDead(models[2], time.Now().Add(time.Hour), "not found")
	for key := range pick() {
		if key == "nv/fresh|NV_KEY" {
			t.Fatalf("a never-used route of a provider with three dead routes was chosen over proven or other routes")
		}
		if key == "nv/dead-1|NV_KEY" || key == "nv/dead-2|NV_KEY" || key == "nv/dead-3|NV_KEY" {
			t.Fatalf("a dead route was chosen: %s", key)
		}
	}

	// With nothing proven and no other provider the fresh route is all that is left and must be used.
	r.mu.Lock()
	r.succeeded = nil
	r.mu.Unlock()
	ModelsMutex.Lock()
	AvailableModels = []Model{models[0], models[1], models[2], models[3]}
	ModelsMutex.Unlock()
	got := pick()
	if len(got) != 1 || !got["nv/fresh|NV_KEY"] {
		t.Fatalf("the preference emptied the candidate set: %v", got)
	}

	// Dead marks expire: after their time the provider is no longer suspect.
	r.mu.Lock()
	for key, dead := range r.dead {
		dead.until = time.Now().Add(-time.Second)
		r.dead[key] = dead
		r.unavailable[key] = dead.until
	}
	r.mu.Unlock()
	if got := pick(); len(got) == 0 {
		t.Fatal("nothing selectable after the dead marks expired")
	}
}
