package routing

import (
	"regexp"
	"strconv"
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
	if r.unavailable == nil {
		r.unavailable = map[string]time.Time{}
	}
	if !until.After(r.unavailable[key]) {
		return
	}
	r.unavailable[key] = until
	if r.Logger != nil {
		r.Logger.Info("Route marked unavailable for other tasks", "route", key, "until", until.Format(time.RFC3339), "reason", reason)
	}
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
