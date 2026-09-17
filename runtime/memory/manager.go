package memory

import (
	"fmt"
	"strings"
	"sync"

	"github.com/reticle/runtime/events"
)

// Manager is the umbrella structure that holds all state managers.
// It subscribes to the Event Bus and translates update requests into physical state mutations.
type Manager struct {
	Artifacts         *ArtifactStore
	Runtime           *RuntimeState
	Session           *SessionState
	bus               *events.Bus
	persistence       *SnapshotFile
	commitMu          sync.Mutex
	committedAttempts map[string]struct{}
}

func NewManager(am *ArtifactStore, rs *RuntimeState, ss *SessionState, b *events.Bus) *Manager {
	return newManager(am, rs, ss, b, nil)
}

func NewPersistentManager(am *ArtifactStore, rs *RuntimeState, ss *SessionState, b *events.Bus, path string) (*Manager, error) {
	persistence := NewSnapshotFile(path)
	committed, err := persistence.LoadWithCommits(rs, am)
	if err != nil {
		return nil, err
	}
	m := newManager(am, rs, ss, b, persistence)
	m.committedAttempts = committed
	return m, nil
}

func newManager(am *ArtifactStore, rs *RuntimeState, ss *SessionState, b *events.Bus, persistence *SnapshotFile) *Manager {
	m := &Manager{
		Artifacts:         am,
		Runtime:           rs,
		Session:           ss,
		bus:               b,
		persistence:       persistence,
		committedAttempts: make(map[string]struct{}),
	}
	m.subscribe()
	return m
}

func (m *Manager) persist() error {
	if m.persistence == nil {
		return nil
	}
	if err := m.persistence.SaveWithCommits(m.Runtime, m.Artifacts, m.committedAttempts); err != nil {
		m.bus.Publish("MemoryPersistenceFailed", "memory", map[string]any{"reason": err.Error(), "restart_recovery_at_risk": true})
		return err
	}
	return nil
}

