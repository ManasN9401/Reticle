package mcp

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type responseEnvelope struct {
	message rpcMessage
	err     error
}

type client struct {
	config         ServerConfig
	command        *exec.Cmd
	stdin          io.WriteCloser
	writeMu        sync.Mutex
	mu             sync.Mutex
	pending        map[string]chan responseEnvelope
	nextID         atomic.Int64
	done           chan struct{}
	closeOnce      sync.Once
	protocol       string
	serverName     string
	serverVersion  string
	onDiagnostic   func(string)
	onToolsChanged func()
}

type rpcCallError struct {
	Code    int
	Message string
}

type requestNotTransmittedError struct{ cause error }

func (e *requestNotTransmittedError) Error() string {
	return "request not transmitted: " + e.cause.Error()
}
func (e *requestNotTransmittedError) Unwrap() error { return e.cause }

func (e *rpcCallError) Error() string { return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message) }

func startClient(ctx context.Context, config ServerConfig, environment []string, onDiagnostic func(string), onToolsChanged func()) (*client, error) {
	command := exec.Command(config.Command, config.Args...)
	command.Env = environment
	if config.CWD != "" {
		command.Dir = config.CWD
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
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
	c := &client{
		config: config, command: command, stdin: stdin, pending: make(map[string]chan responseEnvelope),
		done: make(chan struct{}), onDiagnostic: onDiagnostic, onToolsChanged: onToolsChanged,
	}
	go c.readLoop(stdout)
	go c.stderrLoop(stderr)
	go func() {
		err := command.Wait()
		if err == nil {
			err = io.EOF
		}
		c.fail(err)
	}()
	startupCtx, cancel := context.WithTimeout(ctx, time.Duration(config.StartupTimeoutSeconds)*time.Second)
	defer cancel()
	if err := c.negotiate(startupCtx); err != nil {
		_ = c.close()
		return nil, err
	}
	return c, nil
}

func (c *client) negotiate(ctx context.Context) error {
	// Current MCP first: 2026-07-28 removed the handshake and added
	// server/discover. A method-not-found response falls back to the legacy
	// handshake required by RFC-046 and widely deployed servers.
	var discovery struct {
		ProtocolVersions []string `json:"protocolVersions"`
		ProtocolVersion  string   `json:"protocolVersion"`
		ServerInfo       struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	raw, _, err := c.request(ctx, "server/discover", map[string]any{
		"_meta": modernMeta(),
	})
	if err == nil && json.Unmarshal(raw, &discovery) == nil {
		versions := append([]string(nil), discovery.ProtocolVersions...)
		if discovery.ProtocolVersion != "" {
			versions = append(versions, discovery.ProtocolVersion)
		}
		for _, version := range versions {
			if version == ModernProtocolVersion {
				c.protocol = ModernProtocolVersion
				c.serverName = bounded(discovery.ServerInfo.Name, 200)
				c.serverVersion = bounded(discovery.ServerInfo.Version, 100)
				return nil
			}
		}
	}
	if err != nil {
		var rpcErr *rpcCallError
		if !errors.As(err, &rpcErr) {
			return fmt.Errorf("MCP discovery failed: %w", err)
		}
	}
	params := map[string]any{
		"protocolVersion": LegacyProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "reticle", "version": "0.1.0"},
	}
	raw, _, err = c.request(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("MCP initialize failed: %w", err)
	}
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &initialized); err != nil || initialized.ProtocolVersion == "" {
		return fmt.Errorf("MCP initialize returned an invalid result")
	}
	c.protocol = initialized.ProtocolVersion
	c.serverName = bounded(initialized.ServerInfo.Name, 200)
	c.serverVersion = bounded(initialized.ServerInfo.Version, 100)
	return c.notify("notifications/initialized", map[string]any{})
}

func modernMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ModernProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]string{"name": "reticle", "version": "0.1.0"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
}

func (c *client) methodParams(params map[string]any) map[string]any {
	if params == nil {
		params = make(map[string]any)
	}
	if c.protocol == ModernProtocolVersion {
		params["_meta"] = modernMeta()
	}
	return params
}

func (c *client) request(ctx context.Context, method string, params any) (json.RawMessage, int64, error) {
	id := c.nextID.Add(1)
	key := strconv.FormatInt(id, 10)
	response := make(chan responseEnvelope, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, id, io.EOF
	default:
	}
	c.pending[key] = response
	c.mu.Unlock()
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	if err := c.write(payload); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return nil, id, &requestNotTransmittedError{cause: err}
	}
	select {
	case envelope := <-response:
		if envelope.err != nil {
			return nil, id, envelope.err
		}
		if envelope.message.Error != nil {
			return nil, id, &rpcCallError{Code: envelope.message.Error.Code, Message: bounded(envelope.message.Error.Message, 1024)}
		}
		return append(json.RawMessage(nil), envelope.message.Result...), id, nil
	case <-ctx.Done():
		_ = c.notify("notifications/cancelled", map[string]any{"requestId": id, "reason": "Reticle call cancelled"})
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return nil, id, ctx.Err()
	case <-c.done:
		return nil, id, io.EOF
	}
}

