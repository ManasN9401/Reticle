package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/reticle/runtime/events"
)

type EffectState string

const (
	EffectPrepared    EffectState = "prepared"
	EffectRunning     EffectState = "running"
	EffectSucceeded   EffectState = "succeeded"
	EffectFailed      EffectState = "failed"
	EffectCancelled   EffectState = "cancelled"
	EffectInterrupted EffectState = "interrupted"
)

type EffectRecord struct {
	ID              string      `json:"id"`
	AttemptID       string      `json:"attempt_id"`
	Adapter         string      `json:"adapter"`
	Target          string      `json:"target"`
	RequestHash     string      `json:"request_hash"`
	ExternalID      string      `json:"external_id,omitempty"`
	IdempotencyKey  string      `json:"idempotency_key,omitempty"`
	State           EffectState `json:"state"`
	CleanupOwner    string      `json:"cleanup_owner,omitempty"`
	CleanupDeadline *time.Time  `json:"cleanup_deadline,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	Detail          string      `json:"detail,omitempty"`
}

// EffectManager records operations that may outlive a worker. Adapters must
// reconcile interrupted effects before retrying them.
type EffectManager struct {
	mu      sync.Mutex
	path    string
	records map[string]EffectRecord
}

type EffectBeginRequest struct {
	Record EffectRecord
	Result chan error
}

type EffectFinishRequest struct {
	ID         string
	State      EffectState
	ExternalID string
	Detail     string
	Result     chan error
}

func NewPersistentEffectManager(path string) (*EffectManager, error) {
	m := &EffectManager{path: path, records: make(map[string]EffectRecord)}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &m.records); err != nil {
			return nil, fmt.Errorf("decode effect state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read effect state: %w", err)
	}
	changed := false
	for id, record := range m.records {
		if record.State == EffectRunning || record.State == EffectPrepared {
			record.State = EffectInterrupted
			record.Detail = "runtime restarted before completion was recorded"
			record.UpdatedAt = time.Now().UTC()
			m.records[id] = record
			changed = true
		}
	}
	if changed {
		if err := m.saveLocked(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *EffectManager) Start(bus *events.Bus) {
	bus.Subscribe("ExternalEffectBeginRequested", func(event events.RuntimeEvent) {
		request, ok := event.Payload.(EffectBeginRequest)
		if !ok {
			return
		}
		_, err := m.Begin(request.Record)
		request.Result <- err
	})
	bus.Subscribe("ExternalEffectFinishRequested", func(event events.RuntimeEvent) {
		request, ok := event.Payload.(EffectFinishRequest)
		if !ok {
			return
		}
		request.Result <- m.Finish(request.ID, request.State, request.ExternalID, request.Detail)
	})
}

func (m *EffectManager) Begin(record EffectRecord) (EffectRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if record.ID == "" || record.AttemptID == "" || record.Adapter == "" || record.Target == "" || record.RequestHash == "" {
		return EffectRecord{}, fmt.Errorf("effect id, attempt, adapter, target and request hash are required")
	}
	if existing, ok := m.records[record.ID]; ok {
		if existing.RequestHash != record.RequestHash || existing.Target != record.Target || existing.AttemptID != record.AttemptID || existing.Adapter != record.Adapter {
			return EffectRecord{}, fmt.Errorf("effect identity reused for a different request")
		}
		return existing, nil
	}
	now := time.Now().UTC()
	record.State = EffectRunning
	record.CreatedAt = now
	record.UpdatedAt = now
	m.records[record.ID] = record
	if err := m.saveLocked(); err != nil {
		delete(m.records, record.ID)
		return EffectRecord{}, err
	}
	return record, nil
}

func (m *EffectManager) Finish(id string, state EffectState, externalID, detail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state != EffectSucceeded && state != EffectFailed && state != EffectCancelled && state != EffectInterrupted {
		return fmt.Errorf("invalid terminal effect state %q", state)
	}
	record, ok := m.records[id]
	if !ok {
		return fmt.Errorf("unknown effect %q", id)
	}
	if record.State == EffectSucceeded || record.State == EffectFailed || record.State == EffectCancelled {
		if record.State == state && record.ExternalID == externalID {
			return nil
		}
		return fmt.Errorf("effect %q is already terminal as %s", id, record.State)
	}
	before := record
	record.State = state
	record.ExternalID = externalID
	record.Detail = detail
	record.UpdatedAt = time.Now().UTC()
	m.records[id] = record
	if err := m.saveLocked(); err != nil {
		m.records[id] = before
		return err
	}
	return nil
}

func (m *EffectManager) Get(id string) (EffectRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[id]
	return record, ok
}

// Reconcile resolves an interrupted record from adapter-observed remote state.
// A retry is represented by a new effect ID; interrupted records never move
// back to running because that would hide uncertainty in the original call.
func (m *EffectManager) Reconcile(id string, observed EffectState, externalID, detail string) error {
	m.mu.Lock()
	record, ok := m.records[id]
	m.mu.Unlock()
	if !ok || record.State != EffectInterrupted {
		return fmt.Errorf("effect %q is not interrupted", id)
	}
	if observed != EffectSucceeded && observed != EffectFailed && observed != EffectCancelled {
		return fmt.Errorf("reconciliation requires an observed terminal state")
	}
	return m.Finish(id, observed, externalID, detail)
}

func (m *EffectManager) saveLocked() error {
	data, err := json.Marshal(m.records)
	if err != nil {
		return fmt.Errorf("encode effect state: %w", err)
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".effects-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, m.path); err != nil {
		backup := m.path + ".previous"
		_ = os.Remove(backup)
		if moveErr := os.Rename(m.path, backup); moveErr != nil && !os.IsNotExist(moveErr) {
			return err
		}
		if moveErr := os.Rename(name, m.path); moveErr != nil {
			_ = os.Rename(backup, m.path)
			return moveErr
		}
		_ = os.Remove(backup)
	}
	return nil
}
