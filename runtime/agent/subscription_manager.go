package agent

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
)

// SubscriptionManager evaluates EventBus events against registered Subscriptions.
// When an event matches, it builds a Task and publishes a TaskCreated event.
type SubscriptionManager struct {
	mu            sync.Mutex
	unsubscribe   map[string]func()
	Logger        *logger.Logger
	Bus           *events.Bus
	subscriptions map[string]*Subscription
	autoSeq       atomic.Uint64
}

func NewSubscriptionManager(l *logger.Logger, b *events.Bus) *SubscriptionManager {
	return &SubscriptionManager{
		Logger:        l,
		Bus:           b,
		subscriptions: make(map[string]*Subscription),
		unsubscribe:   make(map[string]func()),
	}
}

func (sm *SubscriptionManager) Register(sub *Subscription) {
	if sub.ID == "" {
		sm.Logger.Error("SubscriptionManager failed to register subscription: ID cannot be empty")
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if dispose := sm.unsubscribe[sub.ID]; dispose != nil {
		dispose()
	}
	sm.subscriptions[sub.ID] = sub

	// Register specific handler on the event bus directly to the asynchronous pool
	sm.unsubscribe[sub.ID] = sm.Bus.Subscribe(events.EventType(sub.EventType), func(e events.RuntimeEvent) {
		matched, data := sm.evaluateFilters(sub.Filters, e.Payload)
		if !matched {
			return
		}

		seq := sm.autoSeq.Add(1)
		execID := fmt.Sprintf("exec-auto-%06d", seq)

		sm.Logger.Info("Automation matched event", "sub_id", sub.ID, "event", e.Type)

		sm.Bus.Publish(events.EventType("AutomationTriggered"), events.Component("subscription_manager"), map[string]any{
			"subscription_id":  sub.ID,
			"execution":        execID,
			"agent_id":         string(sub.WorkerID),
			"trigger_event":    e.Type,
			"trigger_event_id": e.ID,
			"trigger_payload":  data,
		})

		task := Task{
			ID:          TaskID(fmt.Sprintf("%s|%s", execID, sub.WorkerID)),
			AgentID:     string(sub.WorkerID),
			ExecutionID: execID,
			Origin:      "automation",
			Inputs:      []TaskInput{{Name: "trigger", Data: data}},
		}

		sm.Bus.Publish(events.EventType("TaskCreated"), events.Component("subscription_manager"), task)
	})

	sm.Logger.Info("SubscriptionManager registered subscription", "sub_id", sub.ID, "worker_id", sub.WorkerID)
}

func (sm *SubscriptionManager) evaluateFilters(filters map[string]string, payload any) (bool, map[string]any) {
	b, err := json.Marshal(payload)
	var data map[string]any

	if err == nil {
		json.Unmarshal(b, &data)
	}

	if len(filters) == 0 {
		return true, data
	}

	if data == nil {
		return false, nil
	}

	for k, v := range filters {
		val, ok := data[k]
		if !ok {
			return false, nil
		}
		if fmt.Sprintf("%v", val) != v {
			return false, nil
		}
	}
	return true, data
}

func (sm *SubscriptionManager) Remove(subID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if dispose := sm.unsubscribe[subID]; dispose != nil {
		dispose()
		delete(sm.unsubscribe, subID)
	}
	delete(sm.subscriptions, subID)
	sm.Logger.Info("SubscriptionManager removed subscription", "sub_id", subID)
}

// Start no longer uses SubscribeAll, it is handled dynamically by Register.
func (sm *SubscriptionManager) Start() {
	// Intentionally left blank.
}
