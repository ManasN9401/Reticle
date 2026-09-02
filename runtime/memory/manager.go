package memory

import (
	"github.com/reticle/runtime/events"
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
	m.bus.Subscribe(events.EventType("MemoryWriteRequested"), func(e events.RuntimeEvent) {
		entry, ok := e.Payload.(MemoryEntry)
		if !ok {
			return
		}

		m.Runtime.set(entry)
		
		m.bus.Publish(events.EventType("MemoryUpdated"), events.Component("memory"), map[string]any{
			"scope":    entry.Scope,
			"scope_id": entry.ScopeID,
			"key":      entry.Key,
		})
	})

	m.bus.Subscribe(events.EventType("MemoryReadRequested"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		scope, _ := payload["scope"].(MemoryScope)
		scopeID, _ := payload["scope_id"].(string)
		key, _ := payload["key"].(string)

		val, found := m.Runtime.Get(scope, scopeID, key)
		
		m.bus.Publish(events.EventType("MemoryReadCompleted"), events.Component("memory"), map[string]any{
			"scope":    scope,
			"scope_id": scopeID,
			"key":      key,
			"value":    val.Value,
			"found":    found,
		})
	})

	// Subscribe to Artifact storage requests
	m.bus.Subscribe(events.EventType("ArtifactsProduced"), func(e events.RuntimeEvent) {
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
