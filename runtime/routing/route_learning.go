package routing

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What one run learns about a single route (model|credential) is a fact about the route, not
// about the task that happened to find it. Without this every node of a workflow re-discovered
// the same dead model and the same too-small rate limit, two to five attempts each.

// MarkRouteUnavailable keeps a route out of selection until the given time, for every task.
// It is in-memory only: a restart forgets it, and the time bound lets a transient provider
// problem heal without anyone editing configuration.
func (r *ModelRouter) MarkRouteUnavailable(key string, until time.Time, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.markRouteLocked(key, until, reason) {
		r.saveHealthLocked()
	}
}

// markRouteLocked records a route outage. It reports whether anything changed. The caller holds r.mu.
func (r *ModelRouter) markRouteLocked(key string, until time.Time, reason string) bool {
	if r.unavailable == nil {
		r.unavailable = map[string]time.Time{}
	}
	if r.unavailableWhy == nil {
		r.unavailableWhy = map[string]string{}
	}
	if !until.After(r.unavailable[key]) {
		return false
	}
	r.unavailable[key] = until
	r.unavailableWhy[key] = reason
	if r.Logger != nil {
		r.Logger.Info("Route marked unavailable for other tasks", "route", key, "until", until.Format(time.RFC3339), "reason", reason)
	}
	return true
}

// markCredentialLocked is MarkCredentialUnavailable for a caller that holds r.mu.
func (r *ModelRouter) markCredentialLocked(envName string, until time.Time, reason string, persist bool) {
	if envName == "" || !until.After(time.Now()) {
		return
	}
	if r.credentials == nil {
		r.credentials = map[string]credentialState{}
	}
	if existing, ok := r.credentials[envName]; ok && !until.After(existing.until) {
		return
	}
	r.credentials[envName] = credentialState{until: until, reason: reason, digest: secretDigest(envName), persist: persist}
	if r.Logger != nil {
		r.Logger.Info("Credential marked unavailable", "credential", envName, "until", until.Format(time.RFC3339), "reason", reason, "remembered_across_restarts", persist)
	}
	r.saveHealthLocked()
}

// MarkRouteDead is MarkRouteUnavailable for a route the provider says it does not serve to this
// credential. It is also counted per provider and credential: a catalogue that lists hundreds of
// models the account cannot reach hands every node a fresh dead one, so after a few the router
// prefers that provider's routes that have worked (see suspectProviderLocked).
func (r *ModelRouter) MarkRouteDead(model Model, until time.Time, reason string) {
	r.MarkRouteUnavailable(model.Key(), until, reason)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dead == nil {
		r.dead = map[string]deadRoute{}
	}
	r.dead[model.Key()] = deadRoute{provider: model.Provider, credential: model.APIKeyEnv, until: until}
	r.saveHealthLocked()
}

type deadRoute struct {
	provider, credential string
	until                time.Time
}

// deadRoutesSuspect is how many unreachable routes of one provider and credential make the rest of
// its never-used routes suspect.
const deadRoutesSuspect = 3

// suspectProviderLocked reports whether the model's provider and credential have returned
// "not found" for at least deadRoutesSuspect distinct routes that are still marked dead.
// The caller holds r.mu.
func (r *ModelRouter) suspectProviderLocked(m Model, now time.Time) bool {
	count := 0
	for _, dead := range r.dead {
		if dead.provider == m.Provider && dead.credential == m.APIKeyEnv && now.Before(dead.until) {
			count++
		}
	}
	return count >= deadRoutesSuspect
}

// ProvenRoute reports whether the route has completed work before in this run. Caller holds r.mu.
func (r *ModelRouter) provenLocked(key string) bool {
	return r.succeeded[key] > 0
}

// RateWindow is a provider's report that a per-minute (or per-day) allowance is partly used:
// "Limit 8000, Used 6424, Requested 6635 ... try again in 4.5s". Unlike a size cap the request
// would fit an empty window; unlike an exhausted account it clears on its own.
type RateWindow struct {
	Limit, Used, Requested int
	RetryAfter             time.Duration
}

