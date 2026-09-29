---
status: accepted
owner: Reticle Project
updated: 2026-09-29
---

# RFC-050: Attempt-Scoped Tool Broker

## 1. Purpose

Define one runtime-owned extension seam for built-in, MCP and future plugin tools without weakening Worker Protocol v1, duplicating permissions or giving workers durable control credentials.

## 2. Motivation

Current SDK tools execute inside Python workers. MCP requires a Go-owned process to receive calls while a worker is running. Worker Protocol v1 cannot carry those calls: stdin is one task plus EOF, stdout is one final response and stderr is telemetry. The broker is a separate attempt-scoped side channel, leaving final-result framing unchanged.

## 3. Architectural laws

1. The broker is internal and loopback-only.
2. Credentials are random, bound to one active attempt and revoked at terminal state.
3. Effective tools are the intersection of manifest, task capability grant, adapter state and runtime policy.
4. Models receive schemas, never credentials or secrets.
5. Calls cannot add capabilities, servers or permissions.
6. Cancellation and late-result rejection use RFC-045 attempt identity.
7. Built-in and external tools share naming, limits, telemetry and admission semantics.

## 4. Side-channel protocol

Each attempt receives `RETICLE_TOOL_BROKER_URL`, `RETICLE_TOOL_BROKER_TOKEN` and `RETICLE_ATTEMPT_ID` in its process environment. Tokens never enter task JSON, memory, logs, artifacts or model messages. The broker rejects non-loopback traffic, invalid credentials, attempt mismatch and terminal attempts.

V1 exposes `GET /v1/tools`, `POST /v1/calls` with `{callId, tool, arguments}`, and `POST /v1/calls/{callId}/cancel`. Responses are bounded JSON with `ok`, structured result/error and effect certainty: `no_effect`, `effect_started` or `uncertain`. Duplicate call IDs return a safely committed prior response or conflict while active.

The SDK fetches descriptors before its first model request, converts bounded JSON Schemas to provider tools and maps calls to canonical IDs. Authentication stays outside model-visible context.

## 5. Registry and adapters

Descriptors contain canonical ID, model-safe name, bounded description/schema, required capability, adapter identity, timeout, output limit, effect classification and availability. Adapters implement list, call, cancel and shutdown. MCP is the first external adapter; plugins must reuse this registry.

Built-in tools may remain Python-local during migration, but use the same metadata model. Names are collision-checked. Invalid schemas or collisions disable a descriptor visibly and never shadow built-ins.

## 6. Capability resolution and lifecycle

The broker resolves each attempt's task grant once; adapter allowlists narrow it further. Pause blocks new calls. Kill/cancel revokes the token immediately and requests adapter cancellation. Tokens cannot cross attempts, retries or executions.

Request, schema, argument, result and diagnostic sizes are independently bounded. Deadlines sit below the task deadline. Global, per-adapter and per-attempt concurrency are bounded. Only `no_effect` is generically retry-safe; other outcomes require adapter idempotency/reconciliation and cannot be inferred safe from transport status.

Credentials and configured secrets are always redacted. Arguments/results are absent from ordinary telemetry unless an adapter defines a separate bounded domain event.

## 7. Compatibility and observability

Worker Protocol v1 framing is unchanged. The environment variables and SDK behavior are additive. Upon acceptance, authoritative worker/agent specifications and the Event Taxonomy are updated. Broker events contain execution, task, attempt, call, tool, adapter, duration, outcome and effect certainty—never credentials, arguments or results.

Studio may observe activity but receives no broker credential. Operator testing uses adapter-owned authenticated control endpoints.

## 8. Acceptance criteria

Tests cover token isolation and expiry, pause/cancel, duplicate calls, late results, capability filtering, collisions, malformed schemas, limits, deadlines, concurrency, redaction and effect certainty. One integration test must invoke a brokered fixture tool from a real worker while preserving its single final stdout response.

## 9. Drawbacks

The broker adds a local server, credentials and another failure boundary. It does not sandbox adapters or make effects retryable. During migration, one metadata model temporarily covers both Python-local and runtime-brokered execution paths.

## 10. Related documents

RFC-045, RFC-046, RFC-047, Worker Protocol v1 and Agent Definition v1.

## 11. Implementation

Implemented on 29 September 2026 in `runtime/toolbroker`, the dispatcher and
`cmd/forge/compiler/lib/worker_sdk.py`. Forge starts one loopback broker; each
concrete retry receives a random credential through its child-process
environment and loses access before its terminal attempt event is published.

The implementation includes immutable descriptor registration, model-name and
schema admission, capability-filtered discovery, bounded requests/results,
global, adapter and attempt concurrency limits, deadlines, pause/revoke/cancel,
duplicate call handling, late-result rejection, effect certainty and redacted
telemetry. Built-in Python tools remain local but use the same descriptor shape.
Contract tests include a real spawned worker calling a brokered fixture while
preserving the single-response stdout protocol.
