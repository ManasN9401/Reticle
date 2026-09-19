---
status: historical
owner: Reticle Project
updated: 2026-09-19
---

> Historical design record. Current versioned specifications and schemas take precedence.

# RFC-028: API Rate Limit Load Balancing

## Status
**Deprecated / Superseded**

## Context
This RFC originally proposed an array-based fallback mechanism (`endpoints`) inside the Python workers (`architect.py`, `coder.py`) to handle rate limits and quota issues across multiple free-tier keys.

## Resolution
This approach has been completely deprecated in favor of the **Centralized Bayesian Router** implemented directly in the Go Orchestrator. 

Rate-limit and fallback handling is no longer delegated to Python-side `Tenacity` retry loops. Instead, Python scripts now implement a "Fast-Fail" protocol, instantly exiting on rate limits or quota errors. The Go Dispatcher intercepts these failures, globally penalizes the failing API provider in the Bayesian matrix, and seamlessly reroutes the task to the next most capable (and functional) model/provider.

Please see **RFC-012 — Model Routing** for the current architecture.