var (
	rateWindowPattern = regexp.MustCompile(`(?i)limit\s+(\d+)\s*,\s*used\s+(\d+)\s*,\s*requested\s+(\d+)`)
	// "try again in 1m3.2s", "in 14m", "in 250ms", "in 2 hours": a run of number+unit pairs.
	retryAfterPattern = regexp.MustCompile(`(?i)try again in\s+((?:\d+(?:\.\d+)?\s*(?:ms|h|m|s)[a-z]*\s*)+)`)
	durationPart      = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(ms|h|m|s)`)
)

// ParseRateWindow extracts a rate window from a provider's error text.
func ParseRateWindow(text string) (RateWindow, bool) {
	match := rateWindowPattern.FindStringSubmatch(text)
	if len(match) != 4 {
		return RateWindow{}, false
	}
	var window RateWindow
	window.Limit, _ = strconv.Atoi(match[1])
	window.Used, _ = strconv.Atoi(match[2])
	window.Requested, _ = strconv.Atoi(match[3])
	if wait := retryAfterPattern.FindStringSubmatch(text); len(wait) == 2 {
		var total time.Duration
		for _, part := range durationPart.FindAllStringSubmatch(wait[1], -1) {
			amount, err := strconv.ParseFloat(part[1], 64)
			if err != nil {
				continue
			}
			unit := map[string]time.Duration{"ms": time.Millisecond, "s": time.Second, "m": time.Minute, "h": time.Hour}[strings.ToLower(part[2])]
			total += time.Duration(amount * float64(unit))
		}
		window.RetryAfter = total
	}
	return window, window.Limit > 0
}

// Cooldown is how long to keep only this route out of selection: as long as the provider said,
// within sane bounds, and a short default when it did not say.
func (w RateWindow) Cooldown() time.Duration {
	const floor, ceiling, unknown = 5 * time.Second, 15 * time.Minute, 30 * time.Second
	switch {
	case w.RetryAfter <= 0:
		return unknown
	case w.RetryAfter < floor:
		return floor
	case w.RetryAfter > ceiling:
		return ceiling
	}
	return w.RetryAfter
}

// EffectiveRequestCap is the largest request worth sending to the route: a request that alone
// takes most of the window will meet a partly used one whenever anything else ran in the same
// minute. Zero means the request is small enough that waiting is the right answer.
func (w RateWindow) EffectiveRequestCap() int {
	if w.Limit <= 0 || w.Requested*10 <= w.Limit*6 {
		return 0
	}
	return w.Limit * 6 / 10
}

// IsPerMinute reports whether text describes a per-minute window; a per-day one is a quota.
func IsPerMinute(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "per minute") || strings.Contains(lower, "(tpm)") || strings.Contains(lower, "(rpm)")
}

// LearnRequestLimit records the largest request (prompt plus reply, in tokens) the provider
// accepted or reported for a route. The smallest reported value wins.
func (r *ModelRouter) LearnRequestLimit(key string, tokens int) {
	if tokens <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.requestLimits == nil {
		r.requestLimits = map[string]int{}
	}
	if current := r.requestLimits[key]; current != 0 && current <= tokens {
		return
	}
	r.requestLimits[key] = tokens
	if r.Logger != nil {
		r.Logger.Info("Route request limit learned", "route", key, "tokens", tokens)
	}
}

// RoutesOfModel lists the route keys that serve the same model through the same provider,
// typically one per credential.
func (r *ModelRouter) RoutesOfModel(id, provider string) []string {
	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()
	var keys []string
	for _, m := range AvailableModels {
		if m.ID == id && m.Provider == provider {
			keys = append(keys, m.Key())
		}
	}
	return keys
}

// routeBlockedLocked reports routes that are known to fail for this task. The caller holds r.mu.
func (r *ModelRouter) routeBlockedLocked(m Model, policy RoutePolicy, now time.Time) bool {
	key := m.Key()
	if until, ok := r.unavailable[key]; ok && now.Before(until) {
		return true
	}
	if r.credentialBlockedLocked(m.APIKeyEnv, now) {
		return true
	}
	limit := r.requestLimits[key]
	return limit > 0 && policy.MinRequestTokens > limit
}

var (
	// Groq: "... on tokens per minute (TPM): Limit 8000, Requested 11399, please reduce ...".
	// A per-day or already-partly-used limit reads "Limit N, Used M, Requested K" and is not a size cap.
	requestLimitPattern = regexp.MustCompile(`(?i)limit\s+(\d+)\s*,\s*requested\s+(\d+)`)
	// OpenAI style: "This model's maximum context length is 8192 tokens. However, ...".
	contextLimitPattern = regexp.MustCompile(`(?i)maximum context length is (\d+) tokens`)
)

// ParseRequestLimit extracts a per-request token cap from a provider's "too large" error, or 0.
func ParseRequestLimit(text string) int {
	if match := requestLimitPattern.FindStringSubmatch(text); len(match) == 3 {
		limit, _ := strconv.Atoi(match[1])
		requested, _ := strconv.Atoi(match[2])
		if limit > 0 && requested > limit {
			return limit
		}
	}
	if match := contextLimitPattern.FindStringSubmatch(text); len(match) == 2 {
		limit, _ := strconv.Atoi(match[1])
		return limit
	}
	return 0
}
