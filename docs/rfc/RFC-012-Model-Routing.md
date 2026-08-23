# RFC-012 — Model Routing

Status: Stable
Version: 2.0.0
Author: HyperParallel Core
Last Updated: 2026-08-23

---

## 1. Purpose
This document defines the framework's approach to dynamic model selection (Model Routing). It establishes how the runtime determines which Large Language Model (e.g., Local 32B, Gemini Flash, Groq Qwen) should be assigned to execute a specific task at runtime.

## 2. Motivation
In agentic systems, hardcoding a frontier API model for every task results in extreme costs and high latency, while hardcoding local models results in task failures for complex logic. HyperParallel relies on a centralized Bayesian router that continuously optimizes the balance between capability and cost, while actively mitigating API rate limits and failures.

## 3. Scope
This RFC covers:
- Dynamic Model Discovery via external APIs.
- Capability extraction and scoring.
- Effort-Based Routing.
- Multi-Key Mirroring (API Pooling).
- Fast-Fail and Bayesian Probability Penalties.

## 4. Architecture: Dynamic Model Discovery
Instead of hardcoding supported models, the `routing` package invokes `FetchAvailableModels` on boot. 
1. It reads `OPENROUTER_API_KEY`, `GROQ_API_KEY`, and `GEMINI_API_KEY` (and their `_2` variants).
2. It natively queries the `/models` endpoint of these providers.
3. It parses the price per token and registers the models into the global pool.

### Multi-Key Mirroring
If a secondary key (e.g., `GROQ_API_KEY_2`) is provided, the entire model pool for that provider is duplicated in the routing matrix. The `Model.Key()` acts as the unique identifier (e.g., `groq/qwen-27b|GROQ_API_KEY_2`), allowing the router to effortlessly load-balance between different accounts for the exact same model.

## 5. Architecture: Effort-Based Routing
The framework utilizes decoupled **Cost** and **Capability** matrices.

### Capability Scoring
When models are dynamically discovered, a regex parser extracts their parameter count from their name (e.g., `qwen3.6-27b` = `27.0` capability). Frontier models without explicit parameters (e.g., `claude-3.5-sonnet`) are hardcoded to `100.0`. Generic models fall back to `10.0`.

### Effort Tiers
Agents no longer request specific models. Instead, workflows define the required **Effort** for a task:
- `minimal` (0th percentile - e.g., 8b models)
- `low` (20th percentile)
- `standard` (40th percentile - default)
- `elevated` (60th percentile)
- `high` (80th percentile)
- `absolute` (100th percentile - e.g., Sonnet 3.5, GPT-4)

The router sorts all historically successful models by capability, identifies the minimum acceptable capability for the requested tier, and selects the absolute cheapest model that meets that bar.

## 6. Architecture: Fast-Fail & Bayesian Penalties
The router maintains a state matrix mapping `[Agent ID][Model Key]` to a Bayesian probability of success.

### Fast-Fail Protocol
To prevent Python-side `Tenacity` retry loops from stalling the orchestrator when an API provider goes down or hits a hard rate limit, the Python workers (`coder.py`, `architect.py`) must fail-fast on specific strings (`RateLimit`, `429`, `APIError`, `502`, `Insufficient credits`, etc.) and `exit 1`.

### Global Provider Penalization
When the Go Dispatcher catches a failure, it inspects `stderr`. 
- If the failure was a transient logic error, it slightly penalizes the specific model's Bayesian probability.
- If the failure was a fatal provider error (e.g., "Insufficient credits" or "exceeded your current quota"), the Dispatcher invokes `PenalizeProvider`. This instantly drops the probability of **all models** using that specific API Key to `0.0`, forcing the router to immediately fallback to a completely different API provider (e.g., failing over from OpenRouter to Groq) on the next retry attempt.

## 7. Rationale
By moving rate-limit handling and model fallbacks from Python arrays (deprecated in RFC-028) directly into the Orchestrator's central Bayesian matrix, we achieve true cross-provider load balancing and maximize the utility of free-tier API keys.

## 8. Related RFCs
- RFC-003 — Runtime
- RFC-010 — Supervisor Graph
- RFC-028 — API Rate Limit Load Balancing (Deprecated)
