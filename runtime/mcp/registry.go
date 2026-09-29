package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,119}$`)
var safeEnv = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type Registry struct {
	mu      sync.RWMutex
	path    string
	root    string
	servers map[string]ServerConfig
	plugins map[string]map[string]ServerConfig
}

func NewRegistry(root, path string) (*Registry, error) {
	registry := &Registry{root: root, path: path, servers: make(map[string]ServerConfig), plugins: make(map[string]map[string]ServerConfig)}
	if err := registry.load(); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read MCP registry: %w", err)
	}
	var snapshot persistedServers
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.FormatVersion != configFormatVersion {
		return fmt.Errorf("invalid MCP registry snapshot")
	}
	for _, config := range snapshot.Servers {
		validated, err := r.Validate(config)
		if err != nil {
			return fmt.Errorf("stored MCP server %q: %w", config.ID, err)
		}
		if _, duplicate := r.servers[validated.ID]; duplicate {
			return fmt.Errorf("duplicate stored MCP server %q", validated.ID)
		}
		r.servers[validated.ID] = validated
	}
	return nil
}

func (r *Registry) Validate(config ServerConfig) (ServerConfig, error) {
	return ValidateServerConfig(r.root, config)
}

// ValidateServerConfig applies the persisted registry contract without
// mutating a registry. Plugin installation uses it before publishing overlays.
func ValidateServerConfig(root string, config ServerConfig) (ServerConfig, error) {
	config.ID = strings.TrimSpace(config.ID)
	config.Command = strings.TrimSpace(config.Command)
	config.Transport = strings.TrimSpace(config.Transport)
	if !safeID.MatchString(config.ID) {
		return ServerConfig{}, fmt.Errorf("invalid server id")
	}
	if config.Command == "" || strings.ContainsRune(config.Command, 0) {
		return ServerConfig{}, fmt.Errorf("command is required")
	}
	if config.Transport == "" {
		config.Transport = "stdio"
	}
	if config.Transport != "stdio" {
		return ServerConfig{}, fmt.Errorf("transport must be stdio")
	}
	if len(config.Args) > 128 {
		return ServerConfig{}, fmt.Errorf("too many command arguments")
	}
	for _, argument := range config.Args {
		if len(argument) > 4096 || strings.ContainsRune(argument, 0) {
			return ServerConfig{}, fmt.Errorf("invalid command argument")
		}
	}
	if len(config.Env) > 64 {
		return ServerConfig{}, fmt.Errorf("too many environment mappings")
	}
	for childName, hostName := range config.Env {
		if !safeEnv.MatchString(childName) || !safeEnv.MatchString(hostName) {
			return ServerConfig{}, fmt.Errorf("environment entries must map variable names to host variable names")
		}
	}
	if config.CWD != "" {
		cwd := config.CWD
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(root, cwd)
		}
		absolute, err := filepath.Abs(cwd)
		if err != nil {
			return ServerConfig{}, fmt.Errorf("invalid cwd")
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ServerConfig{}, fmt.Errorf("cwd must be inside the configured checkout")
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.IsDir() {
			return ServerConfig{}, fmt.Errorf("cwd must name an existing directory")
		}
		config.CWD = absolute
	}
	if config.StartupTimeoutSeconds == 0 {
		config.StartupTimeoutSeconds = 15
	}
	if config.CallTimeoutSeconds == 0 {
		config.CallTimeoutSeconds = 30
	}
	if config.StartupTimeoutSeconds < 1 || config.StartupTimeoutSeconds > 120 || config.CallTimeoutSeconds < 1 || config.CallTimeoutSeconds > 7200 {
		return ServerConfig{}, fmt.Errorf("timeouts are outside supported bounds")
	}
	config.Args = append([]string(nil), config.Args...)
	config.Env = cloneMap(config.Env)
	return config, nil
}

func (r *Registry) List() []ServerConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	combined := make(map[string]ServerConfig, len(r.servers))
	for id, config := range r.servers {
		combined[id] = cloneConfig(config)
	}
	for _, servers := range r.plugins {
		for id, config := range servers {
			combined[id] = cloneConfig(config)
		}
	}
	result := make([]ServerConfig, 0, len(combined))
	for _, config := range combined {
		result = append(result, config)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (r *Registry) Get(id string) (ServerConfig, bool, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if config, ok := r.servers[id]; ok {
		return cloneConfig(config), true, ""
	}
	for pluginID, servers := range r.plugins {
		if config, ok := servers[id]; ok {
			return cloneConfig(config), true, pluginID
		}
	}
	return ServerConfig{}, false, ""
}

func (r *Registry) Put(config ServerConfig) (ServerConfig, error) {
	validated, err := r.Validate(config)
	if err != nil {
		return ServerConfig{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for pluginID, servers := range r.plugins {
		if _, exists := servers[validated.ID]; exists {
			return ServerConfig{}, fmt.Errorf("server %s is managed by plugin %s", validated.ID, pluginID)
		}
	}
	previous, existed := r.servers[validated.ID]
	r.servers[validated.ID] = validated
	if err := r.persistLocked(); err != nil {
		if existed {
			r.servers[validated.ID] = previous
		} else {
			delete(r.servers, validated.ID)
		}
		return ServerConfig{}, err
	}
	return cloneConfig(validated), nil
}

func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.servers[id]; !exists {
		for pluginID, servers := range r.plugins {
			if _, managed := servers[id]; managed {
				return fmt.Errorf("server %s is managed by plugin %s", id, pluginID)
			}
		}
		return os.ErrNotExist
	}
	previous := r.servers[id]
	delete(r.servers, id)
	if err := r.persistLocked(); err != nil {
		r.servers[id] = previous
		return err
	}
	return nil
}

func (r *Registry) RegisterPlugin(pluginID string, configs []ServerConfig) error {
	validated := make(map[string]ServerConfig, len(configs))
	for _, config := range configs {
		item, err := r.Validate(config)
		if err != nil {
			return fmt.Errorf("plugin %s MCP server %q: %w", pluginID, config.ID, err)
		}
		if _, duplicate := validated[item.ID]; duplicate {
			return fmt.Errorf("plugin %s repeats MCP server %s", pluginID, item.ID)
		}
		validated[item.ID] = item
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range validated {
		if _, exists := r.servers[id]; exists {
			return fmt.Errorf("MCP server %s collides with a standalone server", id)
		}
		for owner, servers := range r.plugins {
			if owner != pluginID {
				if _, exists := servers[id]; exists {
					return fmt.Errorf("MCP server %s collides with plugin %s", id, owner)
				}
			}
		}
	}
	r.plugins[pluginID] = validated
	return nil
}

func (r *Registry) UnregisterPlugin(pluginID string) {
	r.mu.Lock()
	delete(r.plugins, pluginID)
	r.mu.Unlock()
}

func (r *Registry) persistLocked() error {
	snapshot := persistedServers{FormatVersion: configFormatVersion, Servers: make([]ServerConfig, 0, len(r.servers))}
	for _, config := range r.servers {
		snapshot.Servers = append(snapshot.Servers, config)
	}
	sort.Slice(snapshot.Servers, func(i, j int) bool { return snapshot.Servers[i].ID < snapshot.Servers[j].ID })
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	temporary := r.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, r.path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func cloneConfig(config ServerConfig) ServerConfig {
	config.Args = append([]string(nil), config.Args...)
	config.Env = cloneMap(config.Env)
	return config
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
