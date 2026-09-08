> Implementation update (2026-09-08): this historical proposal is superseded where it conflicts with RFC-043 and the current specifications under docs/specifications/. See the audit repair ledger for remaining capability limits.

# RFC-040: Bayesian Model Routing Matrix & Fallback Logic

## 1. Objective
To introduce a dynamic, memory-backed routing matrix that evaluates models statistically over time, enabling the orchestrator to continuously shift routing away from failing APIs (e.g. Rate Limits, Connection Errors, Tool Calling inability) and optimize for high-reliability paths.

## 2. Background
Previously, the `SelectModel` logic relied purely on static capacity limits (`ProviderCapacity`) and explicit configuration. In a landscape with unreliable free-tier endpoints, aggressive rate limits (429), and failing modalities, the engine could get locked into "infinite retry loops" on a single provider. We needed a Bayesian approach to automatically "drop" the probability of a model succeeding and shift traffic entirely onto fallback providers or less complex models in the matrix.

## 3. Architecture Overview

### 3.1 The `Matrix` (Probability State)
The `ModelRouter` maintains a persistent `.reticle/routing_matrix.json` state which maps `[AgentID][ModelID] -> Probability`.
* All models start with a base capability/probability score (typically `1.0` or close to it).
* Upon `WorkerCompleted` (Success): The probability uses Additive Increase (`prob = min(prob + 0.05, 1.0)`).
* Upon `WorkerFailed` (Failure): The probability uses Multiplicative Decrease (`prob = prob * 0.5`).

### 3.2 Main Pass vs Fallback Pass
When `SelectModel` is invoked, it uses a two-pass architecture:
1. **Confidence Pass**: Evaluates only models where `prob >= requiredConfidence` (e.g., `0.90`) and matches the requested `modality`.
2. **Fallback Pass**: If the confidence pass yields no models (e.g., all reliable models are rate-limited or disabled), the router falls back to *any* enabled model matching the `modality`, ignoring the `0.90` threshold but still respecting maximum in-flight capacity.

### 3.3 Provider Penalties & Global Blacklisting
* **429/Timeouts/Connection Errors**: Trigger `PenalizeProvider`. This immediately drops the provider's `ProviderCapacity` by 50% (AIMD) and locks all models for that API key for **60 seconds**. A background timer automatically restores them.
* **Tool Calling Not Supported**: Triggers `PenalizeModel`. This permanently sets `Enabled = false` for that model across all agents to prevent infinite failure loops for agents that mandate tool calling.

## 4. State Persistence
The matrix is saved to `.reticle/routing_matrix.json` on every probability update. On boot, `loadMatrix()` restores the historical preferences, meaning the orchestrator "learns" from previous runtimes which API keys are expired or which models perform poorly for specific agents.
