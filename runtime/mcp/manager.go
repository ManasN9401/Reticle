package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/toolbroker"
)

const brokerAdapterID = "mcp"

type toolBinding struct {
	serverID string
	tool     Tool
}

type serverRuntime struct {
	mu              sync.Mutex
	client          *client
	configSignature [sha256.Size]byte
	state           string
	lastError       string
	missing         []string
	protocol        string
	serverName      string
	serverVersion   string
	tools           []Tool
	updatedAt       time.Time
	callMu          sync.Mutex
}

type Manager struct {
	mu       sync.RWMutex
	registry *Registry
	broker   *toolbroker.Broker
	bus      *events.Bus
	log      *logger.Logger
	servers  map[string]*serverRuntime
	bindings map[string]toolBinding
	active   map[string]context.CancelFunc
	changed  func()
	closed   bool
}

func (m *Manager) SetChangedHandler(handler func()) {
	m.mu.Lock()
	m.changed = handler
	m.mu.Unlock()
}

func (m *Manager) notifyChanged() {
	m.mu.RLock()
	handler := m.changed
	m.mu.RUnlock()
	if handler != nil {
		handler()
	}
}

func NewManager(registry *Registry, broker *toolbroker.Broker, bus *events.Bus, log *logger.Logger) (*Manager, error) {
	if registry == nil || broker == nil {
		return nil, fmt.Errorf("MCP registry and tool broker are required")
	}
	manager := &Manager{
		registry: registry, broker: broker, bus: bus, log: log,
		servers: make(map[string]*serverRuntime), bindings: make(map[string]toolBinding), active: make(map[string]context.CancelFunc),
	}
	if err := broker.Register(brokerAdapterID, manager, nil); err != nil {
		return nil, err
	}
	manager.Sync()
	return manager, nil
}

func (m *Manager) Sync() {
	configs := m.registry.List()
	wanted := make(map[string]ServerConfig, len(configs))
	for _, config := range configs {
		wanted[config.ID] = config
	}
	m.mu.Lock()
	for id, runtime := range m.servers {
		config, exists := wanted[id]
		if !exists || !config.Enabled || runtime.configSignature != signature(config) {
			runtime.mu.Lock()
			client := runtime.client
			runtime.client = nil
			runtime.state = "stopped"
			runtime.tools = nil
			runtime.updatedAt = time.Now().UTC()
			runtime.mu.Unlock()
			if client != nil {
				go client.close()
			}
		}
		if !exists {
			delete(m.servers, id)
		}
	}
	for id, config := range wanted {
		if m.servers[id] == nil {
			state := "disabled"
			if config.Enabled {
				state = "stopped"
			}
			m.servers[id] = &serverRuntime{state: state, configSignature: signature(config), updatedAt: time.Now().UTC()}
		} else {
			m.servers[id].mu.Lock()
			m.servers[id].configSignature = signature(config)
			if !config.Enabled {
				m.servers[id].state = "disabled"
			}
			m.servers[id].mu.Unlock()
		}
	}
	m.mu.Unlock()
	_ = m.refreshBroker()
}

func (m *Manager) ensure(ctx context.Context, serverID string, discover bool) (*client, error) {
	config, exists, _ := m.registry.Get(serverID)
	if !exists {
		return nil, fmt.Errorf("MCP server %s is not registered", serverID)
	}
	if !config.Enabled {
		return nil, fmt.Errorf("MCP server %s is disabled", serverID)
	}
	m.mu.Lock()
	runtime := m.servers[serverID]
	if runtime == nil {
		runtime = &serverRuntime{state: "stopped", configSignature: signature(config), updatedAt: time.Now().UTC()}
		m.servers[serverID] = runtime
	}
	m.mu.Unlock()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.client != nil {
		select {
		case <-runtime.client.done:
			runtime.client = nil
		default:
		}
	}
	if runtime.client == nil {
		environment, missing := childEnvironment(config.Env)
		runtime.missing = missing
		if len(missing) > 0 {
			err := fmt.Errorf("missing host environment variables: %s", strings.Join(missing, ", "))
			m.setFailureLocked(serverID, runtime, err)
			return nil, err
		}
		runtime.state = "starting"
		runtime.updatedAt = time.Now().UTC()
		secretValues := mappedValues(config.Env)
		client, err := startClient(ctx, config, environment, func(message string) {
			message = redactValues(logger.Redact(message), secretValues)
			if m.log != nil {
				m.log.Info("MCP server diagnostic", "server", serverID, "message", message)
			}
		}, func() {
			refreshCtx, cancel := context.WithTimeout(context.Background(), time.Duration(config.StartupTimeoutSeconds)*time.Second)
			defer cancel()
			_ = m.Refresh(refreshCtx, serverID)
		})
		if err != nil {
			m.setFailureLocked(serverID, runtime, err)
			return nil, err
		}
		runtime.client = client
		runtime.state = "connected"
		runtime.lastError = ""
		runtime.protocol = client.protocol
		runtime.serverName = client.serverName
		runtime.serverVersion = client.serverVersion
		runtime.updatedAt = time.Now().UTC()
		go m.watchClient(serverID, runtime, client)
		m.publish("McpServerConnected", map[string]any{"server": serverID, "protocol_version": client.protocol})
	}
	client := runtime.client
	if discover {
		tools, err := client.listTools(ctx)
		if err != nil {
			m.setFailureLocked(serverID, runtime, err)
			return nil, err
		}
		if err := validateTools(tools); err != nil {
			m.setFailureLocked(serverID, runtime, err)
			return nil, err
		}
		runtime.tools = cloneTools(tools)
		runtime.state = "connected"
		runtime.lastError = ""
		runtime.updatedAt = time.Now().UTC()
	}
	return client, nil
}

