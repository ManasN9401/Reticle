package toolbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrJournalPersistence = errors.New("effect journal persistence failed")

func (b *Broker) failPersistence(session *attemptSession, phase string) {
	b.mu.Lock()
	b.persistenceFailed = true
	for _, session := range b.sessions {
		for _, call := range session.calls {
			if call.active && call.cancel != nil {
				call.cancel()
			}
		}
	}
	b.mu.Unlock()
	if b.bus != nil {
		b.bus.Publish("RuntimePersistenceFailed", "tool_broker", map[string]any{"phase": phase, "execution": session.executionID,
			"task_id": session.taskID, "execution_ids": []string{session.executionID}, "restart_required": true, "error": ErrJournalPersistence.Error()})
	}
}

func invokeAdapter(ctx context.Context, adapter Adapter, callID, tool string, args json.RawMessage) (result any, effect EffectCertainty, err error) {
	if err := ctx.Err(); err != nil {
		return nil, NoEffect, err
	}
	defer func() {
		if recover() != nil {
			result, effect, err = nil, Uncertain, fmt.Errorf("adapter panicked; external state requires reconciliation")
		}
	}()
	return adapter.Call(ctx, callID, tool, args)
}

// CallIntent contains identities and digests only: arguments, credentials and
// model-visible results must not enter the effect ledger.
type CallIntent struct {
	ID, AttemptID, ExecutionID, TaskID, Adapter, Tool, RequestHash string
}

type CallOutcome struct {
	OK     bool
	Effect EffectCertainty
	Code   string
}

// EffectJournal must commit Begin before the adapter is entered. An interrupted
// or uncertain intent must be reconciled before an equivalent call can run.
type EffectJournal interface {
	BeginCall(CallIntent) error
	FinishCall(string, CallOutcome) error
}

// SetEffectJournal must be called before the first attempt is opened.
func (b *Broker) SetEffectJournal(journal EffectJournal) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.attemptsOpened || len(b.sessions) != 0 {
		return fmt.Errorf("effect journal cannot change after attempts open")
	}
	if journal == nil {
		return fmt.Errorf("effect journal is required")
	}
	b.journal = journal
	return nil
}
