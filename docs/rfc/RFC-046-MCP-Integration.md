---
status: draft
owner: Reticle Project
updated: 2026-09-27
---

# RFC-046: MCP Integration

## 1. Purpose

Define bounded Model Context Protocol support: explicitly registered local stdio servers, supervised by Go, exposed to workers through RFC-050's attempt-scoped tool broker, and managed from Studio through the authenticated control service.

## 2. Motivation

Reticle has capability-filtered SDK tools, durable attempt identities and an authoritative architect catalogue, but no MCP client or lifecycle. The earlier draft assigned MCP process ownership to Go while saying Python workers would add discovered tools locally. It never defined how a running one-shot worker could call Go without violating Worker Protocol v1, whose stdout is reserved for one final result. RFC-050 closes that gap: MCP is a broker adapter, not a second worker protocol or a Python-owned subprocess tree.

## 3. Scope

Version 1 includes local stdio JSON-RPC servers; explicit CRUD and enable/disable; `initialize`, `notifications/initialized`, `tools/list` and `tools/call`; lazy start, health testing, bounded restart and shutdown; persistent non-secret configuration; per-attempt capability filtering; telemetry, authenticated REST management, Studio settings and a deterministic fixture-server integration test.

It excludes Streamable HTTP/SSE, remote servers, OAuth, marketplace or filesystem discovery, prompts/resources and plugin installation. These are deliberate exclusions, not implied partial support.

## 4. Dependencies

RFC-050 must be accepted and implemented first. RFC-045 remains authoritative for attempt identity, capabilities, cancellation and effects. RFC-047 may consume MCP declarations only after this RFC is implemented.

## 5. Server configuration

The runtime stores `.reticle/mcp/servers.json` separately from execution memory. A record has `id`, `command`, `args`, optional `cwd`, `env`, `enabled`, `transport`, `startupTimeoutSeconds` and `callTimeoutSeconds`. `transport` is exactly `stdio` in v1. `cwd`, when present, must resolve inside the configured checkout.

`env` maps child variable names to existing host environment-variable names. Plaintext secret values are forbidden in persistence and REST responses. Variables are resolved only at spawn; a missing variable fails the connection test without disclosing values.

Registration is explicit through the authenticated API. Reticle never executes a server merely because a package or file exists. Changing process fields restarts a running server after validation.

## 6. Runtime components and lifecycle

- `runtime/mcp/client.go`: bounded newline-delimited stdio JSON-RPC, monotonic IDs, negotiation and response correlation.
- `runtime/mcp/registry.go`: validated persistence, duplicate rejection and immutable snapshots.
- `runtime/mcp/lifecycle.go`: start, initialize, refresh, cancel, restart and shutdown ownership.
- `runtime/mcp/adapter.go`: RFC-050 adapter translating broker calls to `tools/call`.

An enabled server starts lazily on test, discovery or first admitted call. V1 serializes calls per server unless concurrency has been explicitly validated. Unexpected exit marks the server unhealthy and fails in-flight calls. One automatic restart is allowed only before a call has been transmitted. After transmission, failure is uncertain unless an adapter-specific idempotency contract proves otherwise; Reticle must not blindly replay it.

## 7. Permission and exposure model

Agent definitions gain optional `mcp_servers: []string` and the static capability `mcp.call`. A tool is exposed only when the capability is granted, the server is allowlisted, the server is enabled and discovered, and the RFC-050 attempt credential is valid. Either grant alone exposes nothing, and a model cannot add either grant.

Model-safe names use `mcp__<server-id>__<tool-name>` after deterministic normalization. Original identity is retained internally; collisions are rejected. Descriptions and schemas are untrusted server data, bounded and delimited before model use.

The architect catalogue includes successfully discovered tools with their required capability and server allowlist. Generated definitions therefore remain explicit and least-privilege.

## 8. Calls, limits and cancellation

Workers fetch schemas and call MCP tools only through RFC-050; they do not spawn servers or receive server credentials. The broker revalidates every call against the attempt's immutable effective tool set.

Inputs, outputs and diagnostics are independently capped. Text is bounded UTF-8. Supported oversized/binary output becomes an artifact reference; otherwise the call fails explicitly. Server stderr is redacted diagnostic data, never JSON-RPC.

Task cancellation revokes broker access and requests MCP cancellation where supported. Late results from cancelled or superseded attempts are discarded. Calls are not generically retry-safe: only a proven pre-transmission failure is `no_effect`; post-transmission failure is `uncertain` unless the adapter proves idempotency.

## 9. Events and observability

Upon acceptance, the Event Taxonomy gains `McpServerConnected`, `McpServerDisconnected`, `McpServerError`, `McpToolsChanged` and `McpToolInvoked`. Events contain bounded server status. Tool events add execution, task, attempt, broker call, normalized tool, duration and outcome. Arguments, results, environment values and credentials are excluded by default.

## 10. Control API and Studio

The authenticated loopback service provides `GET/POST /api/mcp/servers`, `PUT/DELETE /api/mcp/servers/{id}`, enable/disable, test and tools endpoints. Studio's MCP Servers page is a client of this runtime source of truth. It displays lifecycle, last error, discovered tools and missing variable names, but never values.

## 11. Acceptance criteria

Tests must prove secret-free restart persistence; safe handling of malformed, oversized and out-of-order messages; capability and allowlist denial; credential revocation and late-result rejection; no blind replay of uncertain calls; shared Studio/REST state; a real worker-to-fixture-server call; and architect-catalogue visibility only for enabled, successfully discovered tools.

## 12. Drawbacks

MCP server code runs with the Reticle user's OS authority; stdio is not a sandbox. Long-lived processes add lifecycle cost, dynamic schemas consume context, serialized calls limit throughput, and uncertain effects constrain retries. Explicit registration and capabilities reduce exposure but do not make untrusted code safe.

## 13. Related documents

RFC-045, RFC-047, RFC-050, Worker Protocol v1, Agent Definition v1 and Event Taxonomy v1.