func (m *Manager) watchClient(serverID string, runtime *serverRuntime, watched *client) {
	<-watched.done
	runtime.mu.Lock()
	if runtime.client != watched {
		runtime.mu.Unlock()
		return
	}
	runtime.client = nil
	runtime.state = "error"
	runtime.lastError = "MCP server process exited unexpectedly"
	runtime.updatedAt = time.Now().UTC()
	runtime.mu.Unlock()
	_ = m.refreshBroker()
	m.publish("McpServerDisconnected", map[string]any{"server": serverID, "reason": "process_exited"})
	m.publish("McpServerError", map[string]any{"server": serverID, "error": "server process exited unexpectedly"})
}

func (m *Manager) setFailureLocked(serverID string, runtime *serverRuntime, err error) {
	runtime.state = "error"
	runtime.lastError = bounded(err.Error(), 1024)
	runtime.updatedAt = time.Now().UTC()
	m.publish("McpServerError", map[string]any{"server": serverID, "error": runtime.lastError})
}

func validateTools(tools []Tool) error {
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" || len(tool.Name) > 256 || seen[tool.Name] {
			return fmt.Errorf("MCP server returned an invalid or duplicate tool name")
		}
		seen[tool.Name] = true
		if len(tool.Description) > 4096 || len(tool.InputSchema) == 0 || len(tool.InputSchema) > 64*1024 {
			return fmt.Errorf("MCP tool %s exceeds descriptor limits", tool.Name)
		}
		var schema map[string]any
		if json.Unmarshal(tool.InputSchema, &schema) != nil || schema["type"] != "object" {
			return fmt.Errorf("MCP tool %s has a non-object input schema", tool.Name)
		}
	}
	return nil
}

func (m *Manager) Refresh(ctx context.Context, serverID string) error {
	_, err := m.ensure(ctx, serverID, true)
	if err != nil {
		_ = m.refreshBroker()
		return err
	}
	if err := m.refreshBroker(); err != nil {
		m.mu.RLock()
		runtime := m.servers[serverID]
		m.mu.RUnlock()
		if runtime != nil {
			runtime.mu.Lock()
			m.setFailureLocked(serverID, runtime, err)
			runtime.mu.Unlock()
		}
		return err
	}
	m.publish("McpToolsChanged", map[string]any{"server": serverID})
	return nil
}

func (m *Manager) RefreshEnabled(ctx context.Context) {
	for _, config := range m.registry.List() {
		if !config.Enabled {
			continue
		}
		serverCtx, cancel := context.WithTimeout(ctx, time.Duration(config.StartupTimeoutSeconds)*time.Second)
		_ = m.Refresh(serverCtx, config.ID)
		cancel()
	}
}

func (m *Manager) refreshBroker() error {
	descriptors := make([]toolbroker.Descriptor, 0)
	bindings := make(map[string]toolBinding)
	m.mu.RLock()
	serverIDs := make([]string, 0, len(m.servers))
	for id := range m.servers {
		serverIDs = append(serverIDs, id)
	}
	m.mu.RUnlock()
	sort.Strings(serverIDs)
	modelNames := make(map[string]string)
	for _, serverID := range serverIDs {
		config, exists, _ := m.registry.Get(serverID)
		if !exists || !config.Enabled {
			continue
		}
		m.mu.RLock()
		runtime := m.servers[serverID]
		m.mu.RUnlock()
		runtime.mu.Lock()
		if runtime.state != "connected" {
			runtime.mu.Unlock()
			continue
		}
		tools := cloneTools(runtime.tools)
		runtime.mu.Unlock()
		for _, tool := range tools {
			canonical := canonicalToolID(serverID, tool.Name)
			modelName := modelToolName(serverID, tool.Name)
			if previous := modelNames[modelName]; previous != "" {
				return fmt.Errorf("MCP model tool name %s collides between %s and %s", modelName, previous, canonical)
			}
			modelNames[modelName] = canonical
			descriptors = append(descriptors, toolbroker.Descriptor{
				ID: canonical, Name: modelName, Description: bounded(tool.Description, 4096), Schema: append(json.RawMessage(nil), tool.InputSchema...),
				RequiredCapability: "mcp.call", RequiredPolicy: "mcp.server:" + serverID, TimeoutMS: int64(config.CallTimeoutSeconds) * 1000,
				OutputLimit: 1024 * 1024, Effect: toolbroker.Uncertain, Available: true,
			})
			bindings[canonical] = toolBinding{serverID: serverID, tool: tool}
		}
	}
	if err := m.broker.ReplaceAdapterTools(brokerAdapterID, m, descriptors, true); err != nil {
		return err
	}
	m.mu.Lock()
	m.bindings = bindings
	m.mu.Unlock()
	m.notifyChanged()
	return nil
}

