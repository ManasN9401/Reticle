package toolbroker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type journalFixture struct {
	intents             []CallIntent
	outcomes            []CallOutcome
	beginErr, finishErr error
}

func (j *journalFixture) BeginCall(intent CallIntent) error {
	j.intents = append(j.intents, intent)
	return j.beginErr
}
func (j *journalFixture) FinishCall(_ string, outcome CallOutcome) error {
	j.outcomes = append(j.outcomes, outcome)
	return j.finishErr
}

type journalAdapter struct {
	call func(context.Context, string, string, json.RawMessage) (any, EffectCertainty, error)
}

func (a *journalAdapter) Call(ctx context.Context, id, tool string, args json.RawMessage) (any, EffectCertainty, error) {
	return a.call(ctx, id, tool, args)
}
func (*journalAdapter) Cancel(string)                  {}
func (*journalAdapter) Shutdown(context.Context) error { return nil }

// Exercise the actual HTTP handler without TCP. This is also useful on managed
// executors which forbid loopback connections; no production check is skipped.
func directBrokerCall(t *testing.T, journal EffectJournal, adapter Adapter, declared EffectCertainty) CallResponse {
	t.Helper()
	session := &attemptSession{attemptID: "attempt", executionID: "run", taskID: "run|task", token: "credential", capabilities: map[string]struct{}{"workspace.read": {}}, calls: map[string]*callRecord{}, slots: make(chan struct{}, 4)}
	b := &Broker{tools: map[string]registeredTool{}, names: map[string]string{}, adapters: map[string]Adapter{}, adapterSlots: map[string]chan struct{}{}, sessions: map[string]*attemptSession{"attempt": session}, tokens: map[string]string{"credential": "attempt"}, global: make(chan struct{}, 32), journal: journal}
	err := b.Register("fixture", adapter, []Descriptor{{ID: "fixture.call", Name: "fixture_call", Description: "fixture", Schema: json.RawMessage(`{"type":"object"}`), RequiredCapability: "workspace.read", Effect: declared}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://localhost/v1/calls", strings.NewReader(`{"callId":"call","tool":"fixture.call","arguments":{"b":2,"a":1}}`))
	r.Header.Set("Authorization", "Bearer credential")
	r.Header.Set("X-Reticle-Attempt-ID", "attempt")
	w := httptest.NewRecorder()
	b.handleCall(w, r)
	var response CallResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestJournalPersistsIntentBeforeEnteringAdapter(t *testing.T) {
	j := &journalFixture{}
	a := &journalAdapter{call: func(_ context.Context, id, tool string, args json.RawMessage) (any, EffectCertainty, error) {
		if len(j.intents) != 1 {
			t.Fatal("adapter entered before durable intent")
		}
		if id != "attempt/call" || tool != "fixture.call" || string(args) != `{"a":1,"b":2}` {
			t.Fatalf("wrong invocation: %s %s %s", id, tool, args)
		}
		return "done", Uncertain, nil
	}}
	response := directBrokerCall(t, j, a, Uncertain)
	if !response.OK || len(j.outcomes) != 1 || j.outcomes[0].Effect != Uncertain {
		t.Fatalf("missing uncertainty receipt: %#v %#v", response, j.outcomes)
	}
	intent := j.intents[0]
	if intent.TaskID != "run|task" || intent.ExecutionID != "run" || len(intent.RequestHash) != 64 {
		t.Fatalf("incomplete intent identity: %#v", intent)
	}
}

func TestJournalAdmissionFailureNeverCallsAdapter(t *testing.T) {
	for _, journal := range []EffectJournal{nil, &journalFixture{beginErr: errors.New("disk unavailable")}} {
		a := &journalAdapter{call: func(context.Context, string, string, json.RawMessage) (any, EffectCertainty, error) {
			t.Fatal("effect ran without durable admission")
			return nil, Uncertain, nil
		}}
		response := directBrokerCall(t, journal, a, Uncertain)
		if response.OK || response.Error == nil || response.Error.Code != "effect_admission_failed" {
			t.Fatalf("unsafe admission: %#v", response)
		}
	}
}

func TestJournalCompletionFailureAndAdapterPanicStayUncertain(t *testing.T) {
	for _, panics := range []bool{false, true} {
		j := &journalFixture{}
		if !panics {
			j.finishErr = errors.New("disk full")
		}
		a := &journalAdapter{call: func(context.Context, string, string, json.RawMessage) (any, EffectCertainty, error) {
			if panics {
				panic("private adapter detail")
			}
			return "done", EffectStarted, nil
		}}
		response := directBrokerCall(t, j, a, Uncertain)
		if response.OK || response.Effect != Uncertain || len(j.outcomes) != 1 {
			t.Fatalf("effect uncertainty lost: %#v", response)
		}
		if strings.Contains(response.Error.Message, "private adapter detail") {
			t.Fatal("panic detail exposed")
		}
	}
}
