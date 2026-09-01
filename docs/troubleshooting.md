# HyperParallel Troubleshooting Guide

## The Application Hangs During Execution
**Symptom:** You run `forge.exe` and the telemetry UI stops updating. The terminal does not show any errors, but no new files are created in the workspace.
**Cause:** The Python workers have likely hit an API rate limit and are currently in an exponential backoff retry loop. Because we strictly suppress logging to prevent UI lag, this retry loop is silent.
**Fix:** 
1. Check your `.env` file to ensure your API keys (e.g., `GROQ_API_KEY`, `GEMINI_API_KEY`) are valid and have quota.
2. If you are on a free tier, reduce your batch size using the `-batch 1` flag to prevent concurrent requests from instantly tripping rate limits.
3. Wait up to 10 minutes. The agent will either eventually succeed or fail out after 10 attempts.

## RateLimitExceeded (Tokens Per Minute)
**Symptom:** Groq or OpenRouter instantly fails with a TPM or Context Window error.
**Cause:** The orchestrator defaults to requesting `max_tokens=3000`. Some providers calculate quota usage as `Input Tokens + Max Tokens`. If this sum exceeds your TPM limit, the request is instantly rejected.
**Fix:** Swap to a model with a larger free-tier TPM limit, or edit `coder.py` to omit `max_tokens` entirely.

## Windows Make/Cmake Errors
**Symptom:** Running local Llama/Kimi integrations results in `cmake` or `make` errors.
**Cause:** Our C99 local execution engine requires native Linux `O_DIRECT` syscalls for NVMe streaming and cannot be compiled on Windows MSVC.
**Fix:** Use the API endpoints (`forge.exe` default), or provision a Linux Ubuntu 22.04 VPS as outlined in `RFC-030`.

## API Key Locking & Free-Tier Quota Limits
**Symptom:** The Telemetry UI shows API keys (like OpenRouter) as "Locked" at startup, or workflows instantly fail with `HTTP 429: You exceeded your current quota`.
**Cause:** 
1. **$0 Balance Lock:** If an OpenRouter API key has a $0.00 credit balance, HyperParallel detects this at startup and "locks" the key. A locked key is restricted to fetching only the completely free models (e.g., `glm-5.2:free`), which have extremely harsh global rate limits.
2. **Groq Tool Calling:** Groq recently rotated their free tier models. Currently, their free models (e.g. `groq/compound`, `gpt-oss`) **do not support tool calling**. The orchestrator detects this and permanently disables them during workflows.
3. **Cascading Failure:** Because Groq is disabled, 100% of the workflow load shifts to Gemini and OpenRouter free tiers. When running high-complexity DAGs (e.g. `agent_complexity=5`), dozens of agents launch simultaneously. This instantly triggers 429 Quota Exceeded errors on Gemini's 15 RPM limit and OpenRouter's free limits.
**Fix:** 
- To unlock OpenRouter keys and access fast, high-rate-limit models, add at least $1 of credits to your OpenRouter account.
- To avoid 429 errors on free tier API keys, reduce your task's `agent_complexity` setting to limit the number of parallel agents generated.
