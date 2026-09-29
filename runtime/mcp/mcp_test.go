package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/toolbroker"
)

func TestRegistryPersistsEnvironmentNamesNotValues(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".reticle", "mcp", "servers.json")
	registry, err := NewRegistry(root, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RETICLE_TEST_SECRET", "never-persist-this-value")
	_, err = registry.Put(ServerConfig{
		ID: "fixture", Command: "fixture-command", Transport: "stdio", Enabled: false,
		Env: map[string]string{"TOKEN": "RETICLE_TEST_SECRET"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("never-persist-this-value")) || !bytes.Contains(data, []byte("RETICLE_TEST_SECRET")) {
		t.Fatalf("registry persisted the wrong environment data: %s", data)
	}
	reloaded, err := NewRegistry(root, path)
	if err != nil {
		t.Fatal(err)
	}
	config, exists, _ := reloaded.Get("fixture")
	if !exists || config.Env["TOKEN"] != "RETICLE_TEST_SECRET" {
		t.Fatalf("restart lost environment mapping: %#v", config)
	}
}

func TestManagerDiscoversAndBrokersLegacyServerWithTwoGrants(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RETICLE_MCP_FIXTURE_HOST", "1")
	registry, err := NewRegistry(root, filepath.Join(root, ".reticle", "mcp", "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Put(ServerConfig{
		ID: "fixture", Command: os.Args[0], Args: []string{"-test.run=TestMCPFixtureProcess"},
		Env:     map[string]string{"RETICLE_MCP_FIXTURE_CHILD": "RETICLE_MCP_FIXTURE_HOST"},
		Enabled: true, Transport: "stdio", StartupTimeoutSeconds: 5, CallTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus("mcp-test")
	broker, err := toolbroker.New(bus)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(registry, broker, bus, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = broker.Close(ctx)
		bus.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := manager.Refresh(ctx, "fixture"); err != nil {
		t.Fatal(err)
	}
	descriptors := manager.AvailableDescriptors()
	if len(descriptors) != 1 || descriptors[0].Name != "mcp__fixture__echo" {
		t.Fatalf("unexpected descriptors: %#v", descriptors)
	}

	capabilityOnly, _ := broker.BeginAttemptWithPolicies("cap-only", "exec", "task", []string{"mcp.call"}, nil)
	policyOnly, _ := broker.BeginAttemptWithPolicies("policy-only", "exec", "task", nil, []string{"mcp.server:fixture"})
	allowed, _ := broker.BeginAttemptWithPolicies("allowed", "exec", "task", []string{"mcp.call"}, []string{"mcp.server:fixture"})
	for _, denied := range []toolbroker.Credentials{capabilityOnly, policyOnly} {
		status, body := brokerRequest(t, denied, http.MethodGet, "/v1/tools", nil)
		if status != http.StatusOK || bytes.Contains(body, []byte("mcp__fixture__echo")) {
			t.Fatalf("single grant exposed MCP tool: %d %s", status, body)
		}
	}
	status, body := brokerRequest(t, allowed, http.MethodPost, "/v1/calls", map[string]any{
		"callId": "echo-1", "tool": descriptors[0].ID, "arguments": map[string]any{"value": "hello"},
	})
	if status != http.StatusOK || !bytes.Contains(body, []byte("hello")) {
		t.Fatalf("brokered MCP call failed: %d %s", status, body)
	}
	worker := exec.Command(os.Args[0], "-test.run=TestMCPWorkerFixtureProcess")
	worker.Env = append(os.Environ(), append(allowed.Environment(),
		"RETICLE_MCP_WORKER_CHILD=1", "RETICLE_MCP_TOOL_ID="+descriptors[0].ID,
	)...)
	workerOutput, err := worker.CombinedOutput()
	if err != nil || !bytes.Contains(workerOutput, []byte("worker-value")) {
		t.Fatalf("worker-to-MCP fixture call failed: %v %s", err, workerOutput)
	}
}

func TestClientCorrelatesOutOfOrderAndRejectsMalformedOrOversizedMessages(t *testing.T) {
	for _, mode := range []string{"out-of-order", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("RETICLE_MCP_FIXTURE_HOST", "1")
			t.Setenv("RETICLE_MCP_MODE_HOST", mode)
			config := ServerConfig{
				ID: "fixture", Command: os.Args[0], Args: []string{"-test.run=TestMCPFixtureProcess"},
				Env: map[string]string{
					"RETICLE_MCP_FIXTURE_CHILD": "RETICLE_MCP_FIXTURE_HOST",
					"RETICLE_MCP_FIXTURE_MODE":  "RETICLE_MCP_MODE_HOST",
				},
				Enabled: true, Transport: "stdio", StartupTimeoutSeconds: 5, CallTimeoutSeconds: 5,
			}
			environment, missing := childEnvironment(config.Env)
			if len(missing) != 0 {
				t.Fatalf("missing environment: %v", missing)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client, err := startClient(ctx, config, environment, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.close()
			if mode != "out-of-order" {
				if _, err := client.listTools(ctx); err == nil {
					t.Fatalf("%s response was accepted", mode)
				}
				return
			}
			type outcome struct {
				method string
				raw    json.RawMessage
				err    error
			}
			results := make(chan outcome, 2)
			for _, method := range []string{"fixture/one", "fixture/two"} {
				go func(method string) {
					raw, _, err := client.request(ctx, method, nil)
					results <- outcome{method: method, raw: raw, err: err}
				}(method)
			}
			seen := map[string]bool{}
			for i := 0; i < 2; i++ {
				result := <-results
				if result.err != nil || !bytes.Contains(result.raw, []byte(result.method)) {
					t.Fatalf("mis-correlated %s: %s %v", result.method, result.raw, result.err)
				}
				seen[result.method] = true
			}
			if len(seen) != 2 {
				t.Fatalf("missing responses: %v", seen)
			}
		})
	}
}

func TestClientUsesNamespacedMetadataForModernProtocol(t *testing.T) {
	t.Setenv("RETICLE_MCP_FIXTURE_HOST", "1")
	t.Setenv("RETICLE_MCP_MODE_HOST", "modern")
	config := ServerConfig{
		ID: "modern", Command: os.Args[0], Args: []string{"-test.run=TestMCPFixtureProcess"},
		Env:     map[string]string{"RETICLE_MCP_FIXTURE_CHILD": "RETICLE_MCP_FIXTURE_HOST", "RETICLE_MCP_FIXTURE_MODE": "RETICLE_MCP_MODE_HOST"},
		Enabled: true, Transport: "stdio", StartupTimeoutSeconds: 5, CallTimeoutSeconds: 5,
	}
	environment, _ := childEnvironment(config.Env)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := startClient(ctx, config, environment, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.close()
	tools, err := client.listTools(ctx)
	if err != nil || client.protocol != ModernProtocolVersion || len(tools) != 1 {
		t.Fatalf("modern negotiation failed: %v protocol=%s tools=%#v", err, client.protocol, tools)
	}
}

func brokerRequest(t *testing.T, credentials toolbroker.Credentials, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, credentials.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credentials.Token)
	request.Header.Set("X-Reticle-Attempt-ID", credentials.AttemptID)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, data
}

func TestMCPFixtureProcess(t *testing.T) {
	if os.Getenv("RETICLE_MCP_FIXTURE_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	mode := os.Getenv("RETICLE_MCP_FIXTURE_MODE")
	var held []map[string]any
	for scanner.Scan() {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		if len(request.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(request.ID)}
		switch request.Method {
		case "server/discover":
			if mode == "modern" {
				var params map[string]any
				_ = json.Unmarshal(request.Params, &params)
				meta, _ := params["_meta"].(map[string]any)
				if meta["io.modelcontextprotocol/protocolVersion"] != ModernProtocolVersion {
					response["error"] = map[string]any{"code": -32602, "message": "missing discovery metadata"}
				} else {
					response["result"] = map[string]any{"protocolVersions": []string{ModernProtocolVersion}, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "modern-fixture", "version": "1.0.0"}}
				}
			} else {
				response["error"] = map[string]any{"code": -32601, "message": "legacy server"}
			}
		case "initialize":
			response["result"] = map[string]any{"protocolVersion": LegacyProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "fixture", "version": "1.0.0"}}
		case "tools/list":
			if mode == "modern" {
				var params map[string]any
				_ = json.Unmarshal(request.Params, &params)
				meta, _ := params["_meta"].(map[string]any)
				if meta["io.modelcontextprotocol/protocolVersion"] != ModernProtocolVersion || meta["io.modelcontextprotocol/clientCapabilities"] == nil {
					response["error"] = map[string]any{"code": -32602, "message": "missing modern metadata"}
					break
				}
			}
			if mode == "malformed" {
				fmt.Fprintln(os.Stdout, "not-json")
				continue
			}
			if mode == "oversized" {
				response["result"] = map[string]any{"tools": []any{}, "padding": string(bytes.Repeat([]byte{'x'}, maxMessageBytes+1))}
				if encoder.Encode(response) != nil {
					os.Exit(3)
				}
				continue
			}
			response["result"] = map[string]any{
				"tools": []any{map[string]any{
					"name": "echo", "description": "Echo input",
					"inputSchema": map[string]any{
						"type": "object", "properties": map[string]any{
							"value": map[string]string{"type": "string"},
						}, "required": []string{"value"},
					},
				}},
			}
		case "tools/call":
			var params struct {
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(request.Params, &params)
			response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint(params.Arguments["value"])}}}
		case "fixture/one", "fixture/two":
			if mode == "out-of-order" {
				held = append(held, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(request.ID), "result": map[string]string{"method": request.Method}})
				if len(held) == 2 {
					_ = encoder.Encode(held[1])
					_ = encoder.Encode(held[0])
					held = nil
				}
				continue
			}
			response["error"] = map[string]any{"code": -32601, "message": "unknown method"}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "unknown method"}
		}
		if encoder.Encode(response) != nil {
			os.Exit(3)
		}
	}
	os.Exit(0)
}

func TestMCPWorkerFixtureProcess(t *testing.T) {
	if os.Getenv("RETICLE_MCP_WORKER_CHILD") != "1" {
		return
	}
	credentials := toolbroker.Credentials{
		URL: os.Getenv("RETICLE_TOOL_BROKER_URL"), Token: os.Getenv("RETICLE_TOOL_BROKER_TOKEN"),
		AttemptID: os.Getenv("RETICLE_ATTEMPT_ID"),
	}
	status, body := brokerRequest(t, credentials, http.MethodPost, "/v1/calls", map[string]any{
		"callId": "worker-call", "tool": os.Getenv("RETICLE_MCP_TOOL_ID"),
		"arguments": map[string]any{"value": "worker-value"},
	})
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	fmt.Println(string(body))
}
