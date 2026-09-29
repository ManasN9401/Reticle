package plugin

import (
	"encoding/json"
	"time"

	"github.com/reticle/runtime/mcp"
	"github.com/reticle/runtime/toolbroker"
)

const (
	ManifestFilename      = "plugin.json"
	CurrentReticleVersion = "0.1.0"
	stateFormatVersion    = 1
)

type Dependency struct {
	Plugin       string `json:"plugin"`
	VersionRange string `json:"versionRange"`
}

type Compatibility struct {
	ReticleVersion string `json:"reticleVersion"`
}

type ToolManifest struct {
	ID                 string                     `json:"id"`
	Name               string                     `json:"name"`
	Description        string                     `json:"description"`
	Schema             json.RawMessage            `json:"schema"`
	RequiredCapability string                     `json:"requiredCapability"`
	Command            string                     `json:"command"`
	Args               []string                   `json:"args,omitempty"`
	TimeoutMS          int64                      `json:"timeoutMs,omitempty"`
	OutputLimit        int                        `json:"outputLimit,omitempty"`
	Effect             toolbroker.EffectCertainty `json:"effect"`
}

type Manifest struct {
	ID            string             `json:"id"`
	Version       string             `json:"version"`
	Description   string             `json:"description,omitempty"`
	Agents        []string           `json:"agents,omitempty"`
	Skills        []string           `json:"skills,omitempty"`
	Tools         []ToolManifest     `json:"tools,omitempty"`
	MCPServers    []mcp.ServerConfig `json:"mcpServers,omitempty"`
	Dependencies  []Dependency       `json:"dependencies,omitempty"`
	Compatibility Compatibility      `json:"compatibility"`
}

type View struct {
	ID              string       `json:"id"`
	Version         string       `json:"version"`
	Description     string       `json:"description,omitempty"`
	Enabled         bool         `json:"enabled"`
	Valid           bool         `json:"valid"`
	Error           string       `json:"error,omitempty"`
	RestartRequired bool         `json:"restartRequired"`
	Dependencies    []Dependency `json:"dependencies"`
	Agents          []string     `json:"agents"`
	Skills          []string     `json:"skills"`
	Tools           []string     `json:"tools"`
	MCPServers      []string     `json:"mcpServers"`
	Path            string       `json:"path"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

type persistedState struct {
	FormatVersion int             `json:"formatVersion"`
	Enabled       map[string]bool `json:"enabled"`
}

type toolBinding struct {
	pluginID string
	root     string
	manifest ToolManifest
}
