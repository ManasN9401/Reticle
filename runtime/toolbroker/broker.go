package toolbroker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/events"
)

type EffectCertainty string

const (
	NoEffect      EffectCertainty = "no_effect"
	EffectStarted EffectCertainty = "effect_started"
	Uncertain     EffectCertainty = "uncertain"
)

const (
	maxRequestBytes = 256 * 1024
	maxSchemaBytes  = 64 * 1024
	maxResultBytes  = 1024 * 1024
	defaultTimeout  = 30 * time.Second
)

var safeIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,119}$`)
var safeModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,119}$`)

type Descriptor struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Schema             json.RawMessage `json:"schema"`
	RequiredCapability string          `json:"required_capability"`
	RequiredPolicy     string          `json:"required_policy,omitempty"`
	Adapter            string          `json:"adapter"`
	TimeoutMS          int64           `json:"timeout_ms"`
	OutputLimit        int             `json:"output_limit"`
	Effect             EffectCertainty `json:"effect"`
	Available          bool            `json:"available"`
}

type Adapter interface {
	Call(context.Context, string, string, json.RawMessage) (any, EffectCertainty, error)
	Cancel(string)
	Shutdown(context.Context) error
}

type CallError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CallResponse struct {
	OK     bool            `json:"ok"`
	Result any             `json:"result,omitempty"`
	Error  *CallError      `json:"error,omitempty"`
	Effect EffectCertainty `json:"effect"`
}

type Credentials struct {
	URL       string
	Token     string
	AttemptID string
}

func (c Credentials) Environment() []string {
	return []string{
		"RETICLE_TOOL_BROKER_URL=" + c.URL,
		"RETICLE_TOOL_BROKER_TOKEN=" + c.Token,
		"RETICLE_ATTEMPT_ID=" + c.AttemptID,
	}
}

type registeredTool struct {
	descriptor Descriptor
	adapter    Adapter
}

type callRecord struct {
	active          bool
	cancelRequested bool
	fingerprint     [sha256.Size]byte
	response        CallResponse
	cancel          context.CancelFunc
	adapter         Adapter
}

type attemptSession struct {
	attemptID    string
	executionID  string
	taskID       string
	token        string
	capabilities map[string]struct{}
	policies     map[string]struct{}
	paused       bool
	revoked      bool
	calls        map[string]*callRecord
	slots        chan struct{}
}

type Broker struct {
	mu           sync.RWMutex
	tools        map[string]registeredTool
	names        map[string]string
	adapters     map[string]Adapter
	adapterSlots map[string]chan struct{}
	sessions     map[string]*attemptSession
	tokens       map[string]string
	listener     net.Listener
	server       *http.Server
	bus          *events.Bus
	global       chan struct{}
	closeOnce    sync.Once
	closeErr     error
}

func New(bus *events.Bus) (*Broker, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start tool broker: %w", err)
	}
	b := &Broker{
		tools: make(map[string]registeredTool), names: make(map[string]string),
		adapters: make(map[string]Adapter), adapterSlots: make(map[string]chan struct{}), sessions: make(map[string]*attemptSession),
		tokens: make(map[string]string), listener: listener, bus: bus, global: make(chan struct{}, 32),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/tools", b.handleTools)
	mux.HandleFunc("/v1/calls", b.handleCall)
	mux.HandleFunc("/v1/calls/", b.handleCancel)
	b.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	go func() { _ = b.server.Serve(listener) }()
	return b, nil
}

func (b *Broker) URL() string { return "http://" + b.listener.Addr().String() }

func (b *Broker) Register(adapterID string, adapter Adapter, descriptors []Descriptor) error {
	return b.ReplaceAdapterTools(adapterID, adapter, descriptors, false)
}

