package routing

import (
	"fmt"
	"time"
)

// Potential excludes only disabled, wrong-modality and task-rejected routes;
// temporary health/capacity restrictions must still permit bounded waiting.
type RouteAvailability struct {
	Potential        int
	Eligible         int
	Untried          int
	UntriedProviders int
}

func (s RouteAvailability) String() string {
	return fmt.Sprintf("remaining_routes=%d, eligible_now=%d, untried_eligible_routes=%d, untried_eligible_providers=%d", s.Potential, s.Eligible, s.Untried, s.UntriedProviders)
}

func (r *ModelRouter) RouteAvailability(modality string, policy RoutePolicy) RouteAvailability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()
	if modality == "" {
		modality = "text"
	}
	var result RouteAvailability
	triedProviders := map[string]bool{}
	untriedProviders := map[string]bool{}
	for _, m := range AvailableModels {
		if policy.Tried[m.Key()] {
			triedProviders[m.Provider] = true
		}
	}
	now := time.Now()
	for _, m := range AvailableModels {
		allowed := m.Modality == modality
		if modality == "coding" || (modality == "text" && r.AllowTextToCodingFallback) {
			allowed = m.Modality != "image"
		}
		if !m.Enabled || !allowed || policy.Excluded[m.Key()] {
			continue
		}
		result.Potential++
		slot := ProviderSlot(m)
		if !modelRouteAvailableLocked(m, now) || (r.ProviderCapacity[slot] > 0 && r.ProviderInFlight[slot] >= r.ProviderCapacity[slot]) {
			continue
		}
		result.Eligible++
		if !policy.Tried[m.Key()] {
			result.Untried++
			if !triedProviders[m.Provider] {
				untriedProviders[m.Provider] = true
			}
		}
	}
	result.UntriedProviders = len(untriedProviders)
	return result
}
