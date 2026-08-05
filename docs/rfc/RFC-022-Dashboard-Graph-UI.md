# RFC-022 — Dashboard & Graph UI

Status: Draft
Version: 1.0.0
Author: HyperParallel Core
Last Updated: 2026-08-04

---

## 1. Purpose
This document specifies the architecture and technical design of the HyperParallel Telemetry Dashboard, responsible for rendering real-time execution graphs and providing observability into the core orchestrator.

## 2. Motivation
Due to the massive concurrency and complex, non-linear dependencies (DAGs) inherent to HyperParallel, developers require a unified visual interface to trace task propagation, inspect standard output, and monitor node states in real-time. A purely CLI-based output is insufficient for debugging large-scale parallel agent workflows.

## 3. Scope
This RFC covers:
- The embedded Telemetry HTTP Server (Go `//go:embed`).
- The WebSocket Streaming protocol for broadcasting `RuntimeEvents`.
- The Frontend topological layout logic (Canvas-based Star Map).
- Frontend interactivity requirements (Pan, Zoom, Resize).

## 4. Philosophy
The UI must be ephemeral and completely decoupled from the runtime orchestration loop. The failure, slow execution, or total absence of the Telemetry UI must never block or degrade the primary Orchestrator loop.

## 5. Principles
- **Self-Contained Deployment**: The entire UI must be baked into the Go executable via `//go:embed`, removing the need to manage separate frontend hosting or file distributions.
- **Push-Only Observability**: The orchestrator pushes state via a WebSocket. The frontend reacts. The frontend does not send commands back to the orchestrator (in v1).
- **Responsive Layout**: The visual graph must handle arbitrary numbers of crossing edges and scale infinitely via panning and zooming.

## 6. Architectural Laws
1. The Telemetry Server must bind to the internal `events.Bus` as a passive subscriber.
2. The UI files must be served from memory using Go's `embed.FS`.
3. The Canvas frontend must automatically compute node depth and apply topological sorting (alphabetical fallbacks) to minimize edge crossover chaos.
4. The frontend must implement physics-based panning, zooming, and robust panel resizing.

## 7. Rationale
Embedding the UI directly into the binary ensures a zero-friction developer experience. A single `hyperparallel.exe -gui=true` command spins up the backend and the observability frontend seamlessly. A WebSocket ensures sub-millisecond latency for real-time task updates.

## 8. Trade-offs
- Using a raw HTML5 `<canvas>` instead of a DOM-based framework like React/D3 requires manual handling of render loops, transformations, and coordinate spaces, but provides 60fps performance even with hundreds of nodes.
- Embedding the UI means that any CSS/JS change requires a full binary recompilation.

## 9. Future Considerations
- Introducing bidirectional WebSocket communication, allowing the UI to trigger manual overrides, pause executions, or manually inject Human-in-the-loop responses (RFC-021).
- Time-travel debugging: scrubbing the event timeline to replay the graph execution.

## 10. References
- Initial implementation validated in `03_startup_launch` stress test.

## 11. Related RFCs
- RFC-003 — Runtime
- RFC-004 — Event Bus
