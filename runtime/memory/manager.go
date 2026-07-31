package memory

import (
	"github.com/hyperparallel/runtime/events"
)

// Manager is the umbrella structure that holds all state managers.
// It subscribes to the Event Bus and translates update requests into physical state mutations.
type Manager struct {
	Artifacts *ArtifactStore
	Runtime   *RuntimeState
	Session   *SessionState
	bus       *events.Bus
}

func NewManager(am *ArtifactStore, rs *RuntimeState, ss *SessionState, b *events.Bus) *Manager {
	m := &Manager{
		Artifacts: am,
		Runtime:   rs,
		Session:   ss,
		bus:       b,
	}
	m.subscribe()
	return m
}

func (m *Manager) subscribe() {
	m.bus.Subscribe(events.EventType("MemoryUpdateRequested"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		key, ok1 := payload["key"].(MemoryKey)
		val, ok2 := payload["value"]

		if ok1 && ok2 {
			m.Runtime.set(key, val)
			
			// Acknowledge the mutation to the rest of the system
			m.bus.Publish(events.EventType("MemoryUpdated"), events.Component("memory"), map[string]any{
				"key": key,
			})
		}
	})

	// Subscribe to Artifact storage requests
	m.bus.Subscribe(events.EventType("ArtifactStoreRequested"), func(e events.RuntimeEvent) {
		artifact, ok := e.Payload.(*Artifact)
		if ok {
			m.Artifacts.save(artifact)
			
			// If it's version 1, it's newly stored. Otherwise it's a new revision!
			eventType := "ArtifactStored"
			if artifact.Version > 1 {
				eventType = "ArtifactVersionCreated"
			}
			
			m.bus.Publish(events.EventType(eventType), events.Component("memory"), artifact)
		}
	})
}
