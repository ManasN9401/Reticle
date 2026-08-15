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
