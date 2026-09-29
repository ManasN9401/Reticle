---
status: active
owner: Reticle Project
updated: 2026-09-29
---

# Project State — 27 September 2026

## Current phase

Reticle has completed the first local extensibility slice. The bounded DAG runtime, durable
attempt model, capability-aware dispatch, Forge compiler, Studio control surface
and local-model execution path exist and have working verification coverage.
The dated July foundation snapshot remains historical planning context rather
than the current implementation state.

## Current objective

Harden the implemented MCP and plugin paths without creating a second invocation,
permission or cancellation path, then review RFC-049 as the next independent UI/runtime surface.

## Agreed implementation sequence

1. **RFC-050 — Attempt-Scoped Tool Broker.** Accepted and implemented: loopback
   per-attempt access, immutable descriptors, capability filtering,
   cancellation, limits, effect certainty, telemetry and a real worker fixture.
2. **RFC-046 — MCP Integration.** Accepted and implemented for explicitly
   registered local stdio servers, brokered tools, authenticated APIs and Studio.
3. **RFC-047 — Plugin System.** Accepted and implemented for validated local
   bundles contributing agents, skills, broker tools and MCP declarations.
4. **Studio integration.** Implemented as a client of runtime registry state;
   attempt credentials and resolved secret values remain outside the renderer.

## Follow-up decisions

- Whether the broker protocol should remain private HTTP/JSON or become a
  versioned local RPC contract after the first implementation.
- Which built-in Python tools migrate through the broker first; migration is not
  required to prove MCP, but shared metadata must be used from the start.
- The plugin signing, provenance and sandboxing policy. Until then, installed
  plugin code has the user's OS authority and must be labelled trusted-only.
- Whether remote MCP transports and OAuth belong in a follow-up RFC; they remain
  explicitly outside RFC-046 v1.

## Definition of progress

The next milestone is a hardening pass: broader malformed/protocol-version MCP
fixtures, plugin provenance and upgrade recovery, plus an end-to-end Studio
acceptance run. Remote transports, a marketplace and autonomous plugin writing
remain out of scope until their security contracts are separately accepted.
