package agent

import (
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
)

// SubscriptionManager evaluates EventBus events against registered Subscriptions.
// When an event matches, it builds a Task and publishes a TaskReady event.
type SubscriptionManager struct {
	Logger        *logger.Logger
	Bus           *events.Bus
	subscriptions map[string]*Subscription
}

func NewSubscriptionManager(l *logger.Logger, b *events.Bus) *SubscriptionManager {
	return &SubscriptionManager{
		Logger:        l,
		Bus:           b,
		subscriptions: make(map[string]*Subscription),
	}
}

func (sm *SubscriptionManager) Register(sub *Subscription) {
	if sub.ID == "" {
		sm.Logger.Error("SubscriptionManager failed to register subscription: ID cannot be empty")
		return
	}
	sm.subscriptions[sub.ID] = sub
	sm.Logger.Info("SubscriptionManager registered subscription", "sub_id", sub.ID, "worker_id", sub.WorkerID)
}

func (sm *SubscriptionManager) Remove(subID string) {
	delete(sm.subscriptions, subID)
	sm.Logger.Info("SubscriptionManager removed subscription", "sub_id", subID)
}

func (sm *SubscriptionManager) Start() {
	sm.Bus.SubscribeAll(func(e events.RuntimeEvent) {
		for _, sub := range sm.subscriptions {
			if string(e.Type) == sub.EventType {
				if sub.Condition == nil || sub.Condition(e) {
					// Generate the task dynamically based on the event context
					task := sub.Generate(e)

					sm.Logger.Info("Subscription matched event", "sub_id", sub.ID, "event", e.Type)

					// Decoupled handoff: Publish a TaskReady event
					sm.Bus.Publish(events.EventType("TaskReady"), events.Component("subscription_manager"), task)
				}
			}
		}
	})
}
