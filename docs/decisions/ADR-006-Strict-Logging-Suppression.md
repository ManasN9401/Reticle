# ADR-006: Strict Logging Suppression in Worker Subprocesses

## Status
Accepted

## Context
During high-load concurrent agent execution, APIs (Groq, Gemini, OpenRouter) frequently returned `429 Too Many Requests`. The Python workers use `tenacity` for exponential backoff, which implicitly relied on `litellm`'s native logging. Every rate limit failure triggered a massive JSON stack trace in `stderr`. The Go orchestrator captured these `stderr` streams and flooded the WebSocket telemetry event bus, causing severe frame drops and crashes in the Web UI.

## Decision
We decided to strictly suppress all non-fatal logs emitted by Python workers. We enforce:
```python
logging.basicConfig(level=logging.ERROR)
```
at the root level of all `compiler/workers/*.py` scripts.

## Consequences
- **Positive:** The WebSocket bus remains completely silent and clean during rate-limit retry loops, preserving frontend UI performance.
- **Negative:** We lose visibility into the "silent" retry loops. The user might perceive the system as "hanging" if an agent is stuck in a 40-attempt exponential backoff loop without any logs indicating *why* it's taking so long. Future work may involve adding a custom `TelemetryProgress` ping back to the Go orchestrator during backoff cycles.
