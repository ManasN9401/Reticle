---
status: draft
owner: Reticle Project
updated: 2026-09-23
---

# RFC-046: MCP Integration

## Motivation

`docs/planning/ARCITECTURAL_BACKLOG.md` records that the worker stdio JSON-RPC protocol was deliberately kept generic so that "any agent, whether a simple python script or a massive LLM acting through an MCP server, can be seamlessly plugged into the runtime." No MCP client or server code exists anywhere in `runtime/` or `cmd/forge/` today; `docs/ROADMAP.md`'s RFC-014 entry is a placeholder that was never filed. This RFC proposes the concrete design that placeholder described: discovery, registration, permissions, context sharing, tool exposure and runtime management for Model Context Protocol servers.

## Design

**Transport.** Stdio only for v1, matching the existing worker-process integration seam in `runtime/agent/worker.go`. A `transport` field is reserved on the server-config shape so Streamable HTTP/SSE can be added later without a breaking change; this is a stated deferral, not an omission.

**Discovery.** Server configuration is Studio/REST-driven, not filesystem auto-discovery. A server only becomes reachable when explicitly registered through the API below — an explicit trust boundary rather than an implicit one.

**Permissions.** Discovered MCP tools are surfaced as `Capability`-gated entries so `AgentDefinition.Capabilities` (`runtime/agent/registry.go`) remains the single permission gate a worker's tool exposure goes through. This avoids a second, parallel permission system. Each MCP server's tools are added to the same `tool_capabilities` dispatch table `cmd/forge/compiler/lib/worker_sdk.py` already uses to gate its own built-in tools, filtered by the task's `capabilities` set.

**Agent Definition Schema (additive).** A new optional `mcp_servers: []string` field on `AgentDefinition`, naming which registered servers an agent may reach. Documented in `docs/specifications/agent-definition/v1/001-schema.md` as non-breaking — existing manifests without the field are unaffected, matching the same v1 compatibility grant already given to `capabilities`.

**Event Taxonomy (additive).** Four new events: `McpServerConnected`, `McpServerDisconnected`, `McpServerError`, `McpToolInvoked`. Appended to `docs/specifications/event-taxonomy/v1/001-events.md` once this RFC is accepted.

**Backend.**
- `runtime/mcp/client.go` — stdio JSON-RPC client (`initialize`, `tools/list`, `tools/call`), modeled on `runtime/agent/worker.go`'s subprocess discipline: same timeout handling, same kill-on-shutdown behaviour.
- `runtime/mcp/registry.go` — server configuration store (id, command, args, env, enabled) and a discovered-tools cache, persisted at `.reticle/mcp/servers.json` — kept separate from `.reticle/memory/state.json` so MCP configuration isn't bound to any one execution's memory snapshot.
- `runtime/mcp/lifecycle.go` — start/stop/restart/test-connection, publishing the four events above.
- Lazy start: an enabled server's subprocess is spawned on first tool use, not the moment it is enabled, to avoid idle subprocess sprawl when several servers are configured but rarely exercised.

**Studio surface.** New REST endpoints on `runtime/telemetry/server.go`: `GET/POST /api/mcp/servers`, `PUT/DELETE /api/mcp/servers/{id}`, `POST /api/mcp/servers/{id}/enable`, `POST .../disable`, `POST .../test`, `GET .../tools`. The runtime is the source of truth — Studio's `McpServersSection.tsx` is a CRUD UI against these endpoints, not a locally persisted list, so other clients (and the CLI) observe the same server state.

## Drawbacks

Each enabled server is a long-lived subprocess for the life of `forge.exe`; lazy start mitigates idle sprawl but a server that misbehaves on `tools/call` still consumes a task's attempt budget like any other failure. Remote-server credential handling is out of scope for v1 — every configured server runs as a local subprocess Reticle itself spawns. No HTTP/SSE transport in v1. MCP server processes are not sandboxed beyond the OS process boundary already applied to worker subprocesses; a compromised MCP server has the same reach as a compromised worker.
