# ADR-005: Bifurcated Architecture (Go Orchestrator + Python Workers)

## Status
Accepted

## Context
Reticle requires an orchestration layer capable of managing highly concurrent, multi-agent workflows while simultaneously streaming execution state to a rich UI. At the same time, the individual agents require access to state-of-the-art LLM libraries, web scrapers, and AI-native tooling.

## Decision
We decided to split the architecture into two distinct languages:
1. **The Orchestrator (`forge.exe` in Go)**: Handles the event bus, workspace file routing, concurrency, WebSockets, and telemetry parsing. Go provides superior lightweight goroutines for managing hundreds of events without blocking.
2. **The Workers (`compiler/workers/*.py` in Python)**: Handles the actual AI logic. Python is the lingua franca of AI; by using Python, we have native access to `litellm` (for seamless vendor switching), `tenacity` (for backoff), and specialized prompt engineering tools.

## Consequences
- **Positive:** We get the best of both worlds—Go's concurrency for the backend server and Python's AI ecosystem for the agents.
- **Negative:** We must maintain a robust inter-process communication protocol (via `stdin/stdout` and the JSON event bus) instead of simple function calls, increasing debugging complexity.
