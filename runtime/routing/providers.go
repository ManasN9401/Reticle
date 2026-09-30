package routing

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const providerConfigVersion = 1

var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var providerEnvPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ProviderModelConfig supplies a model when the provider does not expose a
// compatible /models endpoint. Dynamic discovery and static entries may be
// combined; duplicate upstream IDs are collapsed.
type ProviderModelConfig struct {
	ID           string  `json:"id"`
	Modality     string  `json:"modality,omitempty"`
	Capability   float64 `json:"capability,omitempty"`
	ContextLimit int     `json:"contextLimit,omitempty"`
}

// ProviderConfig is intentionally non-secret. APIKeyEnv names a variable in
// the process environment; its value is never written to this registry.
type ProviderConfig struct {
	ID          string                `json:"id"`
	Name        string                `json:"name,omitempty"`
	Protocol    string                `json:"protocol"`
	BaseURL     string                `json:"baseUrl"`
	APIKeyEnv   string                `json:"apiKeyEnv,omitempty"`
	ModelsPath  string                `json:"modelsPath,omitempty"`
	Models      []ProviderModelConfig `json:"models,omitempty"`
	Enabled     bool                  `json:"enabled"`
	ToolSupport string                `json:"toolSupport,omitempty"`
}

type ProviderStatus struct {
	Config           ProviderConfig `json:"config"`
	State            string         `json:"state"`
	LastError        string         `json:"lastError,omitempty"`
	MissingVariables []string       `json:"missingVariables,omitempty"`
	ModelCount       int            `json:"modelCount"`
}

type providerSnapshot struct {
	FormatVersion int              `json:"formatVersion"`
	Providers     []ProviderConfig `json:"providers"`
}

type ProviderRegistry struct {
	mu        sync.RWMutex
	path      string
	providers map[string]ProviderConfig
	statuses  map[string]ProviderStatus
}

func NewProviderRegistry(path string) (*ProviderRegistry, error) {
	r := &ProviderRegistry{path: path, providers: make(map[string]ProviderConfig), statuses: make(map[string]ProviderStatus)}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func ValidateProviderConfig(config ProviderConfig) (ProviderConfig, error) {
	config.ID = strings.ToLower(strings.TrimSpace(config.ID))
	config.Name = strings.TrimSpace(config.Name)
	config.Protocol = strings.ToLower(strings.TrimSpace(config.Protocol))
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.APIKeyEnv = strings.TrimSpace(config.APIKeyEnv)
	config.ModelsPath = strings.TrimSpace(config.ModelsPath)
	config.ToolSupport = strings.ToLower(strings.TrimSpace(config.ToolSupport))
	if !providerIDPattern.MatchString(config.ID) {
		return ProviderConfig{}, fmt.Errorf("provider id must start with a lowercase letter and contain only lowercase letters, digits, underscores or hyphens")
	}
	if len(config.Name) > 120 {
		return ProviderConfig{}, fmt.Errorf("provider name is too long")
	}
	if config.Protocol == "" {
		config.Protocol = "openai-compatible"
	}
	if config.Protocol != "openai-compatible" {
		return ProviderConfig{}, fmt.Errorf("protocol must be openai-compatible")
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ProviderConfig{}, fmt.Errorf("baseUrl must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return ProviderConfig{}, fmt.Errorf("non-loopback providers must use HTTPS")
		}
	}
	if config.APIKeyEnv != "" && !providerEnvPattern.MatchString(config.APIKeyEnv) {
		return ProviderConfig{}, fmt.Errorf("apiKeyEnv is not a valid environment-variable name")
	}
	if config.ModelsPath == "" {
		config.ModelsPath = "/models"
	}
	if !strings.HasPrefix(config.ModelsPath, "/") || strings.Contains(config.ModelsPath, "://") || strings.Contains(config.ModelsPath, "..") || len(config.ModelsPath) > 256 {
		return ProviderConfig{}, fmt.Errorf("modelsPath must be a bounded absolute URL path")
	}
	if config.ToolSupport == "" {
		config.ToolSupport = "unknown"
	}
	if config.ToolSupport != "unknown" && config.ToolSupport != "advertised" {
		return ProviderConfig{}, fmt.Errorf("toolSupport must be unknown or advertised")
	}
	if len(config.Models) > 256 {
		return ProviderConfig{}, fmt.Errorf("too many static models")
	}
	seen := make(map[string]struct{}, len(config.Models))
	for i := range config.Models {
		model := &config.Models[i]
		model.ID = strings.TrimSpace(model.ID)
		model.Modality = strings.ToLower(strings.TrimSpace(model.Modality))
		if model.ID == "" || len(model.ID) > 256 || strings.ContainsAny(model.ID, "\x00\r\n") {
			return ProviderConfig{}, fmt.Errorf("invalid static model id")
		}
		if _, duplicate := seen[model.ID]; duplicate {
			return ProviderConfig{}, fmt.Errorf("duplicate static model %q", model.ID)
		}
		seen[model.ID] = struct{}{}
		if model.Modality == "" {
			model.Modality = detectModality(model.ID)
		}
		if model.Modality != "text" && model.Modality != "coding" && model.Modality != "image" {
			return ProviderConfig{}, fmt.Errorf("model %q has unsupported modality", model.ID)
		}
		if model.Capability < 0 || model.ContextLimit < 0 {
			return ProviderConfig{}, fmt.Errorf("model metadata cannot be negative")
		}
	}
	config.Models = append([]ProviderModelConfig(nil), config.Models...)
	return config, nil
}

func (r *ProviderRegistry) load() error {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read provider registry: %w", err)
	}
	var snapshot providerSnapshot
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.FormatVersion != providerConfigVersion {
		return fmt.Errorf("invalid provider registry snapshot")
	}
	for _, config := range snapshot.Providers {
		validated, err := ValidateProviderConfig(config)
		if err != nil {
			return fmt.Errorf("stored provider %q: %w", config.ID, err)
		}
		if _, duplicate := r.providers[validated.ID]; duplicate {
			return fmt.Errorf("duplicate stored provider %q", validated.ID)
		}
		r.providers[validated.ID] = validated
	}
	return nil
}

