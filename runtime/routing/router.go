package routing

import (
	"math"
	"sort"
	"strings"
	"sync"

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

func NewRouter(l *logger.Logger, b *events.Bus) *ModelRouter {
	FetchAvailableModels(l)

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

	// Fallback to all models if none meet the strict required confidence
	if len(capable) == 0 {
		capable = AvailableModels
	}

	// Sort capable models by Capability ascending, then Cost ascending
	sort.Slice(capable, func(i, j int) bool {
		if capable[i].Capability == capable[j].Capability {
			return capable[i].Cost < capable[j].Cost
		}
		return capable[i].Capability < capable[j].Capability
	})

	effortMap := map[string]float64{
		"minimal":  0.0,
		"low":      0.2,
		"standard": 0.4,
		"elevated": 0.6,
		"high":     0.8,
		"absolute": 1.0,
	}

	percentile, exists := effortMap[strings.ToLower(effortStr)]
	if !exists {
		percentile = 0.4 // standard default
		effortStr = "standard"
	}

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

// PenalizeProvider drops the probability of all models that use the given apiKeyEnv to 0.0 for the specified agent.
func (r *ModelRouter) PenalizeProvider(agentID string, apiKeyEnv string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Matrix[agentID] == nil {
		r.Matrix[agentID] = make(map[string]float64)
	}

	for _, m := range AvailableModels {
		if m.APIKeyEnv == apiKeyEnv {
			r.Matrix[agentID][m.Key()] = 0.0
		}
	}
	r.Logger.Info("Provider penalized globally for agent", "agent_id", agentID, "api_key_env", apiKeyEnv)
}
