---
status: active
owner: Reticle Project
updated: 2026-09-27
---

# Project State — 27 September 2026

## Current phase

Reticle is entering the extensibility phase. The bounded DAG runtime, durable
attempt model, capability-aware dispatch, Forge compiler, Studio control surface
and local-model execution path exist and have working verification coverage.
The dated July foundation snapshot remains historical planning context rather
than the current implementation state.

## Current objective

Establish one secure runtime path for dynamically supplied tools before adding
MCP servers or loadable plugins. This avoids creating incompatible invocation,
permission and cancellation mechanisms in three subsystems.

## Agreed implementation sequence

1. **RFC-050 — Attempt-Scoped Tool Broker.** Add the loopback, per-attempt side
   channel, immutable descriptors, capability filtering, cancellation, limits,
   effect certainty and telemetry while preserving Worker Protocol v1.
2. **RFC-046 — MCP Integration.** Implement explicit local stdio server
   registration and lifecycle as the first external broker adapter, followed by
   authenticated control APIs and Studio management.
3. **RFC-047 — Plugin System.** Version the reserved manifest, validate complete
   bundles and atomically register contributed agents, skills and broker tools.
   MCP declarations become available only when RFC-046 is present.
4. **Studio integration.** Treat runtime registries as the source of truth for
   tool, MCP and plugin status; expose configuration without distributing
   attempt credentials or secret values to the UI.

## Decisions still required during acceptance

- Whether the broker protocol should remain private HTTP/JSON or become a
  versioned local RPC contract after the first implementation.
- Which built-in Python tools migrate through the broker first; migration is not
  required to prove MCP, but shared metadata must be used from the start.
- The plugin signing, provenance and sandboxing policy. Until then, installed
  plugin code has the user's OS authority and must be labelled trusted-only.
- Whether remote MCP transports and OAuth belong in a follow-up RFC; they are
  explicitly outside RFC-046 v1.

## Definition of progress

The next milestone is not a settings-only prototype. It requires a real worker
to discover and invoke a deterministic brokered fixture tool while retaining a
single final stdout result, with tests proving token isolation, capability
denial, cancellation, late-result rejection, output limits and redaction.
