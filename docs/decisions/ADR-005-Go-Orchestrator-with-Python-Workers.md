---
status: accepted
owner: Reticle Project
updated: 2026-09-19
---

# ADR-005: Bifurcated Architecture (Go Orchestrator + Python Workers)

## Status
Accepted

## Context
Reticle requires an orchestration layer capable of managing highly concurrent, multi-agent workflows while simultaneously streaming execution state to a rich UI. At the same time, the individual agents require access to state-of-the-art LLM libraries, web scrapers, and AI-native tooling.

## Decision
We decided to split the architecture into two distinct languages:
1. **The Orchestrator (`forge.exe` in Go)**: Handles the event bus, workspace file routing, concurrency, WebSockets, and telemetry parsing. Go provides superior lightweight goroutines for managing hundreds of events without blocking.
2. **The workers (`cmd/forge/compiler/agents/*/workers/*.py`)**: Handle model and specialist logic through the shared SDK and Python ecosystem. LiteLLM provides provider access. Provider-library retries are disabled; the Go dispatcher owns bounded, effect-aware retry.

## Consequences
- **Positive:** We get the best of both worlds—Go's concurrency for the backend server and Python's AI ecosystem for the agents.
- **Negative:** We must maintain a robust inter-process communication protocol (via `stdin/stdout` and the JSON event bus) instead of simple function calls, increasing debugging complexity.
