# RFC-028: API Rate Limit Load Balancing

## Status
Accepted

## Context
When running parallel agents via `forge.exe`, particularly with `-batch` sizes > 1, the agents trigger a large burst of concurrent requests to LLM APIs. Free-tier accounts on services like Groq, Gemini, and OpenRouter have strict rate limits (e.g., TPM, RPM, TPD). When these limits are hit, the orchestrator stalls or crashes, destroying the user experience.

## Proposal
Implement an array-based fallback mechanism (`endpoints`) inside the Python workers (`architect.py`, `coder.py`). Instead of hardcoding a single `api_key` and `model`, the workers will define a list of prioritized fallback endpoints:

```python
endpoints = [
    {"model": "gemini/gemini-3.5-flash", "api_key": os.environ.get("GEMINI_API_KEY", "")},
    {"model": "groq/llama-3.3-70b-versatile", "api_key": os.environ.get("GROQ_API_KEY", "")},
    {"model": "groq/llama-3.3-70b-versatile", "api_key": os.environ.get("GROQ_API_KEY_2", "")}
]
```

When an agent executes an LLM call via `litellm`, the `endpoints` list is shuffled (or prioritized) and iterated. If the primary endpoint raises a `litellm.RateLimitError`, the exception is swallowed by the iteration loop, and the next endpoint is immediately tried.

## Consequences
- **Pros:** Massive increase in workflow resilience. Users can combine multiple free-tier keys to simulate a higher aggregate rate limit.
- **Cons:** Code duplication across worker scripts; shuffling endpoints may cause slight variability in output quality since different models may be used interchangeably depending on transient rate limits.

## Implementation Notes
- Removed OpenRouter from the fallback array as their capable models were recently removed from the free tier.
- To prevent Groq from instantly failing due to "Requested Tokens" calculations (Input + `max_tokens` > 6000 TPM limit), `max_tokens` was explicitly lowered to `3000`.
