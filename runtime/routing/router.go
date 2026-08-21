package routing

import (
	"math"
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

// SelectModel returns the cheapest model whose expected success rate exceeds the required confidence.
func (r *ModelRouter) SelectModel(taskID string, agentID string, requiredConfidence float64) *Model {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Matrix[agentID] == nil {
		r.Matrix[agentID] = make(map[string]float64)
	}

	var bestModel *Model
	minCost := math.MaxFloat64

	for _, m := range AvailableModels {
		// Use a local copy of m to take its address safely
		mCopy := m
		prob, exists := r.Matrix[agentID][mCopy.Key()]
		if !exists {
			prob = 0.90 // Optimistic prior for cold starts
			r.Matrix[agentID][mCopy.Key()] = prob
		}

		if prob >= requiredConfidence && mCopy.Cost < minCost {
			minCost = mCopy.Cost
			bestModel = &mCopy
		}
	}

	// Fallback to most capable (assumed to be highest cost if threshold not met)
	if bestModel == nil {
		maxCost := -1.0
		for _, m := range AvailableModels {
			mCopy := m
			if mCopy.Cost > maxCost {
				maxCost = mCopy.Cost
				bestModel = &mCopy
			}
		}
	}

	if bestModel != nil {
		// Track this task so we can update telemetry later
		r.inFlight[taskID] = bestModel.Key()
		r.Logger.Info("Model routed", "agent_id", agentID, "model_key", bestModel.Key())
	}
	return bestModel
}

// TrackForcedModel registers a task against a specific model, bypassing SelectModel but enabling telemetry.
func (r *ModelRouter) TrackForcedModel(taskID string, modelKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight[taskID] = modelKey
}