func (r *ProviderRegistry) Configs() []ProviderConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ProviderConfig, 0, len(r.providers))
	for _, config := range r.providers {
		config.Models = append([]ProviderModelConfig(nil), config.Models...)
		result = append(result, config)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (r *ProviderRegistry) List() []ProviderStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ProviderStatus, 0, len(r.providers))
	for id, config := range r.providers {
		status, ok := r.statuses[id]
		if !ok {
			status = ProviderStatus{Config: config, State: "not_tested"}
		}
		status.Config.Models = append([]ProviderModelConfig(nil), status.Config.Models...)
		status.MissingVariables = append([]string(nil), status.MissingVariables...)
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Config.ID < result[j].Config.ID })
	return result
}

func (r *ProviderRegistry) setStatus(status ProviderStatus) {
	r.mu.Lock()
	r.statuses[status.Config.ID] = status
	r.mu.Unlock()
}

func (r *ProviderRegistry) Put(config ProviderConfig) (ProviderConfig, error) {
	validated, err := ValidateProviderConfig(config)
	if err != nil {
		return ProviderConfig{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, existed := r.providers[validated.ID]
	if !existed && len(r.providers) >= 64 {
		return ProviderConfig{}, fmt.Errorf("provider registry is limited to 64 profiles")
	}
	r.providers[validated.ID] = validated
	delete(r.statuses, validated.ID)
	if err := r.persistLocked(); err != nil {
		if existed {
			r.providers[validated.ID] = previous
		} else {
			delete(r.providers, validated.ID)
		}
		return ProviderConfig{}, err
	}
	return validated, nil
}

func (r *ProviderRegistry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, exists := r.providers[id]
	if !exists {
		return os.ErrNotExist
	}
	delete(r.providers, id)
	delete(r.statuses, id)
	if err := r.persistLocked(); err != nil {
		r.providers[id] = previous
		return err
	}
	return nil
}

func (r *ProviderRegistry) persistLocked() error {
	snapshot := providerSnapshot{FormatVersion: providerConfigVersion, Providers: make([]ProviderConfig, 0, len(r.providers))}
	for _, config := range r.providers {
		snapshot.Providers = append(snapshot.Providers, config)
	}
	sort.Slice(snapshot.Providers, func(i, j int) bool { return snapshot.Providers[i].ID < snapshot.Providers[j].ID })
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(r.path), ".providers-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(append(data, '\n'))
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryName, r.path); err == nil {
		return nil
	}
	backup := r.path + ".previous"
	_ = os.Remove(backup)
	if moveErr := os.Rename(r.path, backup); moveErr != nil && !errors.Is(moveErr, os.ErrNotExist) {
		return err
	}
	if moveErr := os.Rename(temporaryName, r.path); moveErr != nil {
		_ = os.Rename(backup, r.path)
		return moveErr
	}
	_ = os.Remove(backup)
	return nil
}
