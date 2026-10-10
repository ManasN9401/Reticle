package routing

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
)

// ExplainNoRoute reports the concrete constraints currently excluding models.
// It is intentionally diagnostic only and does not mutate router state.
func (r *ModelRouter) ExplainNoRoute(modality string) string {
	if modality == "" {
		modality = "text"
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()

	var total, disabled, cooling, unavailableKey, wrongModality, saturated, eligible int
	now := time.Now()
	for _, model := range AvailableModels {
		total++
		if !model.Enabled {
			disabled++
			continue
		}
		if keyUnavailableLocked(model.APIKeyEnv) {
			unavailableKey++
			continue
		}
		if now.Before(model.CooldownUntil) {
			cooling++
			continue
		}
		if !servesChatTasks(model) {
			wrongModality++
			continue
		}
		if model.Modality != modality && !(modality == "coding" && model.Modality == "text") && !(modality == "text" && r.AllowTextToCodingFallback && model.Modality == "coding") {
			wrongModality++
			continue
		}
		slot := ProviderSlot(model)
		capacity := r.ProviderCapacity[slot]
		if capacity > 0 && r.ProviderInFlight[slot] >= capacity {
			saturated++
			continue
		}
		eligible++
	}
	if total == 0 {
		return "no models were discovered; inspect provider discovery health"
	}
	return fmt.Sprintf("no route for modality %q (models=%d, eligible=%d, disabled=%d, credential_unavailable=%d, cooling_down=%d, capacity_saturated=%d, modality_mismatch=%d)", modality, total, eligible, disabled, unavailableKey, cooling, saturated, wrongModality)
}

// ModelRouter selects models using an exponential moving average of task outcomes.
type ModelRouter struct {
	Logger    *logger.Logger
	Bus       *events.Bus
	Providers *ProviderRegistry
	loadAll   bool
	refreshMu sync.Mutex

	// Matrix: agentID -> modelID -> SuccessProbability
	Matrix map[string]map[string]float64

	// In-flight task tracking: taskID -> modelID
	inFlight map[string]string

	// AIMD Congestion Control
	ProviderCapacity map[string]int
	ProviderInFlight map[string]int
	// ProviderPenaltyUntil prevents concurrent 429 responses from repeatedly
	// halving the same credential during one cooldown window.
	ProviderPenaltyUntil map[string]time.Time

	UseBayesianRouting        bool
	AllowTextToCodingFallback bool

	mu sync.RWMutex
}

func (r *ModelRouter) SetTextToCodingFallback(enabled bool) {
	r.mu.Lock()
	r.AllowTextToCodingFallback = enabled
	r.mu.Unlock()
}

func NewRouter(l *logger.Logger, b *events.Bus, loadAll bool) *ModelRouter {
	providerPath := filepath.Join(routerRoot(), ".reticle", "providers.json")
	providers, err := NewProviderRegistry(providerPath)
	if err != nil {
		l.Error("Provider registry failed to load", "path", providerPath, "error", err)
		providers = &ProviderRegistry{path: providerPath, providers: make(map[string]ProviderConfig), statuses: make(map[string]ProviderStatus)}
	}
	FetchAvailableModels(l, loadAll, providers)

	r := &ModelRouter{
		Logger:               l,
		Bus:                  b,
		Providers:            providers,
		loadAll:              loadAll,
		Matrix:               make(map[string]map[string]float64),
		inFlight:             make(map[string]string),
		ProviderCapacity:     make(map[string]int),
		ProviderInFlight:     make(map[string]int),
		ProviderPenaltyUntil: make(map[string]time.Time),
		UseBayesianRouting:   true,
	}
	r.loadMatrix()
	return r
}

func routerRoot() string {
	if root := os.Getenv("RETICLE_ROOT"); root != "" {
		return root
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func (r *ModelRouter) RefreshModels() {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	FetchAvailableModels(r.Logger, r.loadAll, r.Providers)
}

func (r *ModelRouter) ListProviders() []ProviderStatus {
	if r == nil || r.Providers == nil {
		return []ProviderStatus{}
	}
	return r.Providers.List()
}

func (r *ModelRouter) PutProvider(config ProviderConfig) (ProviderStatus, error) {
	if r == nil || r.Providers == nil {
		return ProviderStatus{}, fmt.Errorf("provider registry unavailable")
	}
	validated, err := r.Providers.Put(config)
	if err != nil {
		return ProviderStatus{}, err
	}
	r.RefreshModels()
	for _, status := range r.Providers.List() {
		if status.Config.ID == validated.ID {
			return status, nil
		}
	}
	return ProviderStatus{Config: validated, State: "not_tested"}, nil
}

func (r *ModelRouter) DeleteProvider(id string) error {
	if r == nil || r.Providers == nil {
		return fmt.Errorf("provider registry unavailable")
	}
	if err := r.Providers.Delete(id); err != nil {
		return err
	}
	r.RefreshModels()
	return nil
}

func (r *ModelRouter) getMatrixPath() string {
	if root := os.Getenv("RETICLE_ROOT"); root != "" {
		return filepath.Join(root, ".reticle", "routing_matrix.json")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".reticle/routing_matrix.json" // fallback
	}
	// Assuming workspace is the current working directory, we look for .reticle
	cwd, err := os.Getwd()
	if err == nil {
		return filepath.Join(cwd, ".reticle", "routing_matrix.json")
	}
	return filepath.Join(homeDir, ".reticle", "routing_matrix.json")
}

func (r *ModelRouter) loadMatrix() {
	path := r.getMatrixPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		r.Logger.Error("Could not read persisted model routing preferences", "path", path, "error", err)
		return
	}
	var matrix map[string]map[string]float64
	if err := json.Unmarshal(data, &matrix); err != nil {
		r.Logger.Error("Could not decode persisted model routing preferences", "path", path, "error", err)
		return
	}
	r.Matrix = matrix
	r.Logger.Info("Loaded persisted model routing preferences", "path", path)
}

func (r *ModelRouter) saveMatrix() error {
	path := r.getMatrixPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.Matrix, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".routing-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err == nil {
		return nil
	}
	backup := path + ".previous"
	_ = os.Remove(backup)
	if moveErr := os.Rename(path, backup); moveErr != nil && !os.IsNotExist(moveErr) {
		return err
	}
	if moveErr := os.Rename(tmpName, path); moveErr != nil {
		_ = os.Rename(backup, path)
		return moveErr
	}
	_ = os.Remove(backup)
	return nil
}

// UpdateProbability allows external components (like Dispatcher) to manually report success/failure
func (r *ModelRouter) UpdateProbability(agentID, taskID string, success bool) {
	r.updateProbability(agentID, taskID, success)
}

func (r *ModelRouter) updateProbability(agentID, taskID string, success bool) {

	r.mu.Lock()
	defer r.mu.Unlock()

	modelID, exists := r.inFlight[taskID]
	if !exists {
		return // not an LLM task or untracked
	}

	var provider string
	var apiKeyEnv string
	ModelsMutex.RLock()
	for _, m := range AvailableModels {
		if m.Key() == modelID {
			provider = ProviderSlot(m)
			apiKeyEnv = m.APIKeyEnv
			break
		}
	}
	ModelsMutex.RUnlock()

	if provider != "" {
		occupancy := r.ProviderInFlight[provider]
		if r.ProviderInFlight[provider] > 0 {
			r.ProviderInFlight[provider]--
		}

		// Additive Increase: only if successful and we are operating near capacity
		if success {
			capacity := r.ProviderCapacity[provider]
			if capacity == 0 {
				capacity = 50 // Default starting capacity
				if provider == "" || strings.Contains(strings.ToLower(provider), "ollama") || strings.Contains(strings.ToLower(provider), "local") {
					capacity = 2
				}
			}
			// Only push capacity up if we are actually constrained (using at least 50% of the ceiling)
			if float64(occupancy) >= float64(capacity)*0.5 {
				if capacity < 200 {
					r.ProviderCapacity[provider] = capacity + 1
				}
			}
		}
	}
	if success && apiKeyEnv != "" {
		SetKeyHealth(apiKeyEnv, KeyHealthy, "", time.Time{})
	}

	if !r.UseBayesianRouting {
		delete(r.inFlight, taskID)
		return
	}
	if r.Matrix[agentID] == nil {
		r.Matrix[agentID] = make(map[string]float64)
	}

	currentProb, probExists := r.Matrix[agentID][modelID]
	if !probExists {
		currentProb = 0.90 // Optimistic prior
	}

	// Exponential moving average; this is not a calibrated success probability.
	alpha := 0.2
	target := 0.0
	if success {
		target = 1.0
	}

	newProb := (1.0-alpha)*currentProb + alpha*target
	r.Matrix[agentID][modelID] = newProb

	// Cleanup inflight
	delete(r.inFlight, taskID)

	if err := r.saveMatrix(); err != nil {
		r.Logger.Error("Could not persist model routing preferences", "error", err)
	}

	r.Logger.Info("Model utility updated", "agent_id", agentID, "model_key", modelID, "success", success, "new_prob", newProb)
}

func modelRouteAvailableLocked(model Model, now time.Time) bool {
	return model.Enabled && !now.Before(model.CooldownUntil) && !keyUnavailableLocked(model.APIKeyEnv)
}

// SelectModel returns a model based on the requested effort tier, constrained by confidence.
func (r *ModelRouter) SelectModel(taskID string, agentID string, effortTier int, requiredConfidence float64, modality string) *Model {
	return r.SelectModelWithPolicy(taskID, agentID, effortTier, requiredConfidence, modality, RoutePolicy{})
}

// RoutePolicy is owned by one dispatcher task. Hard exclusions survive the
// confidence fallback; preferences never bypass availability or modality checks.
type RoutePolicy struct {
	Excluded       map[string]bool
	Tried          map[string]bool
	AvoidProviders map[string]bool
	PreferredKey   string
}

func (r *ModelRouter) SelectModelWithPolicy(taskID string, agentID string, effortTier int, requiredConfidence float64, modality string, policy RoutePolicy) *Model {
	r.mu.Lock()
	defer r.mu.Unlock()

	if modality == "" {
		modality = "text"
	}

	if r.Matrix[agentID] == nil {
		r.Matrix[agentID] = make(map[string]float64)
	}

	var capable []Model
	now := time.Now()
	available := func(m Model) bool {
		return !policy.Excluded[m.Key()] && servesChatTasks(m) && modelRouteAvailableLocked(m, now)
	}
	ModelsMutex.RLock()
	for _, m := range AvailableModels {
		if !available(m) {
			continue
		}
		if m.Modality != modality {
			continue
		}

		mCopy := m
		{
			// Predictive Rate Limiting check
			slot := ProviderSlot(mCopy)
			capacity := r.ProviderCapacity[slot]
			if capacity == 0 {
				capacity = 50 // Default
				if mCopy.APIKeyEnv == "" {
					capacity = 2
				}
				r.ProviderCapacity[slot] = capacity
			}

			if r.ProviderInFlight[slot] >= capacity {
				// Provider is currently at max predictive capacity, skip to prevent 429
				continue
			}

		}
		if r.UseBayesianRouting {
			prob, exists := r.Matrix[agentID][mCopy.Key()]
			if !exists {
				prob = 0.90 // Optimistic prior for cold starts
				r.Matrix[agentID][mCopy.Key()] = prob
			}

			if prob >= requiredConfidence || mCopy.Key() == policy.PreferredKey {
				capable = append(capable, mCopy)
			}
		} else {
			capable = append(capable, mCopy)
		}
	}
	ModelsMutex.RUnlock()

	// Fallback to all enabled models if none meet the strict required confidence
	if len(capable) == 0 {
		ModelsMutex.RLock()
		for _, m := range AvailableModels {
			if available(m) && m.Modality == modality {
				// Still respect predictive limits on fallback
				slot := ProviderSlot(m)
				capacity := r.ProviderCapacity[slot]
				if capacity > 0 && r.ProviderInFlight[slot] >= capacity {
					continue
				}
				capable = append(capable, m)
			}
		}
		ModelsMutex.RUnlock()
	}

	// Cross-modality fallback logic (e.g. coding -> text)
	if len(capable) == 0 && modality == "coding" {
		r.Logger.Info("WARNING: No 'coding' models available. Falling back to a standard 'text' model.", "agent_id", agentID, "task_id", taskID)
		ModelsMutex.RLock()
		for _, m := range AvailableModels {
			if available(m) && m.Modality == "text" {
				slot := ProviderSlot(m)
				capacity := r.ProviderCapacity[slot]
				if capacity > 0 && r.ProviderInFlight[slot] >= capacity {
					continue
				}
				capable = append(capable, m)
			}
		}
		ModelsMutex.RUnlock()
	}

	// Cross-modality fallback logic (e.g. text -> coding)
	if len(capable) == 0 && modality == "text" && r.AllowTextToCodingFallback {
		r.Logger.Info("WARNING: No 'text' models available. Falling back to a standard 'coding' model.", "agent_id", agentID, "task_id", taskID)
		ModelsMutex.RLock()
		for _, m := range AvailableModels {
			if available(m) && m.Modality == "coding" {
				slot := ProviderSlot(m)
				capacity := r.ProviderCapacity[slot]
				if capacity > 0 && r.ProviderInFlight[slot] >= capacity {
					continue
				}
				capable = append(capable, m)
			}
		}
		ModelsMutex.RUnlock()
	}

	// Required image capability is never relaxed.
	if len(capable) == 0 && modality == "image" {
		return nil
	}
	// Final generic fallback
	if len(capable) == 0 {
		fallbackScope := "any non-image model"
		if modality == "text" && !r.AllowTextToCodingFallback {
			fallbackScope = "another text model"
		}
		r.Logger.Info("WARNING: No models of requested modality available. Trying a fallback.", "agent_id", agentID, "modality", modality, "fallback_scope", fallbackScope)
		ModelsMutex.RLock()
		for _, m := range AvailableModels {
			allowedModality := m.Modality != "image"
			if modality == "text" && !r.AllowTextToCodingFallback {
				allowedModality = m.Modality == "text"
			}
			if available(m) && allowedModality {
				slot := ProviderSlot(m)
				capacity := r.ProviderCapacity[slot]
				if capacity > 0 && r.ProviderInFlight[slot] >= capacity {
					continue
				}
				capable = append(capable, m)
			}
		}
		ModelsMutex.RUnlock()
	}

	if len(capable) == 0 {
		r.Logger.Error("No models available for selection", "agent_id", agentID, "effort", effortTier)
		return nil
	}

	// Repair a proven request limit on the same route first. Otherwise prefer
	// untried routes and, only after correlated failures, another provider.
	prefer := func(matches func(Model) bool) {
		var preferred []Model
		for _, model := range capable {
			if matches(model) {
				preferred = append(preferred, model)
			}
		}
		if len(preferred) > 0 {
			capable = preferred
		}
	}
	preferredAvailable := false
	for _, m := range capable {
		if m.Key() == policy.PreferredKey {
			preferredAvailable = true
			break
		}
	}
	if preferredAvailable {
		prefer(func(m Model) bool { return m.Key() == policy.PreferredKey })
	} else {
		prefer(func(m Model) bool { return !policy.Tried[m.Key()] })
		prefer(func(m Model) bool { return !policy.AvoidProviders[m.Provider] })
	}

	// Sort capable models by Capability ascending, then Cost ascending, then Priority
	sort.Slice(capable, func(i, j int) bool {
		if capable[i].Capability == capable[j].Capability {
			if capable[i].Cost == capable[j].Cost {
				// Prioritize _3 keys
				iHas3 := strings.HasSuffix(capable[i].APIKeyEnv, "_3")
				jHas3 := strings.HasSuffix(capable[j].APIKeyEnv, "_3")
				if iHas3 && !jHas3 {
					return true
				}
				if jHas3 && !iHas3 {
					return false
				}
			}
			if capable[i].Cost == capable[j].Cost {
				return capable[i].Key() < capable[j].Key()
			}
			if capable[i].Cost < 0 {
				return false
			}
			if capable[j].Cost < 0 {
				return true
			}
			return capable[i].Cost < capable[j].Cost
		}
		return capable[i].Capability < capable[j].Capability
	})

	percentile := float64(effortTier) * 0.2

	idx := int(math.Round(percentile * float64(len(capable)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(capable) {
		idx = len(capable) - 1
	}

	bestModel := &capable[idx]

	if r.inFlight == nil {
		r.inFlight = make(map[string]string)
	}
	r.inFlight[taskID] = bestModel.Key()
	r.ProviderInFlight[ProviderSlot(*bestModel)]++
	r.Logger.Info("Model routed", "agent_id", agentID, "model_key", bestModel.Key(), "effort_tier", effortTier, "capability", bestModel.Capability)
	return bestModel
}

// TrackForcedModel registers a task against a specific model, bypassing SelectModel but enabling telemetry.
func (r *ModelRouter) TrackForcedModel(taskID string, modelKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()
	for _, m := range AvailableModels {
		if m.Key() == modelKey || m.ID == modelKey {
			r.inFlight[taskID] = m.Key()
			r.ProviderInFlight[ProviderSlot(m)]++
			break
		}
	}
}

func (r *ModelRouter) Model(modelKey string) *Model {
	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()
	for _, model := range AvailableModels {
		if model.Key() == modelKey || model.ID == modelKey {
			copy := model
			return &copy
		}
	}
	return nil
}

// PenalizeProvider retains the historical rate-limit behavior for callers that
// do not provide a classified reason.
func (r *ModelRouter) PenalizeProvider(agentID, apiKeyEnv string) {
	r.PenalizeProviderFor(agentID, apiKeyEnv, "provider_rate_limit", "")
}

// PenalizeProviderFor records the reason as well as applying routing policy.
// Only actual rate limits reduce predictive capacity. Authentication remains
// unavailable until credentials are refreshed; quotas receive a later probe.
func (r *ModelRouter) PenalizeProviderFor(agentID, apiKeyEnv, category, reason string) {
	r.PenalizeProviderForUntil(agentID, apiKeyEnv, category, reason, time.Time{})
}

// PenalizeProviderForUntil is PenalizeProviderFor for a provider that reported when
// an exhausted quota resets: the credential stays unavailable until then instead of
// being probed again after the default 15 minutes.
func (r *ModelRouter) PenalizeProviderForUntil(agentID, apiKeyEnv, category, reason string, resetAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()
	now := time.Now()
	retryAt := now.Add(60 * time.Second)
	for i := range AvailableModels {
		if AvailableModels[i].APIKeyEnv == apiKeyEnv {
			AvailableModels[i].CooldownUntil = retryAt
		}
	}
	status := KeyRateLimited
	switch category {
	case "provider_access":
		status = KeyAuthenticationFailed
		retryAt = time.Time{}
	case "provider_quota", "provider_account_quota":
		status = KeyQuotaExhausted
		retryAt = now.Add(15 * time.Minute)
		if resetAt.After(retryAt) {
			retryAt = resetAt
		}
	case "provider_rate_limit":
		if r.ProviderPenaltyUntil == nil {
			r.ProviderPenaltyUntil = make(map[string]time.Time)
		}
		if !now.Before(r.ProviderPenaltyUntil[apiKeyEnv]) {
			capacity := r.ProviderCapacity[apiKeyEnv]
			if capacity < 2 {
				capacity = 2
			}
			r.ProviderCapacity[apiKeyEnv] = capacity / 2
			r.ProviderPenaltyUntil[apiKeyEnv] = retryAt
		}
	}
	health := KeyHealth{Key: apiKeyEnv, Status: status, Reason: reason, ObservedAt: now.UTC(), RetryAt: retryAt}
	found := false
	for i := range KeyHealthStates {
		if KeyHealthStates[i].Key == apiKeyEnv {
			KeyHealthStates[i] = health
			found = true
			break
		}
	}
	if !found {
		KeyHealthStates = append(KeyHealthStates, health)
	}
	rebuildLockedKeysLocked()
}

// PenalizeProviderFamily cools every credential slot for a provider when the
// provider reports an account-wide limit. Rotating variable names cannot evade
// limits such as OpenRouter's free-model daily allowance.
func (r *ModelRouter) PenalizeProviderFamily(provider string) {
	r.PenalizeProviderFamilyUntil(provider, time.Time{})
}

// PenalizeProviderFamilyUntil also honours a provider-reported quota reset time.
func (r *ModelRouter) PenalizeProviderFamilyUntil(provider string, resetAt time.Time) {
	if provider == "" {
		return
	}
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()
	now := time.Now()
	until := now.Add(60 * time.Second)
	keys := make(map[string]struct{})
	for i := range AvailableModels {
		if strings.EqualFold(AvailableModels[i].Provider, provider) {
			AvailableModels[i].CooldownUntil = until
			if AvailableModels[i].APIKeyEnv != "" {
				keys[AvailableModels[i].APIKeyEnv] = struct{}{}
			}
		}
	}
	for key := range keys {
		retryAt := now.Add(15 * time.Minute)
		if resetAt.After(retryAt) {
			retryAt = resetAt
		}
		health := KeyHealth{Key: key, Status: KeyQuotaExhausted, Reason: provider + " reported an account-wide quota limit", ObservedAt: now.UTC(), RetryAt: retryAt}
		found := false
		for i := range KeyHealthStates {
			if KeyHealthStates[i].Key == key {
				KeyHealthStates[i] = health
				found = true
				break
			}
		}
		if !found {
			KeyHealthStates = append(KeyHealthStates, health)
		}
	}
	rebuildLockedKeysLocked()
}

// CoolProviderSlot handles temporary transport/service failures without changing
// credential health or predictive capacity. Later probes can recover normally.
func (r *ModelRouter) CoolProviderSlot(slot string, delay time.Duration) {
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()
	until := time.Now().Add(delay)
	for i := range AvailableModels {
		if ProviderSlot(AvailableModels[i]) == slot && AvailableModels[i].CooldownUntil.Before(until) {
			AvailableModels[i].CooldownUntil = until
		}
	}
}

// PenalizeModel disables an incompatible model, leaving other models available.
func (r *ModelRouter) PenalizeModel(modelID string) {
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()

	for i := range AvailableModels {
		// match by ID or full key
		if AvailableModels[i].ID == modelID || AvailableModels[i].Key() == modelID {
			AvailableModels[i].Enabled = false
			r.Logger.Info("Model disabled globally due to fatal error", "model_id", AvailableModels[i].ID)
		}
	}
}

// GetEffortTier maps an effort string to an integer tier 0-5
func GetEffortTier(effortStr string) int {
	effortMap := map[string]int{
		"minimal":  0,
		"low":      1,
		"standard": 2,
		"elevated": 3,
		"high":     4,
		"absolute": 5,
	}
	tier, exists := effortMap[strings.ToLower(effortStr)]
	if !exists {
		return 2 // standard default
	}
	return tier
}

// GetTierDelta translates a global UI setting into a modifier delta
func GetTierDelta(globalStr string) int {
	switch strings.ToLower(globalStr) {
	case "minimal":
		return -2
	case "low":
		return -1
	case "standard":
		return 0
	case "elevated":
		return 1
	case "high":
		return 2
	case "absolute":
		return 3
	default:
		return 0 // "auto"
	}
}

func (r *ModelRouter) Release(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.inFlight[taskID]
	if !ok {
		return
	}
	delete(r.inFlight, taskID)
	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()
	for _, m := range AvailableModels {
		slot := ProviderSlot(m)
		if m.Key() == key && r.ProviderInFlight[slot] > 0 {
			r.ProviderInFlight[slot]--
			return
		}
	}
}

func (r *ModelRouter) SetLearning(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.UseBayesianRouting = enabled
}
