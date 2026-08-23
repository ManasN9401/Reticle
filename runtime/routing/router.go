package routing

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
)

// ModelRouter manages dynamic LLM selection using Bayesian utility estimates.
type ModelRouter struct {
	Logger  *logger.Logger
	Bus     *events.Bus
	
	// Matrix: agentID -> modelID -> SuccessProbability
	Matrix map[string]map[string]float64
	
	// In-flight task tracking: taskID -> modelID
	inFlight map[string]string
	
	mu sync.RWMutex
}

func NewRouter(l *logger.Logger, b *events.Bus, loadAll bool) *ModelRouter {
	FetchAvailableModels(l, loadAll)

	r := &ModelRouter{
		Logger:   l,
		Bus:      b,
		Matrix:   make(map[string]map[string]float64),
		inFlight: make(map[string]string),
	}
	r.subscribe()
	return r
}

func (r *ModelRouter) subscribe() {
	// WorkerCompleted: Increase probability
	r.Bus.Subscribe(events.EventType("WorkerCompleted"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		taskID, okTask := payload["task_id"].(string)
		workerID, okWorker := payload["worker_id"].(string)
		if okTask && okWorker {
			r.updateProbability(workerID, taskID, true)
		}
	})

	// WorkerFailed: Decrease probability
	r.Bus.Subscribe(events.EventType("WorkerFailed"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		taskID, okTask := payload["task_id"].(string)
		workerID, okWorker := payload["worker_id"].(string)
		if okTask && okWorker {
			r.updateProbability(workerID, taskID, false)
		}
	})
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

	if r.Matrix[agentID] == nil {
		r.Matrix[agentID] = make(map[string]float64)
	}

	currentProb, probExists := r.Matrix[agentID][modelID]
	if !probExists {
		currentProb = 0.90 // Optimistic prior
	}

	// Simple exponential moving average for Bayesian update
	alpha := 0.2
	target := 0.0
	if success {
		target = 1.0
	}
	
	newProb := (1.0-alpha)*currentProb + alpha*target
	r.Matrix[agentID][modelID] = newProb

	// Cleanup inflight
	delete(r.inFlight, taskID)
	
	r.Logger.Info("Model utility updated", "agent_id", agentID, "model_key", modelID, "success", success, "new_prob", newProb)
}

// SelectModel returns a model based on the requested effort tier, constrained by confidence.
func (r *ModelRouter) SelectModel(taskID string, agentID string, effortStr string, requiredConfidence float64) *Model {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Matrix[agentID] == nil {
		r.Matrix[agentID] = make(map[string]float64)
	}

	var capable []Model
	for _, m := range AvailableModels {
		if !m.Enabled {
			continue
		}
		
		mCopy := m
		prob, exists := r.Matrix[agentID][mCopy.Key()]
		if !exists {
			prob = 0.90 // Optimistic prior for cold starts
			r.Matrix[agentID][mCopy.Key()] = prob
		}

		if prob >= requiredConfidence {
			capable = append(capable, mCopy)
		}
	}

	// Fallback to all enabled models if none meet the strict required confidence
	if len(capable) == 0 {
		for _, m := range AvailableModels {
			if m.Enabled {
				capable = append(capable, m)
			}
		}
	}
	
	if len(capable) == 0 {
		r.Logger.Error("No models available for selection", "agent_id", agentID, "effort", effortStr)
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
			return capable[i].Cost < capable[j].Cost
		}
		return capable[i].Capability < capable[j].Capability
	})

	percentile := GetEffortPercentile(effortStr)


	idx := int(math.Round(percentile * float64(len(capable)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(capable) {
		idx = len(capable) - 1
	}

	bestModel := &capable[idx]

	r.inFlight[taskID] = bestModel.Key()
	r.Logger.Info("Model routed", "agent_id", agentID, "model_key", bestModel.Key(), "effort", effortStr, "capability", bestModel.Capability)
	return bestModel
}

// TrackForcedModel registers a task against a specific model, bypassing SelectModel but enabling telemetry.
func (r *ModelRouter) TrackForcedModel(taskID string, modelKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight[taskID] = modelKey
}

// PenalizeProvider globally disables all models that use the given apiKeyEnv across the entire application, and re-enables them after a 60-second cooldown.
func (r *ModelRouter) PenalizeProvider(agentID string, apiKeyEnv string) {
	r.mu.Lock()
	count := 0
	for i := range AvailableModels {
		if AvailableModels[i].APIKeyEnv == apiKeyEnv && AvailableModels[i].Enabled {
			AvailableModels[i].Enabled = false
			count++
		}
	}
	r.mu.Unlock()

	if count > 0 {
		r.Logger.Info("Provider penalized globally for ALL agents (60s cooldown)", "api_key_env", apiKeyEnv, "models_disabled", count)
		
		// Launch recovery timer
		go func() {
			time.Sleep(60 * time.Second)
			r.mu.Lock()
			defer r.mu.Unlock()
			recovered := 0
			for i := range AvailableModels {
				if AvailableModels[i].APIKeyEnv == apiKeyEnv && !AvailableModels[i].Enabled {
					AvailableModels[i].Enabled = true
					recovered++
				}
			}
			r.Logger.Info("Provider cooldown finished, models re-enabled", "api_key_env", apiKeyEnv, "models_recovered", recovered)
		}()
	}
}

// PenalizeModel globally disables a specific model across the entire application.
func (r *ModelRouter) PenalizeModel(modelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
	case "minimal": return -2
	case "low": return -1
	case "standard": return 0
	case "elevated": return 1
	case "high": return 2
	case "absolute": return 3
	default: return 0 // "auto"
	}
}

// TierToEffortString converts a tier index back to a string
func TierToEffortString(tier int) string {
	if tier < 0 {
		tier = 0
	}
	if tier > 5 {
		tier = 5
	}
	tiers := []string{"minimal", "low", "standard", "elevated", "high", "absolute"}
	return tiers[tier]
}

// GetEffortPercentile maps an effort string to a float percentile
func GetEffortPercentile(effortStr string) float64 {
	tier := GetEffortTier(effortStr)
	return float64(tier) * 0.2
}
