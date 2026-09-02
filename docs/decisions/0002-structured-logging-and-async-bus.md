---
title: 0002 - Structured Logging and Asynchronous Event Bus
document_type: ADR
authority: Informative
status: Accepted
version: 1.0.0
scope: Runtime
stability: Stable
owner: Reticle Project
---

# 0002 - Structured Logging and Asynchronous Event Bus

## Context
As the Skeleton Runtime transitions towards managing highly concurrent, parallel agents, debugging via standard `fmt.Printf` strings and blocking on synchronous event emissions becomes unviable. We needed a production-grade observability foundation before attempting complex Shared Memory upgrades or dynamic DAG parsing.

## Decision
1. **Logging**: Upgraded to Go 1.21's native `log/slog` for structured JSON logging.
2. **Event Bus**: Upgraded the `events.Bus` to use a buffered Go channel (`make(chan Event, 1000)`) and a dedicated dispatcher goroutine.

## Consequences
- **Positive:** Agent execution is no longer blocked by event subscribers. All runtime events are now automatically audited and funneled into structured JSON files (`logs/runtime.log`) via a wildcard event subscriber.
- **Negative:** Increased complexity in the event bus (managing goroutines and channels) and slightly more verbose logging syntax.