func (m *Manager) subscribe() {
	m.bus.Subscribe(events.EventType("MemoryWriteRequested"), func(e events.RuntimeEvent) {
		entry, ok := e.Payload.(MemoryEntry)
		var result chan error
		if request, requestOK := e.Payload.(MemoryWriteRequest); requestOK {
			entry, result, ok = request.Entry, request.Result, true
		}
		if !ok {
			return
		}
		m.commitMu.Lock()
		defer m.commitMu.Unlock()

		before, existed := m.Runtime.Get(entry.Scope, entry.ScopeID, entry.Key)
		stored, err := m.Runtime.set(entry)
		if err != nil {
			m.bus.Publish("MemoryWriteRejected", "memory", map[string]any{"scope": entry.Scope, "scope_id": entry.ScopeID, "key": entry.Key, "reason": err.Error()})
			if result != nil {
				result <- err
			}
			return
		}
		if err = m.persist(); err != nil {
			m.Runtime.rollback(entry, before, existed)
			m.bus.Publish("MemoryWriteRejected", "memory", map[string]any{"scope": entry.Scope, "scope_id": entry.ScopeID, "key": entry.Key, "reason": err.Error()})
			if result != nil {
				result <- err
			}
			return
		}
		if result != nil {
			result <- nil
		}

		m.bus.Publish(events.EventType("MemoryUpdated"), events.Component("memory"), map[string]any{
			"scope":    entry.Scope,
			"scope_id": entry.ScopeID,
			"key":      entry.Key,
			"version":  stored.Version,
		})
	})

	m.bus.Subscribe(events.EventType("MemoryReadRequested"), func(e events.RuntimeEvent) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		scope, _ := payload["scope"].(MemoryScope)
		if text, ok := payload["scope"].(string); ok {
			scope = MemoryScope(text)
		}
		scopeID, _ := payload["scope_id"].(string)
		key, _ := payload["key"].(string)

		val, found := m.Runtime.Get(scope, scopeID, key)

		m.bus.Publish(events.EventType("MemoryReadCompleted"), events.Component("memory"), map[string]any{
			"scope":      scope,
			"scope_id":   scopeID,
			"key":        key,
			"value":      val.Value,
			"found":      found,
			"version":    val.Version,
			"request_id": payload["request_id"],
		})
	})

	storeArtifact := func(artifact *Artifact, result chan error) {
		m.commitMu.Lock()
		defer m.commitMu.Unlock()
		if artifact == nil {
			if result != nil {
				result <- fmt.Errorf("artifact is required")
			}
			return
		}
		before, _ := m.Artifacts.GetAllVersions(artifact.ID)
		m.Artifacts.save(artifact)
		if err := m.persist(); err != nil {
			_ = m.Artifacts.restoreSeries(artifact.ID, before)
			if result != nil {
				result <- err
			}
			return
		}
		eventType := "ArtifactStored"
		if artifact.Version > 1 {
			eventType = "ArtifactVersionCreated"
		}
		m.bus.Publish(events.EventType(eventType), events.Component("memory"), artifact)
		if result != nil {
			result <- nil
		}
	}
	m.bus.Subscribe(events.EventType("ArtifactsProduced"), func(e events.RuntimeEvent) {
		artifact, _ := e.Payload.(*Artifact)
		storeArtifact(artifact, nil)
	})
	m.bus.Subscribe(events.EventType("ArtifactWriteRequested"), func(e events.RuntimeEvent) {
		request, ok := e.Payload.(ArtifactWriteRequest)
		if ok {
			storeArtifact(request.Artifact, request.Result)
		}
	})
	m.bus.Subscribe(events.EventType("TaskResultCommitRequested"), func(e events.RuntimeEvent) {
		request, ok := e.Payload.(ResultCommitRequest)
		if !ok {
			return
		}
		m.commitMu.Lock()
		defer m.commitMu.Unlock()
		if request.AttemptID != "" {
			if _, exists := m.committedAttempts[request.AttemptID]; exists {
				request.Result <- nil
				return
			}
		}
		type priorEntry struct {
			requested MemoryEntry
			value     MemoryEntry
			existed   bool
		}
		priors := make([]priorEntry, 0, len(request.Entries))
		stored := make([]MemoryEntry, 0, len(request.Entries))
		rollbackEntries := func() {
			for i := len(priors) - 1; i >= 0; i-- {
				prior := priors[i]
				m.Runtime.rollback(prior.requested, prior.value, prior.existed)
			}
		}
		for _, entry := range request.Entries {
			previous, existed := m.Runtime.Get(entry.Scope, entry.ScopeID, entry.Key)
			priors = append(priors, priorEntry{entry, previous, existed})
			value, err := m.Runtime.set(entry)
			if err != nil {
				rollbackEntries()
				request.Result <- err
				return
			}
			stored = append(stored, value)
		}
		var artifactBefore []*Artifact
		if request.Artifact != nil {
			artifactBefore, _ = m.Artifacts.GetAllVersions(request.Artifact.ID)
			m.Artifacts.save(request.Artifact)
		}
		if request.AttemptID != "" {
			m.committedAttempts[request.AttemptID] = struct{}{}
		}
		if err := m.persist(); err != nil {
			delete(m.committedAttempts, request.AttemptID)
			if request.Artifact != nil {
				_ = m.Artifacts.restoreSeries(request.Artifact.ID, artifactBefore)
			}
			rollbackEntries()
			request.Result <- err
			return
		}
		for _, entry := range stored {
			m.bus.Publish("MemoryUpdated", "memory", map[string]any{"scope": entry.Scope, "scope_id": entry.ScopeID, "key": entry.Key, "version": entry.Version})
		}
		if request.Artifact != nil {
			eventType := "ArtifactStored"
			if request.Artifact.Version > 1 {
				eventType = "ArtifactVersionCreated"
			}
			m.bus.Publish(events.EventType(eventType), "memory", request.Artifact)
		}
		request.Result <- nil
	})

	releaseExecution := func(e events.RuntimeEvent) {
		m.commitMu.Lock()
		defer m.commitMu.Unlock()
		var execution string
		switch payload := e.Payload.(type) {
		case map[string]any:
			execution, _ = payload["execution"].(string)
		case map[string]string:
			execution = payload["execution"]
		}
		if execution == "" {
			return
		}
		before := m.Runtime.Snapshot()
		removed := m.Runtime.DeleteScope(ScopeExecution, execution)
		removedAttempts := 0
		removedAttemptIDs := make([]string, 0)
		for attemptID := range m.committedAttempts {
			if strings.HasPrefix(attemptID, execution+"/") {
				delete(m.committedAttempts, attemptID)
				removedAttempts++
				removedAttemptIDs = append(removedAttemptIDs, attemptID)
			}
		}
		if removed > 0 || removedAttempts > 0 {
			if err := m.persist(); err != nil {
				_ = m.Runtime.Restore(before)
				for _, attemptID := range removedAttemptIDs {
					m.committedAttempts[attemptID] = struct{}{}
				}
				return
			}
			m.bus.Publish("MemoryScopeReleased", "memory", map[string]any{"scope": ScopeExecution, "scope_id": execution, "entries": removed})
		}
	}
	m.bus.Subscribe("WorkflowCompleted", releaseExecution)
	m.bus.Subscribe("WorkflowFailed", releaseExecution)
	m.bus.Subscribe("ExecutionKilled", releaseExecution)
	m.bus.Subscribe("RuntimeShutdown", func(events.RuntimeEvent) {
		m.commitMu.Lock()
		defer m.commitMu.Unlock()
		_ = m.persist()
	})
}
