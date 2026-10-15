package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// What a run learns about a route or a credential (a model the account cannot reach, a balance that
// is empty, a project that was denied) is still true when Forge is restarted a minute later. Without
// this every restart re-paid the same dead attempts first: the same route order, the same
// "insufficient balance", the same 404s, before any work started.
//
// The record is a small file beside the routing matrix. Every entry carries an expiry, so a provider
// that recovers is tried again without anyone editing anything, and a credential entry also carries a
// digest of the secret it was learned for, so replacing a key makes the entry void immediately.
// Authentication failures are never written: restarting after fixing a key is the normal remedy and
// must work. Set RETICLE_FORGET_ROUTE_HEALTH=1 (or delete the file) to start without any of it.

const (
	routeHealthFile = "route_health.json"
	// incompatibleModelFor is how long a model that cannot take chat/tool requests stays out. Such a
	// model does not change by itself, but catalogues are corrected, so it is not forever.
	incompatibleModelFor = 24 * time.Hour
	// billingRetryAfter is how soon an empty balance is probed again. A balance does not refill on its
	// own the way a rate window does, so the usual 15 minutes mostly buys more failed attempts.
	billingRetryAfter = 2 * time.Hour
)

// BillingRetryAfter exposes the empty-balance backoff to the dispatcher.
const BillingRetryAfter = billingRetryAfter

// IncompatibleModelFor exposes how long an incompatible model stays out.
const IncompatibleModelFor = incompatibleModelFor

type credentialState struct {
	until   time.Time
	reason  string
	digest  string
	persist bool
}

type persistedRoute struct {
	Until      time.Time `json:"until"`
	Reason     string    `json:"reason,omitempty"`
	Dead       bool      `json:"dead,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	Credential string    `json:"credential,omitempty"`
}

type persistedCredential struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason,omitempty"`
	Digest string    `json:"digest"`
}

type persistedHealth struct {
	Version     int                            `json:"version"`
	SavedAt     time.Time                      `json:"saved_at"`
	Routes      map[string]persistedRoute      `json:"routes,omitempty"`
	Credentials map[string]persistedCredential `json:"credentials,omitempty"`
}

// secretDigest identifies the value of a credential without storing it.
func secretDigest(envName string) string {
	value := os.Getenv(envName)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(envName + "\x00" + value))
	return hex.EncodeToString(sum[:])[:16]
}

func (r *ModelRouter) healthPath() string {
	return filepath.Join(filepath.Dir(r.getMatrixPath()), routeHealthFile)
}

// MarkCredentialUnavailable keeps every route of one credential out of selection until the given
// time. Unlike key health it is not replaced by a catalogue refresh. persist says whether a restart
// should remember it (see the file comment: authentication failures are not).
func (r *ModelRouter) MarkCredentialUnavailable(envName string, until time.Time, reason string, persist bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markCredentialLocked(envName, until, reason, persist)
}

// credentialBlockedLocked reports a credential that is known to be down. The caller holds r.mu.
func (r *ModelRouter) credentialBlockedLocked(envName string, now time.Time) bool {
	state, ok := r.credentials[envName]
	return ok && now.Before(state.until)
}

// ForgetRouteHealth clears everything learned about routes and credentials, in memory and on disk.
func (r *ModelRouter) ForgetRouteHealth() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unavailable, r.dead, r.credentials = nil, nil, nil
	_ = os.Remove(r.healthPath())
}

// saveHealthLocked writes the entries that should outlive the process. The caller holds r.mu.
func (r *ModelRouter) saveHealthLocked() {
	now := time.Now()
	state := persistedHealth{Version: 1, SavedAt: now.UTC(), Routes: map[string]persistedRoute{}, Credentials: map[string]persistedCredential{}}
	for key, until := range r.unavailable {
		if !now.Before(until) {
			continue
		}
		entry := persistedRoute{Until: until.UTC(), Reason: r.unavailableWhy[key]}
		if dead, ok := r.dead[key]; ok {
			entry.Dead, entry.Provider, entry.Credential = true, dead.provider, dead.credential
		}
		state.Routes[key] = entry
	}
	for envName, credential := range r.credentials {
		if credential.persist && now.Before(credential.until) && credential.digest != "" {
			state.Credentials[envName] = persistedCredential{Until: credential.until.UTC(), Reason: credential.reason, Digest: credential.digest}
		}
	}
	path := r.healthPath()
	if len(state.Routes) == 0 && len(state.Credentials) == 0 {
		_ = os.Remove(path)
		return
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".route-health-*.tmp")
	if err != nil {
		return
	}
	name := temporary.Name()
	_, writeErr := temporary.Write(data)
	closeErr := temporary.Close()
	if writeErr != nil || closeErr != nil || os.Rename(name, path) != nil {
		_ = os.Remove(name)
	}
}

// loadHealth restores what an earlier process learned and has not yet expired.
func (r *ModelRouter) loadHealth() {
	if strings.TrimSpace(os.Getenv("RETICLE_FORGET_ROUTE_HEALTH")) != "" {
		return
	}
	data, err := os.ReadFile(r.healthPath())
	if err != nil {
		return
	}
	var state persistedHealth
	if err := json.Unmarshal(data, &state); err != nil || state.Version != 1 {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	restored := 0
	for key, entry := range state.Routes {
		if !now.Before(entry.Until) {
			continue
		}
		if r.unavailable == nil {
			r.unavailable = map[string]time.Time{}
		}
		if r.unavailableWhy == nil {
			r.unavailableWhy = map[string]string{}
		}
		r.unavailable[key] = entry.Until
		r.unavailableWhy[key] = entry.Reason
		if entry.Dead {
			if r.dead == nil {
				r.dead = map[string]deadRoute{}
			}
			r.dead[key] = deadRoute{provider: entry.Provider, credential: entry.Credential, until: entry.Until}
		}
		restored++
	}
	for envName, entry := range state.Credentials {
		// A replaced key is a different credential; what was learned about the old one is void.
		if !now.Before(entry.Until) || entry.Digest == "" || entry.Digest != secretDigest(envName) {
			continue
		}
		if r.credentials == nil {
			r.credentials = map[string]credentialState{}
		}
		r.credentials[envName] = credentialState{until: entry.Until, reason: entry.Reason, digest: entry.Digest, persist: true}
		restored++
	}
	if restored > 0 && r.Logger != nil {
		r.Logger.Info("Restored route health from an earlier run", "entries", restored, "path", r.healthPath())
	}
}
