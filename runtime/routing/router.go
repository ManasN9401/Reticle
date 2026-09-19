package routing

import (
	"encoding/json"
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

// ModelRouter selects models using an exponential moving average of task outcomes.
type ModelRouter struct {
	Logger *logger.Logger
	Bus    *events.Bus

	// Matrix: agentID -> modelID -> SuccessProbability
	Matrix map[string]map[string]float64

	// In-flight task tracking: taskID -> modelID
	inFlight map[string]string

	// AIMD Congestion Control
	ProviderCapacity map[string]int
	ProviderInFlight map[string]int

	UseBayesianRouting bool

	mu sync.RWMutex
}

func NewRouter(l *logger.Logger, b *events.Bus, loadAll bool) *ModelRouter {
	FetchAvailableModels(l, loadAll)

	r := &ModelRouter{
		Logger:             l,
		Bus:                b,
		Matrix:             make(map[string]map[string]float64),
		inFlight:           make(map[string]string),
		ProviderCapacity:   make(map[string]int),
		ProviderInFlight:   make(map[string]int),
		UseBayesianRouting: true,
	}
	r.loadMatrix()
	return r
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
	ModelsMutex.RLock()
	for _, m := range AvailableModels {
		if m.Key() == modelID {
			provider = m.APIKeyEnv
			break
		}
	}
	ModelsMutex.RUnlock()

	if true {
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
			if float64(r.ProviderInFlight[provider]) >= float64(capacity)*0.5 {
				if capacity < 200 {
					r.ProviderCapacity[provider] = capacity + 1
				}
			}
		}
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

// SelectModel returns a model based on the requested effort tier, constrained by confidence.
func (r *ModelRouter) SelectModel(taskID string, agentID string, effortTier int, requiredConfidence float64, modality string) *Model {
	r.mu.Lock()
	defer r.mu.Unlock()

	if modality == "" {
		modality = "text"
	}

	if r.Matrix[agentID] == nil {
		r.Matrix[agentID] = make(map[string]float64)
	}

	var capable []Model
	ModelsMutex.RLock()
	for _, m := range AvailableModels {
		if !m.Enabled || time.Now().Before(m.CooldownUntil) {
			continue
		}
		if m.Modality != modality {
			continue
		}

		mCopy := m
		{
			// Predictive Rate Limiting check
			capacity := r.ProviderCapacity[mCopy.APIKeyEnv]
			if capacity == 0 {
				capacity = 50 // Default
				if mCopy.APIKeyEnv == "" || strings.Contains(strings.ToLower(mCopy.APIKeyEnv), "ollama") || strings.Contains(strings.ToLower(mCopy.APIKeyEnv), "local") {
					capacity = 2
				}
				r.ProviderCapacity[mCopy.APIKeyEnv] = capacity
			}

			if r.ProviderInFlight[mCopy.APIKeyEnv] >= capacity {
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

			if prob >= requiredConfidence {
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
			if m.Enabled && !time.Now().Before(m.CooldownUntil) && m.Modality == modality {
				// Still respect predictive limits on fallback
				capacity := r.ProviderCapacity[m.APIKeyEnv]
				if capacity > 0 && r.ProviderInFlight[m.APIKeyEnv] >= capacity {
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
			if m.Enabled && !time.Now().Before(m.CooldownUntil) && m.Modality == "text" {
				capacity := r.ProviderCapacity[m.APIKeyEnv]
				if capacity > 0 && r.ProviderInFlight[m.APIKeyEnv] >= capacity {
					continue
				}
				capable = append(capable, m)
			}
		}
		ModelsMutex.RUnlock()
	}

	// Cross-modality fallback logic (e.g. text -> coding)
	if len(capable) == 0 && modality == "text" {
		r.Logger.Info("WARNING: No 'text' models available. Falling back to a standard 'coding' model.", "agent_id", agentID, "task_id", taskID)
		ModelsMutex.RLock()
		for _, m := range AvailableModels {
			if m.Enabled && !time.Now().Before(m.CooldownUntil) && m.Modality == "coding" {
				capacity := r.ProviderCapacity[m.APIKeyEnv]
				if capacity > 0 && r.ProviderInFlight[m.APIKeyEnv] >= capacity {
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
		r.Logger.Info("WARNING: No models of requested modality available. Falling back to ANY non-image model.", "agent_id", agentID, "modality", modality)
		ModelsMutex.RLock()
		for _, m := range AvailableModels {
			if m.Enabled && !time.Now().Before(m.CooldownUntil) && m.Modality != "image" {
				capacity := r.ProviderCapacity[m.APIKeyEnv]
				if capacity > 0 && r.ProviderInFlight[m.APIKeyEnv] >= capacity {
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

	r.inFlight[taskID] = bestModel.Key()
	r.ProviderInFlight[bestModel.APIKeyEnv]++
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
			r.ProviderInFlight[m.APIKeyEnv]++
			break
		}
	}
}

// PenalizeProvider globally disables all models that use the given apiKeyEnv across the entire application, and re-enables them after a 60-second cooldown.
func (r *ModelRouter) PenalizeProvider(agentID, apiKeyEnv string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()
	for i := range AvailableModels {
		if AvailableModels[i].APIKeyEnv == apiKeyEnv {
			AvailableModels[i].CooldownUntil = time.Now().Add(60 * time.Second)
		}
	}
	capacity := r.ProviderCapacity[apiKeyEnv]
	if capacity < 2 {
		capacity = 2
	}
	r.ProviderCapacity[apiKeyEnv] = capacity / 2
}

// PenalizeProviderFamily cools every credential slot for a provider when the
// provider reports an account-wide limit. Rotating variable names cannot evade
// limits such as OpenRouter's free-model daily allowance.
func (r *ModelRouter) PenalizeProviderFamily(provider string) {
	if provider == "" {
		return
	}
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()
	until := time.Now().Add(60 * time.Second)
	for i := range AvailableModels {
		if strings.EqualFold(AvailableModels[i].Provider, provider) {
			AvailableModels[i].CooldownUntil = until
		}
	}
}

// PenalizeModel globally disables a specific model across the entire application.
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
		if m.Key() == key && r.ProviderInFlight[m.APIKeyEnv] > 0 {
			r.ProviderInFlight[m.APIKeyEnv]--
			return
		}
	}
}

func (r *ModelRouter) SetLearning(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.UseBayesianRouting = enabled
}