// ReplaceAdapterTools atomically replaces one adapter's visible descriptor
// set. Dynamic adapters (MCP and plugins) use this after a validated refresh;
// static callers use Register, which rejects an existing adapter identity.
func (b *Broker) ReplaceAdapterTools(adapterID string, adapter Adapter, descriptors []Descriptor, replace bool) error {
	if !safeIdentity.MatchString(adapterID) || adapter == nil {
		return errors.New("valid adapter identity and implementation are required")
	}
	validated := make([]Descriptor, len(descriptors))
	seenIDs, seenNames := map[string]bool{}, map[string]bool{}
	for i, descriptor := range descriptors {
		if !safeIdentity.MatchString(descriptor.ID) || !safeModelName.MatchString(descriptor.Name) {
			return fmt.Errorf("invalid tool identity %q or model name %q", descriptor.ID, descriptor.Name)
		}
		if descriptor.RequiredCapability == "" {
			return fmt.Errorf("tool %s requires a capability", descriptor.ID)
		}
		if len(descriptor.Description) > 4096 || len(descriptor.Schema) == 0 || len(descriptor.Schema) > maxSchemaBytes {
			return fmt.Errorf("tool %s has invalid descriptor limits", descriptor.ID)
		}
		var schema map[string]any
		if json.Unmarshal(descriptor.Schema, &schema) != nil || schema["type"] != "object" {
			return fmt.Errorf("tool %s schema must be a JSON object schema", descriptor.ID)
		}
		if seenIDs[descriptor.ID] || seenNames[descriptor.Name] {
			return fmt.Errorf("duplicate tool identity or name in adapter %s", adapterID)
		}
		seenIDs[descriptor.ID], seenNames[descriptor.Name] = true, true
		descriptor.Adapter = adapterID
		if descriptor.TimeoutMS <= 0 {
			descriptor.TimeoutMS = defaultTimeout.Milliseconds()
		}
		if descriptor.OutputLimit <= 0 || descriptor.OutputLimit > maxResultBytes {
			descriptor.OutputLimit = maxResultBytes
		}
		if descriptor.Effect != NoEffect && descriptor.Effect != EffectStarted && descriptor.Effect != Uncertain {
			return fmt.Errorf("tool %s has invalid effect classification", descriptor.ID)
		}
		descriptor.Schema = append(json.RawMessage(nil), descriptor.Schema...)
		descriptor.Available = true
		validated[i] = descriptor
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	existingAdapter, exists := b.adapters[adapterID]
	if exists && !replace {
		return fmt.Errorf("adapter %s already registered", adapterID)
	}
	if exists && !sameAdapter(existingAdapter, adapter) {
		return fmt.Errorf("adapter %s implementation cannot be replaced", adapterID)
	}
	for _, descriptor := range validated {
		if existing, exists := b.tools[descriptor.ID]; exists && existing.descriptor.Adapter != adapterID {
			return fmt.Errorf("tool ID %s already registered", descriptor.ID)
		}
		if existing, exists := b.names[descriptor.Name]; exists && b.tools[existing].descriptor.Adapter != adapterID {
			return fmt.Errorf("tool name %s collides with %s", descriptor.Name, existing)
		}
	}
	if exists {
		for id, tool := range b.tools {
			if tool.descriptor.Adapter == adapterID {
				delete(b.names, tool.descriptor.Name)
				delete(b.tools, id)
			}
		}
	}
	b.adapters[adapterID] = adapter
	if !exists {
		b.adapterSlots[adapterID] = make(chan struct{}, 8)
	}
	for _, descriptor := range validated {
		b.tools[descriptor.ID] = registeredTool{descriptor: descriptor, adapter: adapter}
		b.names[descriptor.Name] = descriptor.ID
	}
	return nil
}

func sameAdapter(left, right Adapter) bool {
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	return leftValue.IsValid() && rightValue.IsValid() && leftValue.Type() == rightValue.Type() &&
		leftValue.Kind() == reflect.Pointer && leftValue.Pointer() == rightValue.Pointer()
}

func (b *Broker) BeginAttempt(attemptID, executionID, taskID string, capabilities []string) (Credentials, error) {
	return b.BeginAttemptWithPolicies(attemptID, executionID, taskID, capabilities, nil)
}

func (b *Broker) BeginAttemptWithPolicies(attemptID, executionID, taskID string, capabilities, policies []string) (Credentials, error) {
	if attemptID == "" || executionID == "" || taskID == "" {
		return Credentials{}, errors.New("attempt, execution and task identities are required")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Credentials{}, fmt.Errorf("create attempt credential: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	grants := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		grants[capability] = struct{}{}
	}
	policyGrants := make(map[string]struct{}, len(policies))
	for _, policy := range policies {
		policyGrants[policy] = struct{}{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.sessions[attemptID]; exists {
		return Credentials{}, fmt.Errorf("attempt %s already active", attemptID)
	}
	b.sessions[attemptID] = &attemptSession{
		attemptID: attemptID, executionID: executionID, taskID: taskID, token: token,
		capabilities: grants, policies: policyGrants, calls: make(map[string]*callRecord), slots: make(chan struct{}, 4),
	}
	b.tokens[token] = attemptID
	return Credentials{URL: b.URL(), Token: token, AttemptID: attemptID}, nil
}

func (b *Broker) SetExecutionPaused(executionID string, paused bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, session := range b.sessions {
		if session.executionID == executionID {
			session.paused = paused
		}
	}
}

func (b *Broker) RevokeAttempt(attemptID string) {
	b.mu.Lock()
	session := b.sessions[attemptID]
	if session == nil {
		b.mu.Unlock()
		return
	}
	session.revoked = true
	delete(b.tokens, session.token)
	delete(b.sessions, attemptID)
	active := make(map[string]*callRecord)
	for id, call := range session.calls {
		if call.active {
			call.cancel()
			if call.cancelRequested {
				continue
			}
			call.cancelRequested = true
			active[id] = call
		}
	}
	b.mu.Unlock()
	for id, call := range active {
		call.adapter.Cancel(attemptID + "/" + id)
	}
}

func (b *Broker) RevokeExecution(executionID string) {
	b.mu.RLock()
	ids := make([]string, 0)
	for id, session := range b.sessions {
		if session.executionID == executionID || session.executionID == "compile-"+executionID {
			ids = append(ids, id)
		}
	}
	b.mu.RUnlock()
	for _, id := range ids {
		b.RevokeAttempt(id)
	}
}

func (b *Broker) Close(ctx context.Context) error {
	b.closeOnce.Do(func() {
		b.mu.RLock()
		ids := make([]string, 0, len(b.sessions))
		adapters := make([]Adapter, 0, len(b.adapters))
		for id := range b.sessions {
			ids = append(ids, id)
		}
		for _, adapter := range b.adapters {
			adapters = append(adapters, adapter)
		}
		b.mu.RUnlock()
		for _, id := range ids {
			b.RevokeAttempt(id)
		}
		for _, adapter := range adapters {
			_ = adapter.Shutdown(ctx)
		}
		b.closeErr = b.server.Shutdown(ctx)
	})
	return b.closeErr
}

func (b *Broker) authenticate(r *http.Request) (*attemptSession, int, string) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || len(token) > 256 {
		return nil, http.StatusUnauthorized, "invalid_credential"
	}
	b.mu.RLock()
	attemptID := b.tokens[token]
	session := b.sessions[attemptID]
	valid := session != nil && !session.revoked && subtle.ConstantTimeCompare([]byte(token), []byte(session.token)) == 1
	b.mu.RUnlock()
	if !valid {
		return nil, http.StatusUnauthorized, "invalid_credential"
	}
	if r.Header.Get("X-Reticle-Attempt-ID") != session.attemptID {
		return nil, http.StatusForbidden, "attempt_mismatch"
	}
	return session, 0, ""
}

func (b *Broker) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	session, status, code := b.authenticate(r)
	if session == nil {
		writeError(w, status, code)
		return
	}
	b.mu.RLock()
	tools := make([]Descriptor, 0)
	for _, tool := range b.tools {
		_, capabilityAllowed := session.capabilities[tool.descriptor.RequiredCapability]
		_, policyAllowed := session.policies[tool.descriptor.RequiredPolicy]
		if tool.descriptor.RequiredPolicy == "" {
			policyAllowed = true
		}
		if capabilityAllowed && policyAllowed && tool.descriptor.Available {
			copyDescriptor := tool.descriptor
			copyDescriptor.Schema = append(json.RawMessage(nil), tool.descriptor.Schema...)
			tools = append(tools, copyDescriptor)
		}
	}
	b.mu.RUnlock()
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

type callRequest struct {
	CallID    string          `json:"callId"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

func (b *Broker) handleCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/calls" {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	session, status, code := b.authenticate(r)
	if session == nil {
		writeError(w, status, code)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	var request callRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || !safeIdentity.MatchString(request.CallID) || len(request.Arguments) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var args map[string]any
	if json.Unmarshal(request.Arguments, &args) != nil {
		writeError(w, http.StatusBadRequest, "invalid_arguments")
		return
	}
	fingerprintInput := make([]byte, 0, len(request.Tool)+1+len(request.Arguments))
	fingerprintInput = append(fingerprintInput, request.Tool...)
	fingerprintInput = append(fingerprintInput, 0)
	fingerprintInput = append(fingerprintInput, request.Arguments...)
	fingerprint := sha256.Sum256(fingerprintInput)

	b.mu.Lock()
	if session.revoked {
		b.mu.Unlock()
		writeError(w, http.StatusUnauthorized, "revoked_attempt")
		return
	}
	if session.paused {
		b.mu.Unlock()
		writeError(w, http.StatusConflict, "attempt_paused")
		return
	}
	if previous := session.calls[request.CallID]; previous != nil {
		if previous.fingerprint != fingerprint {
			b.mu.Unlock()
			writeError(w, http.StatusConflict, "call_id_conflict")
			return
		}
		if previous.active {
			b.mu.Unlock()
			writeError(w, http.StatusConflict, "call_active")
			return
		}
		response := previous.response
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, response)
		return
	}
	registered, exists := b.tools[request.Tool]
	if !exists {
		if id := b.names[request.Tool]; id != "" {
			registered, exists = b.tools[id]
		}
	}
	if !exists || !registered.descriptor.Available {
		b.mu.Unlock()
		writeError(w, http.StatusNotFound, "tool_unavailable")
		return
	}
	if _, allowed := session.capabilities[registered.descriptor.RequiredCapability]; !allowed {
		b.mu.Unlock()
		writeError(w, http.StatusForbidden, "capability_denied")
		return
	}
	if registered.descriptor.RequiredPolicy != "" {
		if _, allowed := session.policies[registered.descriptor.RequiredPolicy]; !allowed {
			b.mu.Unlock()
			writeError(w, http.StatusForbidden, "policy_denied")
			return
		}
	}
	if err := validateArguments(registered.descriptor.Schema, args); err != nil {
		b.mu.Unlock()
		writeError(w, http.StatusBadRequest, "schema_validation_failed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(registered.descriptor.TimeoutMS)*time.Millisecond)
	record := &callRecord{active: true, fingerprint: fingerprint, cancel: cancel, adapter: registered.adapter}
	session.calls[request.CallID] = record
	b.mu.Unlock()

	select {
	case b.global <- struct{}{}:
	case <-ctx.Done():
		response := CallResponse{OK: false, Error: &CallError{Code: "deadline_exceeded", Message: "tool call deadline exceeded"}, Effect: NoEffect}
		b.finishCall(session, request.CallID, response)
		writeJSON(w, http.StatusGatewayTimeout, response)
		return
	}
	defer func() { <-b.global }()
	select {
	case session.slots <- struct{}{}:
	case <-ctx.Done():
		response := CallResponse{OK: false, Error: &CallError{Code: "deadline_exceeded", Message: "tool call deadline exceeded"}, Effect: NoEffect}
		b.finishCall(session, request.CallID, response)
		writeJSON(w, http.StatusGatewayTimeout, response)
		return
	}
	defer func() { <-session.slots }()
	b.mu.RLock()
	adapterSlots := b.adapterSlots[registered.descriptor.Adapter]
	b.mu.RUnlock()
	select {
	case adapterSlots <- struct{}{}:
	case <-ctx.Done():
		response := CallResponse{OK: false, Error: &CallError{Code: "deadline_exceeded", Message: "tool call deadline exceeded"}, Effect: NoEffect}
		b.finishCall(session, request.CallID, response)
		writeJSON(w, http.StatusGatewayTimeout, response)
		return
	}
	defer func() { <-adapterSlots }()

	started := time.Now()
	b.publish("BrokerToolCallStarted", session, request.CallID, registered.descriptor, "", "", 0)
	adapterCallID := session.attemptID + "/" + request.CallID
	result, effect, err := registered.adapter.Call(ctx, adapterCallID, registered.descriptor.ID, request.Arguments)
	if effect == "" {
		effect = Uncertain
	}
	response := CallResponse{OK: err == nil, Result: result, Effect: effect}
	if err != nil {
		response.Result = nil
		response.Error = &CallError{Code: "adapter_error", Message: boundedMessage(err.Error())}
	}
	encoded, marshalErr := json.Marshal(response)
	if marshalErr != nil || len(encoded) > registered.descriptor.OutputLimit {
		response = CallResponse{OK: false, Error: &CallError{Code: "result_too_large", Message: "tool result exceeds its output limit"}, Effect: effect}
	}
	b.mu.RLock()
	stillActive := b.sessions[session.attemptID] == session && !session.revoked
	b.mu.RUnlock()
	if !stillActive {
		b.publish("BrokerToolCallFinished", session, request.CallID, registered.descriptor, "discarded", effect, time.Since(started))
		writeError(w, http.StatusUnauthorized, "revoked_attempt")
		return
	}
	b.finishCall(session, request.CallID, response)
	outcome := "succeeded"
	if !response.OK {
		outcome = "failed"
	}
	b.publish("BrokerToolCallFinished", session, request.CallID, registered.descriptor, outcome, response.Effect, time.Since(started))
	writeJSON(w, http.StatusOK, response)
}

// Descriptors returns an immutable, deterministic registry snapshot for
// architect discovery and control-plane diagnostics.
func (b *Broker) Descriptors() []Descriptor {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Descriptor, 0, len(b.tools))
	for _, tool := range b.tools {
		descriptor := tool.descriptor
		descriptor.Schema = append(json.RawMessage(nil), descriptor.Schema...)
		result = append(result, descriptor)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (b *Broker) finishCall(session *attemptSession, callID string, response CallResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if record := session.calls[callID]; record != nil {
		record.active = false
		record.response = response
		if record.cancel != nil {
			record.cancel()
		}
	}
}

func (b *Broker) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	session, status, code := b.authenticate(r)
	if session == nil {
		writeError(w, status, code)
		return
	}
	callPath := strings.TrimPrefix(r.URL.Path, "/v1/calls/")
	if !strings.HasSuffix(callPath, "/cancel") {
		writeError(w, http.StatusNotFound, "endpoint_not_found")
		return
	}
	callID := strings.TrimSuffix(callPath, "/cancel")
	if strings.Contains(callID, "/") || !safeIdentity.MatchString(callID) {
		writeError(w, http.StatusBadRequest, "invalid_call_id")
		return
	}
	b.mu.Lock()
	record := session.calls[callID]
	if record == nil || !record.active {
		b.mu.Unlock()
		writeError(w, http.StatusNotFound, "call_not_active")
		return
	}
	record.cancel()
	record.cancelRequested = true
	adapter := record.adapter
	b.mu.Unlock()
	adapter.Cancel(session.attemptID + "/" + callID)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (b *Broker) publish(event string, session *attemptSession, callID string, descriptor Descriptor, outcome string, effect EffectCertainty, duration time.Duration) {
	if b.bus == nil {
		return
	}
	payload := map[string]any{
		"execution": session.executionID, "task_id": session.taskID, "attempt_id": session.attemptID,
		"call_id": callID, "tool": descriptor.ID, "adapter": descriptor.Adapter,
	}
	if outcome != "" {
		payload["outcome"] = outcome
		payload["effect"] = effect
	}
	if duration > 0 {
		payload["duration_ms"] = duration.Milliseconds()
	}
	b.bus.Publish(events.EventType(event), events.Component("tool_broker"), payload)
}

func boundedMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}

func validateArguments(raw json.RawMessage, arguments map[string]any) error {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			name, ok := item.(string)
			if ok {
				if _, exists := arguments[name]; !exists {
					return fmt.Errorf("required argument %s is missing", name)
				}
			}
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	if additional, exists := schema["additionalProperties"].(bool); exists && !additional {
		for name := range arguments {
			if _, exists := properties[name]; !exists {
				return fmt.Errorf("argument %s is not allowed", name)
			}
		}
	}
	for name, value := range arguments {
		property, exists := properties[name].(map[string]any)
		if !exists {
			continue
		}
		types := make([]string, 0, 1)
		switch declared := property["type"].(type) {
		case string:
			types = append(types, declared)
		case []any:
			for _, item := range declared {
				if typeName, ok := item.(string); ok {
					types = append(types, typeName)
				}
			}
		}
		if len(types) == 0 {
			continue
		}
		valid := false
		for _, declared := range types {
			if matchesJSONType(value, declared) {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("argument %s has the wrong type", name)
		}
	}
	return nil
}

func matchesJSONType(value any, declared string) bool {
	switch declared {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "null":
		return value == nil
	default:
		return true
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, CallResponse{OK: false, Error: &CallError{Code: code, Message: strings.ReplaceAll(code, "_", " ")}, Effect: NoEffect})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxResultBytes+4096 {
		status = http.StatusInternalServerError
		data = []byte(`{"ok":false,"error":{"code":"response_encoding_failed","message":"response encoding failed"},"effect":"uncertain"}`)
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
