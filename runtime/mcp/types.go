package mcp

import (
	"encoding/json"
	"time"
)

const (
	LegacyProtocolVersion = "2025-11-25"
	ModernProtocolVersion = "2026-07-28"
	configFormatVersion   = 1
	maxMessageBytes       = 4 * 1024 * 1024
	maxToolsPerServer     = 256
)

type ServerConfig struct {
	ID                    string            `json:"id" yaml:"id"`
	Command               string            `json:"command" yaml:"command"`
	Args                  []string          `json:"args,omitempty" yaml:"args,omitempty"`
	CWD                   string            `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	Env                   map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Enabled               bool              `json:"enabled" yaml:"enabled"`
	Transport             string            `json:"transport" yaml:"transport"`
	StartupTimeoutSeconds int               `json:"startupTimeoutSeconds" yaml:"startupTimeoutSeconds"`
	CallTimeoutSeconds    int               `json:"callTimeoutSeconds" yaml:"callTimeoutSeconds"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations map[string]any  `json:"annotations,omitempty"`
}

type ServerStatus struct {
	ID               string       `json:"id"`
	Config           ServerConfig `json:"config"`
	Source           string       `json:"source"`
	Plugin           string       `json:"plugin,omitempty"`
	State            string       `json:"state"`
	ProtocolVersion  string       `json:"protocolVersion,omitempty"`
	ServerName       string       `json:"serverName,omitempty"`
	ServerVersion    string       `json:"serverVersion,omitempty"`
	LastError        string       `json:"lastError,omitempty"`
	MissingVariables []string     `json:"missingVariables,omitempty"`
	Tools            []Tool       `json:"tools"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

type persistedServers struct {
	FormatVersion int            `json:"formatVersion"`
	Servers       []ServerConfig `json:"servers"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type callResult struct {
	Content           []map[string]any `json:"content,omitempty"`
	StructuredContent any              `json:"structuredContent,omitempty"`
	IsError           bool             `json:"isError,omitempty"`
	Meta              map[string]any   `json:"_meta,omitempty"`
}
