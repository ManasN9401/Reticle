package agent

import (
	"fmt"
	"time"

	"github.com/reticle/runtime/toolbroker"
)

// BeginCall atomically rejects an unresolved equivalent operation and writes a
// running record. Identical arguments on another task are independent intents.
func (m *EffectManager) BeginCall(intent toolbroker.CallIntent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if intent.ID == "" || intent.AttemptID == "" || intent.ExecutionID == "" || intent.TaskID == "" || intent.Adapter == "" || intent.Tool == "" || intent.RequestHash == "" {
		return fmt.Errorf("broker effect requires full attempt and operation identity")
	}
	if _, exists := m.records[intent.ID]; exists {
		return fmt.Errorf("broker call already recorded; reconcile instead of replaying")
	}
	for _, previous := range m.records {
		if previous.ExecutionID == intent.ExecutionID && previous.TaskID == intent.TaskID && previous.Adapter == intent.Adapter && previous.Target == intent.Tool && previous.RequestHash == intent.RequestHash &&
			(previous.State == EffectPrepared || previous.State == EffectRunning || previous.State == EffectInterrupted) {
			return fmt.Errorf("equivalent external effect %s requires reconciliation", previous.ID)
		}
	}
	now := time.Now().UTC()
	m.records[intent.ID] = EffectRecord{ID: intent.ID, AttemptID: intent.AttemptID, ExecutionID: intent.ExecutionID, TaskID: intent.TaskID,
		Adapter: intent.Adapter, Target: intent.Tool, RequestHash: intent.RequestHash, State: EffectRunning, CreatedAt: now, UpdatedAt: now,
		CleanupOwner: "toolbroker"}
	if err := m.saveLocked(); err != nil {
		delete(m.records, intent.ID)
		return fmt.Errorf("%w: %v", toolbroker.ErrJournalPersistence, err)
	}
	return nil
}

func (m *EffectManager) FinishCall(id string, outcome toolbroker.CallOutcome) error {
	state := EffectInterrupted
	if outcome.Effect == toolbroker.NoEffect {
		state = EffectFailed
		if outcome.OK {
			state = EffectSucceeded
		}
	}
	// Uncertain is never converted to a success receipt merely because a remote
	// server returned an answer. A provider-specific observer must reconcile it.
	detail := "broker outcome: " + string(outcome.Effect)
	if outcome.Code != "" {
		detail += " (" + outcome.Code + ")"
	}
	return m.Finish(id, state, "", detail)
}