func (m *Manager) Call(ctx context.Context, callID, toolID string, arguments json.RawMessage) (any, toolbroker.EffectCertainty, error) {
	m.mu.RLock()
	binding, exists := m.bindings[toolID]
	runtime := m.servers[binding.serverID]
	m.mu.RUnlock()
	if !exists || runtime == nil {
		return nil, toolbroker.NoEffect, fmt.Errorf("MCP tool is no longer available")
	}
	callCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.active[callID] = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.active, callID)
		m.mu.Unlock()
	}()
	runtime.callMu.Lock()
	defer runtime.callMu.Unlock()
	client, err := m.ensure(callCtx, binding.serverID, false)
	if err != nil {
		return nil, toolbroker.NoEffect, err
	}
	started := time.Now()
	result, _, err := client.callTool(callCtx, binding.tool.Name, arguments)
	var notTransmitted *requestNotTransmittedError
	if errors.As(err, &notTransmitted) {
		// A failed stdin write proves the server never received this call. One
		// supervised restart is therefore safe; any later failure is returned
		// without replay because its effect is no longer knowable.
		runtime.mu.Lock()
		if runtime.client == client {
			runtime.client = nil
			runtime.state = "stopped"
		}
		runtime.mu.Unlock()
		_ = client.close()
		client, restartErr := m.ensure(callCtx, binding.serverID, false)
		if restartErr == nil {
			result, _, err = client.callTool(callCtx, binding.tool.Name, arguments)
		} else {
			err = &requestNotTransmittedError{cause: restartErr}
		}
	}
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
	}
	m.publish("McpToolInvoked", map[string]any{
		"server": binding.serverID, "broker_call": callID, "tool": toolID,
		"duration_ms": time.Since(started).Milliseconds(), "outcome": outcome,
	})
	if err != nil {
		if errors.As(err, &notTransmitted) {
			return nil, toolbroker.NoEffect, err
		}
		return nil, toolbroker.Uncertain, err
	}
	return result, toolbroker.Uncertain, nil
}

