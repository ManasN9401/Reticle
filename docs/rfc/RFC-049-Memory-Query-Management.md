---
status: draft
owner: Reticle Project
updated: 2026-09-23
---

# RFC-049: Memory Query and Management Surface

## Motivation

Studio wants a Memory Browser: a panel where a user can see what's actually in the runtime's shared memory system, scoped by `global | workflow | execution | agent`, and edit or delete entries. Today `runtime/memory/manager.go` exposes only `Write` and `WriteBatch` plus a single-key, event-driven read (`MemoryReadRequested`) — there is no way to list or query "everything in scope X." `runtime/telemetry/server.go` has no memory REST route at all: its full handler table is `/`, `/artifacts/`, `/api/outputs/`, `/api/upload`, `/api/models`, `/api/models/toggle`, `/ws`. This RFC adds the missing read/query/delete surface and the REST endpoints Studio needs to reach it.

## Design

**Internal store restructure.** `RuntimeState`'s current store is `map[string]map[string]MemoryEntry]`, keyed by a synthetic concatenated `computeKey(scope, scopeID, key)`. Listing "everything in scope X" against that shape means prefix-decoding a string key, which is fragile. This RFC proposes restructuring to a scope-partitioned map — `map[MemoryScope]map[string]map[string]MemoryEntry]` — instead. This is the one piece of this RFC that touches a hot path: every task dispatch reads through `RuntimeState.Get` today, so the restructure needs real test coverage before merge, not just additive methods bolted on top of the existing shape.

**New methods.** `RuntimeState.List(scope, scopeID)` and `RuntimeState.Query(...)` on top of the restructured store. `Manager.List` and `Manager.Delete` wrap those, each publishing a bus event for consistency with the existing request/response pattern `MemoryReadRequested` already establishes: `MemoryListCompleted` and `MemoryEntryDeleted`.

**Event Taxonomy (additive).** `MemoryEntryDeleted` and `MemoryListCompleted` are new events. Per `docs/governance/RFC_PROCESS.md`, the Event Taxonomy is a Frozen Contract — even additive entries go through an RFC, which is what this document is for. Appended to `docs/specifications/event-taxonomy/v1/001-events.md` once accepted.

**REST surface.** `GET /api/memory?scope=&scopeId=` (list/query), `POST /api/memory` (write, respecting the existing `ExpectedVersion` optimistic-concurrency field on `MemoryEntry` — a write with a stale expected version is rejected, not silently overwritten), `DELETE /api/memory/{scope}/{scopeId}/{key}`.

**Trust boundary.** `docs/specifications/worker-protocol/v1/001-protocol.md` already states that "Worker memory writes are execution-scoped; higher-scope changes require a trusted runtime path." Studio's Electron main process, as the authenticated loopback control-server client, is a trusted runtime path in exactly the sense that sentence anticipates — this RFC treats Studio-initiated writes at any scope as legitimate through this REST surface, gated by the same `ExpectedVersion` mechanic a worker's own writes already respect.

**Studio surface.** `MemoryBrowser.tsx` — scope tree, then key list, then a value editor. A version conflict on write is surfaced as an explicit retry-or-overwrite choice, never silently dropped or silently forced through. New `bridge.memory.{list, write, remove}` methods.

## Drawbacks

Exposing global- and workflow-scope memory writes from a UI increases the blast radius of an operator mistake — a bad edit at global scope can affect every concurrent execution reading that key, not just one run. `ExpectedVersion` enforcement bounds this to "your stale write is rejected," not "your write is invisible to others," but it does not prevent a deliberate, considered edit from having a wide effect — that is the feature working as intended, not a gap. The internal store restructure is a non-trivial change to a hot path and is the part of this RFC most likely to need iteration during implementation.
