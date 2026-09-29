package toolbroker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/toolbroker"
)

var objectSchema = json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)

type fixtureAdapter struct {
	mu            sync.Mutex
	calls         int
	active        int
	maxActive     int
	cancelled     []string
	delay         time.Duration
	large         bool
	ignoreContext bool
	effect        toolbroker.EffectCertainty
}

func (a *fixtureAdapter) Call(ctx context.Context, callID, toolID string, arguments json.RawMessage) (any, toolbroker.EffectCertainty, error) {
	a.mu.Lock()
	a.calls++
	a.active++
	if a.active > a.maxActive {
		a.maxActive = a.active
	}
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.active--; a.mu.Unlock() }()
	if a.delay > 0 {
		if a.ignoreContext {
			time.Sleep(a.delay)
		} else {
			select {
			case <-time.After(a.delay):
			case <-ctx.Done():
				return nil, toolbroker.NoEffect, ctx.Err()
			}
		}
	}
	if a.large {
		return strings.Repeat("x", 4096), toolbroker.NoEffect, nil
	}
	var input map[string]string
	if err := json.Unmarshal(arguments, &input); err != nil {
		return nil, toolbroker.NoEffect, err
	}
	effect := a.effect
	if effect == "" {
		effect = toolbroker.NoEffect
	}
	return map[string]string{"echo": input["value"]}, effect, nil
}
func (a *fixtureAdapter) Cancel(callID string) {
	a.mu.Lock()
	a.cancelled = append(a.cancelled, callID)
	a.mu.Unlock()
}
func (a *fixtureAdapter) Shutdown(context.Context) error { return nil }

func descriptor() toolbroker.Descriptor {
	return toolbroker.Descriptor{ID: "fixture.echo", Name: "fixture__echo", Description: "Echo a bounded fixture value", Schema: objectSchema, RequiredCapability: "workspace.read", Effect: toolbroker.NoEffect, TimeoutMS: 1000, OutputLimit: 8192}
}

func newBroker(t *testing.T, adapter *fixtureAdapter, d toolbroker.Descriptor) (*toolbroker.Broker, *events.Bus) {
	t.Helper()
	bus := events.NewBus("broker-test")
	broker, err := toolbroker.New(bus)
	if err != nil {
		t.Fatal(err)
	}
	if err = broker.Register("fixture", adapter, []toolbroker.Descriptor{d}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = broker.Close(ctx)
		bus.Close()
	})
	return broker, bus
}

func request(t *testing.T, credentials toolbroker.Credentials, method, path string, body any) (int, toolbroker.CallResponse, []byte) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, credentials.URL+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+credentials.Token)
	req.Header.Set("X-Reticle-Attempt-ID", credentials.AttemptID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded toolbroker.CallResponse
	_ = json.Unmarshal(raw, &decoded)
	return resp.StatusCode, decoded, raw
}

func TestRegistryRejectsMalformedAndCollidingDescriptors(t *testing.T) {
	bus := events.NewBus("registry-test")
	broker, err := toolbroker.New(bus)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = broker.Close(context.Background()); bus.Close() }()
	adapter := &fixtureAdapter{}
	bad := descriptor()
	bad.Schema = json.RawMessage(`{"type":"string"}`)
	if err := broker.Register("bad", adapter, []toolbroker.Descriptor{bad}); err == nil {
		t.Fatal("malformed schema accepted")
	}
	registered := descriptor()
	registered.Schema = append(json.RawMessage(nil), registered.Schema...)
	if err := broker.Register("fixture", adapter, []toolbroker.Descriptor{registered}); err != nil {
		t.Fatal(err)
	}
	registered.Schema[0] = '['
	credentials, _ := broker.BeginAttempt("immutable-attempt", "immutable-exec", "immutable-task", []string{"workspace.read"})
	status, _, raw := request(t, credentials, http.MethodGet, "/v1/tools", nil)
	if status != http.StatusOK || !bytes.Contains(raw, []byte(`"schema":{"type":"object"`)) {
		t.Fatalf("registered descriptor was mutable through caller-owned schema: %d %s", status, raw)
	}
	other := descriptor()
	other.ID = "other.echo"
	if err := broker.Register("other", &fixtureAdapter{}, []toolbroker.Descriptor{other}); err == nil {
		t.Fatal("model-name collision accepted")
	}
}

