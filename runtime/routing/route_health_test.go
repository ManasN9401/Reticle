package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reticle/runtime/logger"
)

// restarted is a second router over the same state directory, as after restarting Forge.
func restarted(t *testing.T) *ModelRouter {
	t.Helper()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}
	r.loadHealth()
	return r
}

// blocked asks the router's own question: would this route be skipped for a task right now?
func blocked(r *ModelRouter, key string) bool {
	ModelsMutex.RLock()
	var model Model
	for _, candidate := range AvailableModels {
		if candidate.Key() == key {
			model = candidate
		}
	}
	ModelsMutex.RUnlock()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.routeBlockedLocked(model, RoutePolicy{}, time.Now())
}

func healthRouter(t *testing.T) *ModelRouter {
	t.Helper()
	t.Setenv("RETICLE_ROOT", t.TempDir())
	t.Setenv("RETICLE_FORGET_ROUTE_HEALTH", "")
	t.Setenv("LEARN_KEY_A", "secret-a")
	t.Setenv("LEARN_KEY_B", "secret-b")
	t.Setenv("LEARN_KEY_C", "secret-c")
	return learningRouter(t)
}

func TestARestartRemembersWhichRoutesAreDead(t *testing.T) {
	r := healthRouter(t)
	model := Model{ID: "big/a", Provider: "big", APIKeyEnv: "LEARN_KEY_C"}
	r.MarkRouteDead(model, time.Now().Add(2*time.Hour), "model not found for this credential")
	r.MarkRouteUnavailable("small/a|LEARN_KEY_A", time.Now().Add(time.Hour), "rate window")

	next := restarted(t)
	if !blocked(next, "big/a|LEARN_KEY_C") || !blocked(next, "small/a|LEARN_KEY_A") {
		t.Fatal("a restarted router would try routes the last run found dead")
	}
	if blocked(next, "small/a|LEARN_KEY_B") {
		t.Fatal("a route nobody reported on was kept out")
	}
	next.mu.RLock()
	_, dead := next.dead["big/a|LEARN_KEY_C"]
	next.mu.RUnlock()
	if !dead {
		t.Fatal("the provider-level count of unreachable routes was not restored")
	}
}

func TestExpiredEntriesAreNotRestored(t *testing.T) {
	r := healthRouter(t)
	r.MarkRouteUnavailable("big/a|LEARN_KEY_C", time.Now().Add(80*time.Millisecond), "short")
	time.Sleep(120 * time.Millisecond)
	if blocked(restarted(t), "big/a|LEARN_KEY_C") {
		t.Fatal("an expired entry kept a route out")
	}
}

func TestAnEmptyBalanceSurvivesARestartUntilTheKeyIsReplaced(t *testing.T) {
	r := healthRouter(t)
	r.PenalizeProviderForUntil("agent", "LEARN_KEY_C", "provider_quota", "insufficient balance", time.Now().Add(BillingRetryAfter))

	next := restarted(t)
	if !blocked(next, "big/a|LEARN_KEY_C") {
		t.Fatal("a credential with no balance would be tried again after a restart")
	}
	if blocked(next, "small/a|LEARN_KEY_A") || blocked(next, "small/a|LEARN_KEY_B") {
		t.Fatal("other credentials were affected")
	}

	t.Setenv("LEARN_KEY_C", "a-new-key-after-topping-up")
	if blocked(restarted(t), "big/a|LEARN_KEY_C") {
		t.Fatal("replacing the key did not clear what was learned about the old one")
	}
}

func TestAccessFailuresBlockTheCredentialButAreNotRememberedAcrossARestart(t *testing.T) {
	r := healthRouter(t)
	r.PenalizeProviderForUntil("agent", "LEARN_KEY_C", "provider_access", "project denied", time.Time{})
	if !blocked(r, "big/a|LEARN_KEY_C") {
		t.Fatal("a denied credential would be used")
	}
	// Restarting after fixing the key is the remedy; it must work without any other step.
	if blocked(restarted(t), "big/a|LEARN_KEY_C") {
		t.Fatal("a restart did not give a fixed credential another chance")
	}
}

func TestACredentialOutageIsNotLostWhenTheCatalogueRefreshes(t *testing.T) {
	r := healthRouter(t)
	r.PenalizeProviderForUntil("agent", "LEARN_KEY_C", "provider_quota", "insufficient balance", time.Now().Add(time.Hour))
	// FetchAvailableModels replaces the whole key-health list with what discovery just saw.
	ModelsMutex.Lock()
	KeyHealthStates, LockedKeys = nil, nil
	ModelsMutex.Unlock()
	if !blocked(r, "big/a|LEARN_KEY_C") {
		t.Fatal("a refresh cleared a known outage")
	}
}

func TestAnIncompatibleModelStaysOutForOtherTasksAndAcrossARestart(t *testing.T) {
	r := healthRouter(t)
	r.PenalizeModel("small/a")
	if !blocked(r, "small/a|LEARN_KEY_A") || !blocked(r, "small/a|LEARN_KEY_B") {
		t.Fatal("every route of the incompatible model must be out for other tasks")
	}
	// A new process builds its model list fresh (everything enabled); the router must still skip it.
	ModelsMutex.Lock()
	for i := range AvailableModels {
		AvailableModels[i].Enabled = true
	}
	ModelsMutex.Unlock()
	next := restarted(t)
	if !blocked(next, "small/a|LEARN_KEY_A") || !blocked(next, "small/a|LEARN_KEY_B") || blocked(next, "big/a|LEARN_KEY_C") {
		t.Fatal("the restart forgot the incompatible model, or lost the others")
	}
}

func TestRouteHealthCanBeIgnoredAndCleared(t *testing.T) {
	r := healthRouter(t)
	r.MarkRouteUnavailable("big/a|LEARN_KEY_C", time.Now().Add(time.Hour), "dead")
	path := r.healthPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nothing was written: %v", err)
	}

	t.Setenv("RETICLE_FORGET_ROUTE_HEALTH", "1")
	if blocked(restarted(t), "big/a|LEARN_KEY_C") {
		t.Fatal("RETICLE_FORGET_ROUTE_HEALTH did not start clean")
	}
	t.Setenv("RETICLE_FORGET_ROUTE_HEALTH", "")

	r.ForgetRouteHealth()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the record was not removed: %v", err)
	}
	if blocked(r, "big/a|LEARN_KEY_C") {
		t.Fatal("forgetting did not clear the live router")
	}
}

func TestACorruptRecordIsIgnored(t *testing.T) {
	r := healthRouter(t)
	if err := os.MkdirAll(filepath.Dir(r.healthPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.healthPath(), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if blocked(restarted(t), "big/a|LEARN_KEY_C") {
		t.Fatal("a damaged file blocked routing")
	}
}

func TestTheRecordNeverContainsASecret(t *testing.T) {
	r := healthRouter(t)
	r.PenalizeProviderForUntil("agent", "LEARN_KEY_C", "provider_quota", "insufficient balance", time.Now().Add(time.Hour))
	data, err := os.ReadFile(r.healthPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || strings.Contains(string(data), "secret-c") {
		t.Fatalf("the credential value is in the file: %s", data)
	}
}
