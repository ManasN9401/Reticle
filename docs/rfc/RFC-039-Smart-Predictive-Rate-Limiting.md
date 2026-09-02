# RFC-039: Smart Predictive Rate Limiting

## 1. Overview
As HyperParallel executes highly concurrent Directed Acyclic Graphs (DAGs) across numerous external API providers (Groq, OpenAI, Anthropic), blindly dispatching requests can trigger `429 Quota Exceeded` errors. This wastes tokens, loses progress, and destabilizes the graph. 

However, hardcoding rate limits per provider (e.g., assuming a 30 RPM limit) causes **false rate limiting**, punishing users on higher paid tiers. 

This RFC introduces **Smart Predictive Rate Limiting** using the **AIMD (Additive Increase, Multiplicative Decrease)** algorithm. This allows the orchestrator to dynamically "learn" any user's exact API limits without configuration, predictively avoiding 429s.

## 2. Architecture: AIMD Congestion Control

The `routing.ModelRouter` has been expanded to act as a localized token bucket that tracks `ProviderCapacity` and `ProviderInFlight` requests per API Key environment (e.g., `OPENAI_API_KEY`).

### 2.1 The "Smart" Learning Algorithm
The router assumes no predetermined capacity limits. It starts at an optimistic default of 50 concurrent requests.
- **Additive Increase (Cautious Push):** When a worker completes a task successfully, the router checks if the provider was operating near its ceiling (i.e., using >= 50% of the theoretical capacity). If so, it cautiously nudges the capacity ceiling up by `+1` (capped at 200). This guarantees we never artificially constrain a paid-tier user.
- **Multiplicative Decrease (Instant Protection):** If a worker triggers a `429 Rate Limit` penalty via the `PenalizeProvider` method, the orchestrator instantly divides the provider's `ProviderCapacity` by 2 (minimum 1). 
- **Death-Spiral Prevention:** To prevent concurrent 429s (from multiple in-flight requests failing at once) from repeatedly halving the capacity down to 1, the Multiplicative Decrease is locked to execute only once per penalty window.

### 2.2 Predictive Skipping
During `SelectModel`, before a task is dispatched, the router compares `ProviderInFlight` to `ProviderCapacity`. 
If the provider is congested, the router safely **skips** that provider entirely. It will seamlessly reroute the node to an alternative capable model (e.g., switching from Groq to a Local VPS model) to preserve DAG momentum without hitting a failure state.

### 2.3 Strict In-Flight Tracking
All task assignments, including those forced by `TrackForcedModel` (e.g., tasks pinned to a specific agent), securely increment the `ProviderInFlight` counter. This ensures the router has 100% accurate accounting of active API traffic.
