# RFC-029: Python Tenacity Retry Policy & Logging Suppression

## Status
Accepted

## Context
When an API key returns a Rate Limit Exception, the agent script should not instantly crash. It should retry with an exponential backoff. However, using the default `Tenacity` retry wrappers combined with `litellm` caused a catastrophic edge case: `litellm` and `tenacity` outputted massive `WARNING` JSON payloads to `stderr` for *every* failed attempt. When 4 agents run in parallel and fail 10 times each, 40 massive JSON stack traces are piped into the Go `forge.exe` orchestrator, which then broadcasts them over WebSockets to the Telemetry UI. This causes massive UI lag and ruins the UX.

## Proposal
1. **Exponential Backoff (`tenacity`)**: Wrap the core `litellm.completion` calls in `architect.py` and `coder.py` with `@retry(stop=stop_after_attempt(10), wait=wait_exponential(multiplier=2, min=4, max=60))`.
2. **Strict Logging Suppression**: Force the global logger to `ERROR` mode in all Python workers.

```python
import logging
logging.basicConfig(level=logging.ERROR)
```

## Consequences
- **Pros:** Prevents UI lag. Allows the system to silently self-heal and wait out rate limits.
- **Cons:** If a fatal, non-rate-limit error occurs, the user won't see the warnings leading up to the final crash.

## Alternatives Considered
- Suppressing only `tenacity` (`before_sleep_nothing`): Failed because `litellm` still logged warnings dynamically.
- Truncating logs in the Go orchestrator: More complex and doesn't solve the root issue of noisy Python subprocesses.
