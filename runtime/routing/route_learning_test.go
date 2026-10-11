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