func TestAttemptIsolationCapabilitiesDuplicateAndRevocation(t *testing.T) {
	adapter := &fixtureAdapter{}
	broker, _ := newBroker(t, adapter, descriptor())
	allowed, err := broker.BeginAttempt("attempt-a", "exec-a", "task-a", []string{"workspace.read"})
	if err != nil {
		t.Fatal(err)
	}
	denied, err := broker.BeginAttempt("attempt-b", "exec-b", "task-b", nil)
	if err != nil {
		t.Fatal(err)
	}

	status, _, raw := request(t, allowed, http.MethodGet, "/v1/tools", nil)
	if status != http.StatusOK || !bytes.Contains(raw, []byte("fixture__echo")) {
		t.Fatalf("allowed catalog: %d %s", status, raw)
	}
	status, _, raw = request(t, denied, http.MethodGet, "/v1/tools", nil)
	if status != http.StatusOK || bytes.Contains(raw, []byte("fixture__echo")) {
		t.Fatalf("denied catalog leaked tool: %d %s", status, raw)
	}
	status, _, _ = request(t, allowed, http.MethodPost, "/v1/calls", map[string]any{"callId": "invalid-schema", "tool": "fixture.echo", "arguments": map[string]any{"unexpected": true}})
	if status != http.StatusBadRequest || adapter.calls != 0 {
		t.Fatalf("invalid arguments reached adapter: status=%d calls=%d", status, adapter.calls)
	}

	call := map[string]any{"callId": "same-call", "tool": "fixture.echo", "arguments": map[string]any{"value": "hello"}}
	status, first, _ := request(t, allowed, http.MethodPost, "/v1/calls", call)
	if status != http.StatusOK || !first.OK {
		t.Fatalf("first call failed: %d %#v", status, first)
	}
	status, second, _ := request(t, allowed, http.MethodPost, "/v1/calls", call)
	if status != http.StatusOK || !second.OK || adapter.calls != 1 {
		t.Fatalf("duplicate was re-executed: %d %#v calls=%d", status, second, adapter.calls)
	}
	call["arguments"] = map[string]any{"value": "different"}
	status, _, _ = request(t, allowed, http.MethodPost, "/v1/calls", call)
	if status != http.StatusConflict || adapter.calls != 1 {
		t.Fatalf("reused call ID accepted different arguments: status=%d calls=%d", status, adapter.calls)
	}
	status, _, _ = request(t, denied, http.MethodPost, "/v1/calls", call)
	if status != http.StatusForbidden {
		t.Fatalf("capability denial returned %d", status)
	}

	wrong := allowed
	wrong.AttemptID = denied.AttemptID
	status, _, _ = request(t, wrong, http.MethodGet, "/v1/tools", nil)
	if status != http.StatusForbidden {
		t.Fatalf("attempt mismatch returned %d", status)
	}
	broker.RevokeAttempt(allowed.AttemptID)
	status, _, _ = request(t, allowed, http.MethodGet, "/v1/tools", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked token returned %d", status)
	}
}

func TestBrokerTelemetryExcludesArgumentsResultsAndCredentials(t *testing.T) {
	adapter := &fixtureAdapter{effect: toolbroker.EffectStarted}
	broker, bus := newBroker(t, adapter, descriptor())
	eventsSeen := make(chan events.RuntimeEvent, 2)
	bus.SubscribeAll(func(event events.RuntimeEvent) {
		if event.Type == "BrokerToolCallStarted" || event.Type == "BrokerToolCallFinished" {
			eventsSeen <- event
		}
	})
	credentials, _ := broker.BeginAttempt("telemetry-attempt", "telemetry-exec", "telemetry-task", []string{"workspace.read"})
	status, response, _ := request(t, credentials, http.MethodPost, "/v1/calls", map[string]any{"callId": "telemetry-call", "tool": "fixture.echo", "arguments": map[string]any{"value": "private-argument"}})
	if status != http.StatusOK || !response.OK {
		t.Fatalf("tool call failed: %d %#v", status, response)
	}
	for i := 0; i < 2; i++ {
		select {
		case event := <-eventsSeen:
			encoded, _ := json.Marshal(event.Payload)
			text := string(encoded)
			if strings.Contains(text, "private-argument") || strings.Contains(text, credentials.Token) {
				t.Fatalf("sensitive tool data entered telemetry: %s", text)
			}
			if event.Type == "BrokerToolCallFinished" && !strings.Contains(text, `"effect":"effect_started"`) {
				t.Fatalf("finished telemetry omitted actual effect certainty: %s", text)
			}
		case <-time.After(time.Second):
			t.Fatal("missing broker telemetry")
		}
	}
}