func (m *Manager) Cancel(callID string) {
	m.mu.RLock()
	cancel := m.active[callID]
	m.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	servers := make([]*serverRuntime, 0, len(m.servers))
	for _, runtime := range m.servers {
		servers = append(servers, runtime)
	}
	for _, cancel := range m.active {
		cancel()
	}
	m.mu.Unlock()
	for _, runtime := range servers {
		runtime.mu.Lock()
		client := runtime.client
		runtime.client = nil
		runtime.mu.Unlock()
		if client != nil {
			done := make(chan struct{})
			go func() { _ = client.close(); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (m *Manager) List() []ServerStatus {
	configs := m.registry.List()
	result := make([]ServerStatus, 0, len(configs))
	for _, config := range configs {
		_, _, pluginID := m.registry.Get(config.ID)
		source := "standalone"
		if pluginID != "" {
			source = "plugin"
		}
		status := ServerStatus{ID: config.ID, Config: config, Source: source, Plugin: pluginID, State: "disabled", Tools: []Tool{}}
		m.mu.RLock()
		runtime := m.servers[config.ID]
		m.mu.RUnlock()
		if runtime != nil {
			runtime.mu.Lock()
			status.State = runtime.state
			status.ProtocolVersion = runtime.protocol
			status.ServerName = runtime.serverName
			status.ServerVersion = runtime.serverVersion
			status.LastError = runtime.lastError
			status.MissingVariables = append([]string(nil), runtime.missing...)
			status.Tools = cloneTools(runtime.tools)
			status.UpdatedAt = runtime.updatedAt
			runtime.mu.Unlock()
		}
		result = append(result, status)
	}
	return result
}

func (m *Manager) Put(ctx context.Context, config ServerConfig) (ServerStatus, error) {
	previous, existed, _ := m.registry.Get(config.ID)
	saved, err := m.registry.Put(config)
	if err != nil {
		return ServerStatus{}, err
	}
	m.Sync()
	if saved.Enabled {
		if err := m.Refresh(ctx, saved.ID); err != nil {
			// Configuration remains stored so Studio can show and repair it.
			return m.status(saved.ID), err
		}
	}
	if !saved.Enabled {
		m.publish("McpToolsChanged", map[string]any{"server": saved.ID})
	}
	if existed && processFieldsChanged(previous, saved) {
		m.publish("McpServerDisconnected", map[string]any{"server": saved.ID, "reason": "configuration_changed"})
	}
	return m.status(saved.ID), nil
}

func (m *Manager) Delete(id string) error {
	if err := m.registry.Delete(id); err != nil {
		return err
	}
	m.Sync()
	m.publish("McpServerDisconnected", map[string]any{"server": id, "reason": "deleted"})
	m.publish("McpToolsChanged", map[string]any{"server": id})
	return nil
}

func (m *Manager) SetEnabled(ctx context.Context, id string, enabled bool) (ServerStatus, error) {
	config, exists, pluginID := m.registry.Get(id)
	if !exists {
		return ServerStatus{}, os.ErrNotExist
	}
	if pluginID != "" {
		return ServerStatus{}, fmt.Errorf("server %s is managed by plugin %s", id, pluginID)
	}
	config.Enabled = enabled
	return m.Put(ctx, config)
}

func (m *Manager) Test(ctx context.Context, id string) (ServerStatus, error) {
	if err := m.Refresh(ctx, id); err != nil {
		return m.status(id), err
	}
	return m.status(id), nil
}

func (m *Manager) Tools(id string) ([]Tool, error) {
	status := m.status(id)
	if status.ID == "" {
		return nil, os.ErrNotExist
	}
	return status.Tools, nil
}

func (m *Manager) status(id string) ServerStatus {
	for _, status := range m.List() {
		if status.ID == id {
			return status
		}
	}
	return ServerStatus{}
}

func (m *Manager) RegisterPluginServers(pluginID string, configs []ServerConfig) error {
	if err := m.registry.RegisterPlugin(pluginID, configs); err != nil {
		return err
	}
	m.Sync()
	return nil
}

func (m *Manager) UnregisterPluginServers(pluginID string) {
	m.registry.UnregisterPlugin(pluginID)
	m.Sync()
}

func (m *Manager) publish(event string, payload map[string]any) {
	if m.bus != nil {
		m.bus.Publish(events.EventType(event), events.Component("mcp"), payload)
	}
}

func signature(config ServerConfig) [sha256.Size]byte {
	data, _ := json.Marshal(config)
	return sha256.Sum256(data)
}

func processFieldsChanged(left, right ServerConfig) bool {
	left.Enabled, right.Enabled = false, false
	return signature(left) != signature(right)
}

var unsafeModelName = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func modelToolName(serverID, toolName string) string {
	base := "mcp__" + unsafeModelName.ReplaceAllString(serverID, "_") + "__" + unsafeModelName.ReplaceAllString(toolName, "_")
	base = strings.Trim(base, "_")
	if base == "" || base[0] < 'A' && (base[0] < 'a' || base[0] > 'z') && (base[0] < '0' || base[0] > '9') {
		base = "mcp__tool"
	}
	if len(base) <= 100 {
		return base
	}
	hash := sha256.Sum256([]byte(serverID + "\x00" + toolName))
	return base[:83] + "_" + hex.EncodeToString(hash[:8])
}

func canonicalToolID(serverID, toolName string) string {
	hash := sha256.Sum256([]byte(toolName))
	return "mcp." + serverID + "." + hex.EncodeToString(hash[:12])
}

func cloneTools(tools []Tool) []Tool {
	result := make([]Tool, len(tools))
	for i, tool := range tools {
		result[i] = tool
		result[i].InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
	}
	return result
}

func mappedValues(mappings map[string]string) []string {
	values := make([]string, 0, len(mappings))
	for _, hostName := range mappings {
		if value, exists := os.LookupEnv(hostName); exists && value != "" {
			values = append(values, value)
		}
	}
	return values
}

func redactValues(message string, values []string) string {
	for _, value := range values {
		message = strings.ReplaceAll(message, value, "[REDACTED]")
	}
	return message
}

func (m *Manager) AvailableDescriptors() []toolbroker.Descriptor {
	all := m.broker.Descriptors()
	result := make([]toolbroker.Descriptor, 0)
	for _, descriptor := range all {
		if descriptor.Adapter == brokerAdapterID {
			result = append(result, descriptor)
		}
	}
	return result
}

var _ toolbroker.Adapter = (*Manager)(nil)
