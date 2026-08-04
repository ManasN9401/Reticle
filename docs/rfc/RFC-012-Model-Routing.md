# RFC-012 — Model Routing

Status: Draft
Version: 1.0.0
Author: HyperParallel Core
Last Updated: 2026-08-03

---

## 1. Purpose
This document defines the framework's approach to dynamic model selection (Model Routing). It establishes how the runtime determines which Large Language Model (e.g., Local 32B, GPT-5.5) should be assigned to execute a specific task at runtime.

## 2. Motivation
In agentic systems, hardcoding a frontier API model for every task results in extreme costs and high latency, while hardcoding local models results in task failures for complex logic. HyperParallel needs an intelligent router that continuously optimizes the balance between cost, speed, and capability based on historical success rates.

## 3. Scope
This RFC covers:
- Dynamic Bayesian utility estimates.
- The threshold-based selection policy.
- Runtime learning via Event Bus telemetry (e.g., `WorkerCompleted` vs. `WorkerFailed`).
- **LLM Integration via Skills**: How API keys and clients (e.g. `litellm`) are injected into the sandbox.
- **The Benchmark Harness**: How the system seeds its Bayesian matrix prior to execution.

## 4. Philosophy
Model selection should not be static. The framework must learn from its own failures and successes over time, dynamically demoting models that repeatedly fail at a specific task type and promoting cheaper models that prove themselves capable.

## 5. Principles
- **Cost Optimization**: The cheapest model capable of succeeding should always be chosen.
- **Dynamic Learning**: Utility is not static; it is a Bayesian estimate updated continuously.
- **Task Typology**: Models perform differently on different tasks (e.g., regex vs. architecture). Utility must be mapped per task type.

## 6. Architectural Laws
1. The Model Router must maintain a state matrix mapping `[Task Type][Model ID]` to a Bayesian probability of success.
2. The orchestrator must evaluate a threshold policy before dispatching an LLM worker. It selects the cheapest model whose expected success rate exceeds the required confidence threshold.
3. The orchestrator must inject the selected `model_id` into the Worker Protocol `stdin` payload under `Task.Parameters["llm_model"]`.
4. The orchestrator must update the utility matrix based on the terminal events of a task (e.g., a `WorkerFailed` event decreases the probability of success for that model on that task type).

## 7. LLM Integration via Skills
Instead of hardcoding API keys in agent scripts, LLM access must be provided via the **Agent Skills** system.
- Agents declare an `llm-access` skill in their YAML.
- The Orchestrator (`EnvironmentManager`) installs a universal LLM client (like `litellm`) and injects the API key (e.g., `OPENAI_API_KEY`, `GROQ_API_KEY`) as an environment variable.
- This allows testing with free OpenAI-compatible APIs (like Groq or OpenRouter) or local endpoints (Ollama) with zero changes to the agent logic.

## 8. The Benchmark Harness (Seeding the Matrix)
To prevent the router from starting completely blind, the framework provides a `benchmark` mode.
- **Parallel Evaluation**: The framework runs an agent against a known dataset using *every* available model simultaneously.
- **Telemetry Seeding**: The outcomes (success/fail) are captured by the Event Bus and recorded directly into the Router's Bayesian matrix.
- **Production Readiness**: When the framework switches to normal mode, the router already knows which free/cheap models are capable of executing the task.

## 9. Rationale
By tying the Model Router directly into the Event Bus, the router becomes a native subsystem that passively observes execution traces. If a Supervisor delegates a task to an agent, and that agent fails and retries, the router immediately learns that the chosen model's capability for that task type was insufficient.

## 10. Trade-offs
- **Cold Starts**: When a new task type is introduced without benchmarking, the system has no prior probabilities, requiring it to explore randomly or use an Epsilon-Greedy approach, leading to initial inefficiencies.

## 11. Future Considerations
- Introducing capability matching (e.g., routing tasks that require `web-browser` skills only to models that have high function-calling accuracy).
- Epsilon-greedy exploration: occasionally dispatching tasks to cheaper models with low confidence just to see if they have improved after fine-tuning.

## 12. References
- Deprecated Routing Spec: `docs/specifications/model-routing/v1/001-routing.md`

## 13. Related RFCs
- RFC-003 — Runtime
- RFC-010 — Supervisor Graph