func TestExplicitCancellationAndLateResultDiscard(t *testing.T) {
	adapter := &fixtureAdapter{delay: 120 * time.Millisecond, ignoreContext: true, effect: toolbroker.EffectStarted}
	d := descriptor()
	d.TimeoutMS = 1000
	broker, _ := newBroker(t, adapter, d)
	credentials, _ := broker.BeginAttempt("cancel-attempt", "cancel-exec", "cancel-task", []string{"workspace.read"})
	done := make(chan int, 1)
	go func() {
		status, _, _ := request(t, credentials, http.MethodPost, "/v1/calls", map[string]any{"callId": "cancel-call", "tool": "fixture.echo", "arguments": map[string]any{"value": "hello"}})
		done <- status
	}()
	time.Sleep(25 * time.Millisecond)
	status, _, _ := request(t, credentials, http.MethodPost, "/v1/calls/cancel-call/cancel", nil)
	if status != http.StatusAccepted {
		t.Fatalf("cancel endpoint returned %d", status)
	}
	broker.RevokeAttempt(credentials.AttemptID)
	select {
	case resultStatus := <-done:
		if resultStatus != http.StatusUnauthorized {
			t.Fatalf("late result was not discarded: status=%d", resultStatus)
		}
	case <-time.After(time.Second):
		t.Fatal("late adapter result did not return")
	}
	adapter.mu.Lock()
	cancelled := append([]string(nil), adapter.cancelled...)
	adapter.mu.Unlock()
	if len(cancelled) == 0 || cancelled[0] != "cancel-attempt/cancel-call" {
		t.Fatalf("adapter did not receive the canonical cancellation ID: %#v", cancelled)
	}
}

func TestPauseDeadlineOutputLimitAndCancellation(t *testing.T) {
	adapter := &fixtureAdapter{delay: 150 * time.Millisecond}
	d := descriptor()
	d.TimeoutMS = 30
	broker, _ := newBroker(t, adapter, d)
	credentials, _ := broker.BeginAttempt("attempt", "exec", "task", []string{"workspace.read"})
	broker.SetExecutionPaused("exec", true)
	call := map[string]any{"callId": "paused", "tool": "fixture.echo", "arguments": map[string]any{"value": "hello"}}
	status, _, _ := request(t, credentials, http.MethodPost, "/v1/calls", call)
	if status != http.StatusConflict {
		t.Fatalf("paused call returned %d", status)
	}
	broker.SetExecutionPaused("exec", false)
	call["callId"] = "timeout"
	status, response, _ := request(t, credentials, http.MethodPost, "/v1/calls", call)
	if status != http.StatusOK || response.OK || response.Error == nil || response.Effect != toolbroker.NoEffect {
		t.Fatalf("deadline response: %d %#v", status, response)
	}

	large := &fixtureAdapter{large: true}
	largeDescriptor := descriptor()
	largeDescriptor.ID = "large.echo"
	largeDescriptor.Name = "large__echo"
	largeDescriptor.OutputLimit = 128
	if err := broker.Register("large", large, []toolbroker.Descriptor{largeDescriptor}); err != nil {
		t.Fatal(err)
	}
	call = map[string]any{"callId": "large", "tool": "large.echo", "arguments": map[string]any{"value": "hello"}}
	status, response, _ = request(t, credentials, http.MethodPost, "/v1/calls", call)
	if status != http.StatusOK || response.OK || response.Error.Code != "result_too_large" {
		t.Fatalf("large response: %d %#v", status, response)
	}

	slow := &fixtureAdapter{delay: time.Second}
	slowDescriptor := descriptor()
	slowDescriptor.ID = "slow.echo"
	slowDescriptor.Name = "slow__echo"
	slowDescriptor.TimeoutMS = 2000
	if err := broker.Register("slow", slow, []toolbroker.Descriptor{slowDescriptor}); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		s, _, _ := request(t, credentials, http.MethodPost, "/v1/calls", map[string]any{"callId": "cancel", "tool": "slow.echo", "arguments": map[string]any{"value": "hello"}})
		done <- s
	}()
	time.Sleep(30 * time.Millisecond)
	broker.RevokeAttempt(credentials.AttemptID)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel active call")
	}
	if len(slow.cancelled) != 1 || !strings.Contains(slow.cancelled[0], "attempt/cancel") {
		t.Fatalf("adapter cancellation missing: %#v", slow.cancelled)
	}
}