func (c *client) notify(method string, params any) error {
	payload := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		payload["params"] = params
	}
	return c.write(payload)
}

func (c *client) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxMessageBytes {
		return fmt.Errorf("MCP request exceeds %d bytes", maxMessageBytes)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (c *client) readLoop(reader io.Reader) {
	buffer := bufio.NewReaderSize(reader, 64*1024)
	for {
		line, err := readBoundedLine(buffer, maxMessageBytes)
		if err != nil {
			c.fail(err)
			return
		}
		var message rpcMessage
		decoder := json.NewDecoder(bytes.NewReader(line))
		if err := decoder.Decode(&message); err != nil || message.JSONRPC != "2.0" {
			c.fail(fmt.Errorf("malformed MCP JSON-RPC message"))
			return
		}
		if len(message.ID) > 0 && message.Method == "" {
			key := rpcIDKey(message.ID)
			c.mu.Lock()
			waiter := c.pending[key]
			delete(c.pending, key)
			c.mu.Unlock()
			if waiter != nil {
				waiter <- responseEnvelope{message: message}
			}
			continue
		}
		if message.Method == "notifications/tools/list_changed" {
			if c.onToolsChanged != nil {
				go c.onToolsChanged()
			}
			continue
		}
		if len(message.ID) > 0 && message.Method == "ping" {
			_ = c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(message.ID), "result": map[string]any{}})
		}
	}
}

func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var result []byte
	for {
		fragment, more, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(result)+len(fragment) > limit {
			return nil, fmt.Errorf("MCP response exceeds %d bytes", limit)
		}
		result = append(result, fragment...)
		if !more {
			return result, nil
		}
	}
}

func (c *client) stderrLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		if c.onDiagnostic != nil {
			c.onDiagnostic(bounded(scanner.Text(), 1024))
		}
	}
}

func (c *client) fail(err error) {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mu.Lock()
		pending := c.pending
		c.pending = make(map[string]chan responseEnvelope)
		c.mu.Unlock()
		for _, waiter := range pending {
			waiter <- responseEnvelope{err: err}
		}
	})
}

func (c *client) listTools(ctx context.Context) ([]Tool, error) {
	tools := make([]Tool, 0)
	cursor := ""
	for page := 0; page < 64; page++ {
		params := make(map[string]any)
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, _, err := c.request(ctx, "tools/list", c.methodParams(params))
		if err != nil {
			return nil, err
		}
		var response struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, fmt.Errorf("invalid tools/list result")
		}
		if len(tools)+len(response.Tools) > maxToolsPerServer {
			return nil, fmt.Errorf("server exposes more than %d tools", maxToolsPerServer)
		}
		tools = append(tools, response.Tools...)
		if response.NextCursor == "" {
			sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
			return tools, nil
		}
		if response.NextCursor == cursor {
			return nil, fmt.Errorf("tools/list repeated its cursor")
		}
		cursor = response.NextCursor
	}
	return nil, fmt.Errorf("tools/list pagination exceeded 64 pages")
}

func (c *client) callTool(ctx context.Context, name string, arguments json.RawMessage) (callResult, int64, error) {
	var decoded map[string]any
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return callResult{}, 0, err
	}
	raw, requestID, err := c.request(ctx, "tools/call", c.methodParams(map[string]any{"name": name, "arguments": decoded}))
	if err != nil {
		return callResult{}, requestID, err
	}
	var result callResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return callResult{}, requestID, fmt.Errorf("invalid tools/call result")
	}
	return result, requestID, nil
}

func (c *client) cancelRequest(requestID int64) {
	if requestID > 0 {
		_ = c.notify("notifications/cancelled", map[string]any{"requestId": requestID, "reason": "Reticle broker cancellation"})
	}
}

func (c *client) close() error {
	_ = c.stdin.Close()
	select {
	case <-c.done:
		return nil
	case <-time.After(2 * time.Second):
		if c.command.Process != nil {
			_ = c.command.Process.Kill()
		}
		return nil
	}
}

func rpcIDKey(raw json.RawMessage) string {
	var number int64
	if json.Unmarshal(raw, &number) == nil {
		return strconv.FormatInt(number, 10)
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func childEnvironment(mappings map[string]string) ([]string, []string) {
	allowed := map[string]bool{"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true, "LANG": true, "LC_ALL": true}
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, found := strings.Cut(item, "=")
		if found && allowed[strings.ToUpper(key)] {
			values[strings.ToUpper(key)] = value
		}
	}
	missing := make([]string, 0)
	for childName, hostName := range mappings {
		value, exists := os.LookupEnv(hostName)
		if !exists {
			missing = append(missing, hostName)
			continue
		}
		values[childName] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	sort.Strings(missing)
	return environment, missing
}

func bounded(value string, limits ...int) string {
	limit := 1024
	if len(limits) > 0 {
		limit = limits[0]
	}
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
