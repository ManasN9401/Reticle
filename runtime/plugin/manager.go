package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/mcp"
	"github.com/reticle/runtime/toolbroker"
)

const pluginAdapterID = "plugins"

type Manager struct {
	ops           sync.Mutex
	mu            sync.RWMutex
	root          string
	pluginsRoot   string
	statePath     string
	registry      *agent.Registry
	dispatcher    *agent.Dispatcher
	environments  *agent.EnvironmentManager
	subscriptions *agent.SubscriptionManager
	mcp           *mcp.Manager
	broker        *toolbroker.Broker
	bus           *events.Bus
	log           *logger.Logger
	enabled       map[string]bool
	loaded        map[string]*loadedBundle
	bindings      map[string]toolBinding
	active        map[string]context.CancelFunc
	changed       func()
}

func NewManager(root string, registry *agent.Registry, dispatcher *agent.Dispatcher, environments *agent.EnvironmentManager, subscriptions *agent.SubscriptionManager, mcpManager *mcp.Manager, broker *toolbroker.Broker, bus *events.Bus, log *logger.Logger) (*Manager, error) {
	manager := &Manager{
		root: root, pluginsRoot: filepath.Join(root, ".reticle", "plugins"), statePath: filepath.Join(root, ".reticle", "plugins", "state.json"),
		registry: registry, dispatcher: dispatcher, environments: environments, subscriptions: subscriptions,
		mcp: mcpManager, broker: broker, bus: bus, log: log, enabled: make(map[string]bool), loaded: make(map[string]*loadedBundle),
		bindings: make(map[string]toolBinding), active: make(map[string]context.CancelFunc),
	}
	if err := manager.loadState(); err != nil {
		return nil, err
	}
	if err := broker.Register(pluginAdapterID, manager, nil); err != nil {
		return nil, err
	}
	return manager, nil
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

func (m *Manager) LoadEnabled(ctx context.Context) {
	entries, err := os.ReadDir(m.pluginsRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.publish("PluginError", map[string]any{"error": bounded(err.Error(), 1024)})
		return
	}
	ids := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() && safeID.MatchString(entry.Name()) && m.enabled[entry.Name()] {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := m.Enable(ctx, id); err != nil {
			m.publish("PluginError", map[string]any{"plugin": id, "error": bounded(err.Error(), 1024)})
		}
	}
}

func (m *Manager) List() []View {
	entries, _ := os.ReadDir(m.pluginsRoot)
	views := make([]View, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !safeID.MatchString(entry.Name()) {
			continue
		}
		root := filepath.Join(m.pluginsRoot, entry.Name())
		bundle, err := loadBundle(root)
		view := View{ID: entry.Name(), Enabled: m.isEnabled(entry.Name()), Valid: err == nil, Path: root, UpdatedAt: time.Now().UTC(), Dependencies: []Dependency{}, Agents: []string{}, Skills: []string{}, Tools: []string{}, MCPServers: []string{}}
		if err != nil {
			view.Error = bounded(err.Error(), 1024)
		} else {
			view = viewForBundle(bundle, m.isEnabled(entry.Name()))
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	return views
}

func (m *Manager) Install(source string) (View, error) {
	m.ops.Lock()
	defer m.ops.Unlock()
	return m.install(source)
}

func (m *Manager) install(source string) (View, error) {
	absolute, err := filepath.Abs(source)
	if err != nil {
		return View{}, err
	}
	bundle, err := loadBundle(absolute)
	if err != nil {
		return View{}, err
	}
	if err := os.MkdirAll(m.pluginsRoot, 0700); err != nil {
		return View{}, err
	}
	target := filepath.Join(m.pluginsRoot, bundle.manifest.ID)
	if _, err := os.Stat(target); err == nil {
		return View{}, fmt.Errorf("plugin %s is already installed", bundle.manifest.ID)
	}
	temporary, err := os.MkdirTemp(m.pluginsRoot, ".install-")
	if err != nil {
		return View{}, err
	}
	defer os.RemoveAll(temporary)
	payload := filepath.Join(temporary, bundle.manifest.ID)
	if err := copyBundle(absolute, payload); err != nil {
		return View{}, err
	}
	if _, err := loadBundle(payload); err != nil {
		return View{}, fmt.Errorf("copied plugin failed validation: %w", err)
	}
	if err := os.Rename(payload, target); err != nil {
		return View{}, err
	}
	m.mu.Lock()
	m.enabled[bundle.manifest.ID] = false
	err = m.persistStateLocked()
	m.mu.Unlock()
	if err != nil {
		_ = os.RemoveAll(target)
		return View{}, err
	}
	installed, _ := loadBundle(target)
	view := viewForBundle(installed, false)
	m.publish("PluginInstalled", map[string]any{"plugin": view.ID, "version": view.Version})
	return view, nil
}

func (m *Manager) Enable(ctx context.Context, id string) (View, error) {
	m.ops.Lock()
	defer m.ops.Unlock()
	return m.enable(ctx, id)
}

func (m *Manager) enable(ctx context.Context, id string) (View, error) {
	if !safeID.MatchString(id) {
		return View{}, fmt.Errorf("invalid plugin id")
	}
	m.mu.RLock()
	if existing := m.loaded[id]; existing != nil {
		view := viewForBundle(existing, true)
		m.mu.RUnlock()
		return view, nil
	}
	m.mu.RUnlock()
	bundle, err := loadBundle(filepath.Join(m.pluginsRoot, id))
	if err != nil {
		return View{}, err
	}
	if err := m.validateDependencies(bundle); err != nil {
		return View{}, err
	}
	if err := m.registry.RegisterBundle(bundle.agents, bundle.skills); err != nil {
		return View{}, err
	}
	agentIDs := sortedAgentIDs(bundle.agents)
	skillIDs := sortedSkillIDs(bundle.skills)
	rollbackRegistry := true
	defer func() {
		if rollbackRegistry {
			m.registry.UnregisterBundle(agentIDs, skillIDs)
		}
	}()
	if err := m.mcp.RegisterPluginServers(id, bundle.manifest.MCPServers); err != nil {
		return View{}, err
	}
	rollbackMCP := true
	defer func() {
		if rollbackMCP {
			m.mcp.UnregisterPluginServers(id)
		}
	}()
	m.mu.Lock()
	prospective := make(map[string]*loadedBundle, len(m.loaded)+1)
	for pluginID, loaded := range m.loaded {
		prospective[pluginID] = loaded
	}
	prospective[id] = bundle
	m.mu.Unlock()
	if err := m.replaceTools(prospective); err != nil {
		return View{}, err
	}
	workers := m.registry.BuildWorkersByID(agentIDs, m.log, m.bus, m.environments)
	if len(workers) != len(agentIDs) {
		_ = m.replaceTools(m.snapshotLoaded())
		return View{}, fmt.Errorf("plugin workers could not be constructed")
	}
	for _, worker := range workers {
		m.dispatcher.RegisterWorker(worker)
	}
	local := agent.NewRegistry()
	local.Definitions = bundle.agents
	subscriptions := local.BuildSubscriptions()
	for _, subscription := range subscriptions {
		m.subscriptions.Register(subscription)
	}
	m.mu.Lock()
	m.loaded[id] = bundle
	m.enabled[id] = true
	if err := m.persistStateLocked(); err != nil {
		m.mu.Unlock()
		for _, subscription := range subscriptions {
			m.subscriptions.Remove(subscription.ID)
		}
		for _, workerID := range agentIDs {
			m.dispatcher.UnregisterWorker(workerID)
		}
		_ = m.replaceTools(m.snapshotLoaded())
		return View{}, err
	}
	m.mu.Unlock()
	rollbackRegistry = false
	rollbackMCP = false
	m.publish("PluginEnabled", map[string]any{"plugin": id, "version": bundle.manifest.Version})
	m.notifyChanged()
	return viewForBundle(bundle, true), nil
}

func (m *Manager) Disable(id string) (View, error) {
	m.ops.Lock()
	defer m.ops.Unlock()
	return m.disable(id)
}

func (m *Manager) disable(id string) (View, error) {
	m.mu.Lock()
	bundle := m.loaded[id]
	if bundle == nil {
		m.enabled[id] = false
		err := m.persistStateLocked()
		m.mu.Unlock()
		if err != nil {
			return View{}, err
		}
		if unloaded, loadErr := loadBundle(filepath.Join(m.pluginsRoot, id)); loadErr == nil {
			return viewForBundle(unloaded, false), nil
		}
		return View{}, os.ErrNotExist
	}
	delete(m.loaded, id)
	m.enabled[id] = false
	remaining := make(map[string]*loadedBundle, len(m.loaded))
	for pluginID, loaded := range m.loaded {
		remaining[pluginID] = loaded
	}
	m.mu.Unlock()
	if dependants := m.enabledDependants(id); len(dependants) > 0 {
		m.mu.Lock()
		m.loaded[id] = bundle
		m.enabled[id] = true
		m.mu.Unlock()
		return View{}, fmt.Errorf("plugin is required by enabled plugins: %s", strings.Join(dependants, ", "))
	}
	if err := m.replaceTools(remaining); err != nil {
		m.mu.Lock()
		m.loaded[id] = bundle
		m.enabled[id] = true
		m.mu.Unlock()
		return View{}, err
	}
	local := agent.NewRegistry()
	local.Definitions = bundle.agents
	for _, subscription := range local.BuildSubscriptions() {
		m.subscriptions.Remove(subscription.ID)
	}
	agentIDs := sortedAgentIDs(bundle.agents)
	for _, workerID := range agentIDs {
		m.dispatcher.UnregisterWorker(workerID)
	}
	m.registry.UnregisterBundle(agentIDs, sortedSkillIDs(bundle.skills))
	m.mcp.UnregisterPluginServers(id)
	m.mu.Lock()
	err := m.persistStateLocked()
	m.mu.Unlock()
	if err != nil {
		return View{}, err
	}
	m.publish("PluginDisabled", map[string]any{"plugin": id})
	m.notifyChanged()
	return viewForBundle(bundle, false), nil
}

func (m *Manager) Delete(id string) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	if m.isEnabled(id) {
		if _, err := m.disable(id); err != nil {
			return err
		}
	}
	if dependants := m.installedDependants(id); len(dependants) > 0 {
		return fmt.Errorf("plugin is required by installed plugins: %s", strings.Join(dependants, ", "))
	}
	target := filepath.Join(m.pluginsRoot, id)
	if !safeID.MatchString(id) || filepath.Dir(target) != m.pluginsRoot {
		return fmt.Errorf("invalid plugin id")
	}
	if _, err := os.Stat(target); err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.enabled, id)
	err := m.persistStateLocked()
	m.mu.Unlock()
	if err == nil {
		m.publish("PluginDeleted", map[string]any{"plugin": id})
		m.notifyChanged()
	}
	return err
}

func (m *Manager) Call(ctx context.Context, callID, toolID string, arguments json.RawMessage) (any, toolbroker.EffectCertainty, error) {
	m.mu.RLock()
	binding, exists := m.bindings[toolID]
	m.mu.RUnlock()
	if !exists {
		return nil, toolbroker.NoEffect, fmt.Errorf("plugin tool is no longer available")
	}
	callCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.active[callID] = cancel
	m.mu.Unlock()
	defer func() { cancel(); m.mu.Lock(); delete(m.active, callID); m.mu.Unlock() }()
	result, err := invokePluginTool(callCtx, callID, binding, arguments, m.log)
	if err != nil {
		return nil, binding.manifest.Effect, err
	}
	return result, binding.manifest.Effect, nil
}

func (m *Manager) Cancel(callID string) {
	m.mu.RLock()
	cancel := m.active[callID]
	m.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) Shutdown(context.Context) error {
	m.mu.Lock()
	for _, cancel := range m.active {
		cancel()
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) replaceTools(plugins map[string]*loadedBundle) error {
	ids := make([]string, 0, len(plugins))
	for id := range plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	descriptors := make([]toolbroker.Descriptor, 0)
	bindings := make(map[string]toolBinding)
	for _, id := range ids {
		bundle := plugins[id]
		descriptors = append(descriptors, bundle.descriptors...)
		for toolID, binding := range bundle.bindings {
			bindings[toolID] = binding
		}
	}
	if err := m.broker.ReplaceAdapterTools(pluginAdapterID, m, descriptors, true); err != nil {
		return err
	}
	m.mu.Lock()
	m.bindings = bindings
	m.mu.Unlock()
	return nil
}

func (m *Manager) validateDependencies(bundle *loadedBundle) error {
	declared := make(map[string]bool, len(bundle.manifest.Dependencies))
	for _, dependency := range bundle.manifest.Dependencies {
		declared[dependency.Plugin] = true
		m.mu.RLock()
		installed := m.loaded[dependency.Plugin]
		m.mu.RUnlock()
		if installed == nil {
			return fmt.Errorf("dependency %s is not enabled", dependency.Plugin)
		}
		if !satisfies(installed.manifest.Version, dependency.VersionRange) {
			return fmt.Errorf("dependency %s version %s does not satisfy %s", dependency.Plugin, installed.manifest.Version, dependency.VersionRange)
		}
	}
	for agentID, definition := range bundle.agents {
		for _, skillID := range definition.Skills {
			if _, bundled := bundle.skills[skillID]; bundled {
				continue
			}
			m.mu.RLock()
			owner := ""
			for pluginID, loaded := range m.loaded {
				if _, contributed := loaded.skills[skillID]; contributed {
					owner = pluginID
					break
				}
			}
			m.mu.RUnlock()
			if owner != "" && !declared[owner] {
				return fmt.Errorf("agent %s uses skill %s from plugin %s without declaring that dependency", agentID, skillID, owner)
			}
		}
	}
	return nil
}

func (m *Manager) enabledDependants(id string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := []string{}
	for pluginID, bundle := range m.loaded {
		for _, dependency := range bundle.manifest.Dependencies {
			if dependency.Plugin == id {
				result = append(result, pluginID)
			}
		}
	}
	sort.Strings(result)
	return result
}

func (m *Manager) installedDependants(id string) []string {
	result := []string{}
	for _, view := range m.List() {
		for _, dependency := range view.Dependencies {
			if dependency.Plugin == id {
				result = append(result, view.ID)
			}
		}
	}
	sort.Strings(result)
	return result
}

func (m *Manager) isEnabled(id string) bool { m.mu.RLock(); defer m.mu.RUnlock(); return m.enabled[id] }
func (m *Manager) snapshotLoaded() map[string]*loadedBundle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := map[string]*loadedBundle{}
	for id, bundle := range m.loaded {
		result[id] = bundle
	}
	return result
}

func (m *Manager) loadState() error {
	data, err := os.ReadFile(m.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state persistedState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || state.FormatVersion != stateFormatVersion {
		return fmt.Errorf("invalid plugin state")
	}
	for id, enabled := range state.Enabled {
		if safeID.MatchString(id) {
			m.enabled[id] = enabled
		}
	}
	return nil
}

func (m *Manager) persistStateLocked() error {
	state := persistedState{FormatVersion: stateFormatVersion, Enabled: m.enabled}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.pluginsRoot, 0700); err != nil {
		return err
	}
	temporary := m.statePath + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, m.statePath); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func viewForBundle(bundle *loadedBundle, enabled bool) View {
	view := View{ID: bundle.manifest.ID, Version: bundle.manifest.Version, Description: bundle.manifest.Description, Enabled: enabled, Valid: true, Path: bundle.root, UpdatedAt: time.Now().UTC(), Dependencies: append([]Dependency(nil), bundle.manifest.Dependencies...), Agents: []string{}, Skills: []string{}, Tools: []string{}, MCPServers: []string{}}
	for id := range bundle.agents {
		view.Agents = append(view.Agents, string(id))
	}
	for id := range bundle.skills {
		view.Skills = append(view.Skills, id)
	}
	for _, descriptor := range bundle.descriptors {
		view.Tools = append(view.Tools, descriptor.ID)
	}
	for _, server := range bundle.manifest.MCPServers {
		view.MCPServers = append(view.MCPServers, server.ID)
	}
	sort.Strings(view.Agents)
	sort.Strings(view.Skills)
	sort.Strings(view.Tools)
	sort.Strings(view.MCPServers)
	return view
}

func sortedAgentIDs(values map[agent.WorkerID]agent.AgentDefinition) []agent.WorkerID {
	result := make([]agent.WorkerID, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
func sortedSkillIDs(values map[string]agent.SkillDefinition) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func copyBundle(source, target string) error {
	count := 0
	total := int64(0)
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin bundles may not contain symlinks")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0700)
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("plugin payload contains a non-regular file")
		}
		count++
		total += info.Size()
		if count > 10_000 || total > 100*1024*1024 {
			return fmt.Errorf("plugin payload exceeds installation limits")
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return closeErr
	})
}

type pluginToolResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

func invokePluginTool(ctx context.Context, callID string, binding toolBinding, arguments json.RawMessage, log *logger.Logger) (any, error) {
	command := exec.CommandContext(ctx, binding.manifest.Command, binding.manifest.Args...)
	command.Dir = binding.root
	command.Env = safeChildEnvironment()
	request, _ := json.Marshal(map[string]any{"id": callID, "tool": binding.manifest.ID, "arguments": json.RawMessage(arguments)})
	command.Stdin = bytes.NewReader(append(request, '\n'))
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 1024), 64*1024)
		for scanner.Scan() {
			if log != nil {
				log.Info("Plugin tool diagnostic", "plugin", binding.pluginID, "tool", binding.manifest.ID, "message", logger.Redact(bounded(scanner.Text(), 1024)))
			}
		}
	}()
	reader := bufio.NewReader(stdout)
	line, err := readLimitedLine(reader, binding.manifest.OutputLimit+4096)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	extra, extraErr := reader.ReadByte()
	if extraErr == nil || (extraErr != io.EOF && extra != 0) {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("plugin tool emitted multiple stdout values")
	}
	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf("plugin tool failed: %w", err)
	}
	var response pluginToolResponse
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("plugin tool returned invalid JSON")
	}
	if response.Error != "" {
		return nil, fmt.Errorf("plugin tool error: %s", bounded(response.Error, 1024))
	}
	return response.Result, nil
}

func readLimitedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	line := make([]byte, 0, min(limit, 4096))
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > limit {
			return nil, fmt.Errorf("plugin tool output exceeds limit")
		}
		line = append(line, fragment...)
		switch err {
		case nil:
			return bytes.TrimSpace(line), nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if len(line) == 0 {
				return nil, io.EOF
			}
			return bytes.TrimSpace(line), nil
		default:
			return nil, err
		}
	}
}

func safeChildEnvironment() []string {
	allowed := map[string]bool{"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true, "LANG": true, "LC_ALL": true}
	result := []string{}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if allowed[strings.ToUpper(key)] {
			result = append(result, item)
		}
	}
	return result
}

func bounded(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
func (m *Manager) publish(event string, payload map[string]any) {
	if m.bus != nil {
		m.bus.Publish(events.EventType(event), events.Component("plugin"), payload)
	}
}

var _ toolbroker.Adapter = (*Manager)(nil)