func TestAdapterAndAttemptConcurrencyAreBounded(t *testing.T) {
	adapter := &fixtureAdapter{delay: 80 * time.Millisecond}
	d := descriptor()
	d.TimeoutMS = 1000
	broker, _ := newBroker(t, adapter, d)
	credentials := make([]toolbroker.Credentials, 3)
	for i := range credentials {
		credentials[i], _ = broker.BeginAttempt(fmt.Sprintf("attempt-%d", i), fmt.Sprintf("exec-%d", i), fmt.Sprintf("task-%d", i), []string{"workspace.read"})
	}
	var wait sync.WaitGroup
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			status, response, _ := request(t, credentials[index%len(credentials)], http.MethodPost, "/v1/calls", map[string]any{"callId": fmt.Sprintf("call-%d", index), "tool": "fixture.echo", "arguments": map[string]any{"value": "hello"}})
			if status != http.StatusOK || !response.OK {
				t.Errorf("concurrent call %d failed: %d %#v", index, status, response)
			}
		}(i)
	}
	wait.Wait()
	adapter.mu.Lock()
	calls, maxActive := adapter.calls, adapter.maxActive
	adapter.mu.Unlock()
	if calls != 12 {
		t.Fatalf("expected 12 calls, got %d", calls)
	}
	if maxActive > 8 {
		t.Fatalf("adapter concurrency exceeded bound: %d", maxActive)
	}
}

func TestBrokerWorkerChild(t *testing.T) {
	if os.Getenv("RETICLE_BROKER_CHILD") != "1" {
		return
	}
	var task agent.Task
	if err := json.NewDecoder(os.Stdin).Decode(&task); err != nil {
		os.Exit(2)
	}
	credentials := toolbroker.Credentials{URL: os.Getenv("RETICLE_TOOL_BROKER_URL"), Token: os.Getenv("RETICLE_TOOL_BROKER_TOKEN"), AttemptID: os.Getenv("RETICLE_ATTEMPT_ID")}
	status, response, _ := request(t, credentials, http.MethodPost, "/v1/calls", map[string]any{"callId": "worker-call", "tool": "fixture.echo", "arguments": map[string]any{"value": "from-worker"}})
	if status != http.StatusOK || !response.OK {
		os.Exit(3)
	}
	encoded, _ := json.Marshal(response.Result)
	fmt.Printf("{\"id\":%q,\"verification\":[{\"tool\":\"fixture__echo\",\"target\":%q,\"outcome\":\"succeeded\"}]}\n", task.ID, string(encoded))
	os.Exit(0)
}

func TestRealWorkerUsesBrokerWithoutChangingStdoutProtocol(t *testing.T) {
	adapter := &fixtureAdapter{}
	broker, bus := newBroker(t, adapter, descriptor())
	credentials, err := broker.BeginAttempt("worker-attempt", "worker-exec", "worker-task", []string{"workspace.read"})
	if err != nil {
		t.Fatal(err)
	}
	worker := agent.NewWorker("fixture", os.Args[0], []string{"-test.run=^TestBrokerWorkerChild$"}, []string{"RETICLE_BROKER_CHILD=1"}, &logger.Logger{}, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, failure := worker.ExecuteWithEnvironment(ctx, agent.Task{ID: "worker-task", AttemptID: "worker-attempt", ExecutionID: "worker-exec", Capabilities: []agent.Capability{agent.CapabilityWorkspaceRead}}, credentials.Environment())
	if failure != nil {
		t.Fatalf("worker failed: %#v", failure)
	}
	if response == nil || response.ID != "worker-task" || len(response.Verification) != 1 || adapter.calls != 1 {
		t.Fatalf("unexpected worker response %#v calls=%d", response, adapter.calls)
	}
}
