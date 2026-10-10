---
status: accepted
owner: Reticle Project
updated: 2026-10-01
---

# Runtime Orchestration Architecture

This document specifies the core architecture of Reticle's Orchestration Engine, handled entirely by the Go backend (specifically `workflow_engine.go` and `worker.go`).

## 1. The Workflow Engine

The `WorkflowEngine` is responsible for parsing, validating, and executing Directed Acyclic Graphs (DAGs). These DAGs can be statically defined (e.g., `compile.yaml`) or generated dynamically by LLM planning agents like the Architect.

### Graph Execution
- The orchestrator builds a topological sort of the DAG.
- Nodes with no unmet dependencies are executed in parallel using goroutines.
- If a node fails, dependent nodes are cancelled or marked as blocked.
- Executions, graph revisions, node states and attempts are persisted under `.reticle/executions/`.
- Work that was running when the process stopped recovers as `interrupted` and requires an explicit reconciliation decision.
- Dynamic delegation requires the active attempt ID, a registered target worker and remaining node budget before the revised graph is committed.

## 2. Worker Lifecycle (`worker.go`)

Each node in the DAG maps to a specific `Worker` process (usually a Python agent using `worker_sdk.py`).

1. **Context Provisioning**: The Engine resolves the assigned skills and provisions a virtual environment (via UV).
2. **Execution**: The worker is spawned as an external OS process.
3. **Data Pipes**: `stdin` is fed the initial prompt and graph context. `stdout` and `stderr` are streamed back and broadcast over the `EventBus`.
4. **Completion**: The worker must terminate on its own (typically via an LLM JSON tool call that exits the loop) or it is killed via timeout (Iteration budget exhausted).

Local LLM and image requests are serialized through a host lock. A local LLM request asks ComfyUI to release cached models before loading Ollama; a local image request unloads resident Ollama models before queueing ComfyUI and releases ComfyUI caches after collecting the result. The first provider stream event has a separate configurable deadline so a silent model load cannot consume the full task deadline. These controls coordinate Reticle workers on the same host but do not preempt unrelated requests submitted directly to the local services.

Every concrete try has a random attempt ID. The dispatcher owns retry policy, routing outcome updates and completion publication. Accepted result mutations use the attempt ID as a durable idempotency key so duplicate delivery cannot apply them twice.

Provider fallback requires one of the worker's two retry-safety proofs on stderr: `[RETICLE_RETRY_SAFE: NO_EFFECTS]` (it changed nothing) or `[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]` (it changed only workspace files through tools that are safe to run again: `write_file`, which never overwrites, `replace_file_content`, which fails once its target is gone, image generation and the image-edit tools). A worker that ran anything else, such as a terminal command, a RAG index change or a brokered tool, prints neither. With either proof, rate limits, unavailable/connection failures, authentication or permission failures, upstream streaming timeouts, and recognized bad-request/model-compatibility failures may consume another bounded attempt. Authentication and exhausted quota make the affected credential unavailable; a live rate limit applies only a bounded retry delay. Connection and provider-service failures cool the provider slot without falsely marking every key as rate-limited. Request-shape and context failures lower the selected model's score; unsupported tool calling disables that model. An unrecognized process failure is terminal because the runtime cannot assume retry safety from an error code alone.

With only the file-effects proof the node is resumed, not restarted: the dispatcher retries it on another route for provider and request failures (rate limits, quota, access, connection, timeouts, rejected requests, unsupported models), sets the task memory key `resumed_after_files` for that and every later attempt, and the worker adds a note telling the model to list and read the existing files and finish only what is missing. A stall, empty-reply loop or exhausted budget after files were written is the model's own behaviour and stays terminal, because another attempt would repeat the loop at more cost. The one exception is a completion that is rejected repeatedly with nothing changing between rejections (`Agent stalled: completion repeatedly rejected`): the worker stops after six identical rejections, and because every file is already on disk another model only has to verify and finish, so it is resumed. The resume is an ordinary attempt: it draws on the same budget and deadline and takes a new attempt identity, so duplicate result delivery stays idempotent.

When a provider reports when an exhausted account quota resets (`X-RateLimit-Reset`, in the error body), the credential stays unavailable until then, within 48 hours, instead of being probed again every 15 minutes. A reset sooner than the default never shortens the 15 minutes.

The dispatcher keeps a task-local route history. Request rejections exclude that
exact model/credential route for the remaining task, including confidence and
modality fallback passes. Other tasks are unaffected. Untried eligible routes
are preferred; repeated catalog/request failures from one provider favour an
eligible alternative provider. Transport failures temporarily cool the affected
slot without changing credential health or predictive capacity. Transient and
model-behaviour failures may be retried within the existing budget when there
is no eligible untried route. These preferences never bypass admission limits.

An explicit output-token upper bound in the final provider exception can lower a
configured request once per route. This consumes another normal attempt, requires
no-effects proof, and tries that route again without a learning penalty for the
adjustable request. The bound is task-local; a different model receives the original
budget. Context/input overflow is not an output limit. Forced models retain their
single-route/no-fallback behavior. Stalls and exhausted worker budgets require the
no-effects proof; the file-effects proof does not cover them.

Recovery diagnostics distinguish attempt-budget exhaustion, unavailable routes,
cancellation/deadline, forced-model failure, worker-contract failure and
unclassified/unsafe replay. They include tried and still-untried route/provider
counts. The default three attempts (maximum fifteen) and task deadline are shared
by ordinary retries and output-limit adjustments.

The dispatcher stays the only recovery owner while it can act. Once a worker has
changed the workspace the dispatcher can no longer replay it, so the worker SDK
repairs the failed model request itself, and only then: before any change every
provider error is raised untouched for the rules above. After a change the SDK
may repeat the same request after a bounded wait (a rate limit, using the
provider's own wait up to two minutes; a gateway or connection failure on a short
schedule), lower `max_tokens` by the overshoot a provider reports for a request
over its per-minute token limit, and shorten the conversation history in stages.
It never repeats a tool call, never switches model, and abandons a provider that
sends nothing within the first-event deadline. These repairs are visible as status
lines and do not consume the dispatcher's attempt budget. This is an interim
subset of RFC-052, which remains a draft for the full design (runtime-owned
inference with failover).

Hosted model integrations may be built in or supplied as non-secret provider
profiles under `.reticle/providers.json`. A profile binds an OpenAI-compatible
base URL and model catalog to an environment-variable name. The router creates
one catalog and health model for both paths; the dispatcher passes the selected
upstream model and API base through trusted parameters, and worker credential
filtering admits only variables attached to router models. Discovery is bounded,
does not follow redirects and does not treat connection failure as credential
exhaustion. Providers with another protocol or authentication scheme require a
reviewed adapter. See RFC-051.

Waitlist admission creates the isolated workspace and commits required run/compiler memory as one acknowledged batch before submitting a workflow. A workspace or memory failure terminalizes that item without starting compilation.

Workers receive resolved capabilities from their manifest. The shared SDK filters its tool definitions from those grants. Native execution also requires the user's native setting.

Forge owns one loopback attempt-scoped tool broker. Before spawning a concrete
retry, the dispatcher creates a random credential bound to the attempt's
immutable capability grant and passes it only through the child environment.
The broker filters descriptors, bounds and correlates calls, enforces global,
adapter and attempt concurrency, blocks new calls while paused, and revokes the
credential on completion, failure, cancellation or shutdown. Runtime-owned
adapters cannot use Worker Protocol stdout as a second request channel.

MCP is the first external adapter. Go supervises explicitly registered local
stdio servers, bounds newline-delimited JSON-RPC, discovers their tools and
publishes descriptors into the same broker. Every call requires both `mcp.call`
and a trusted `mcp.server:<id>` policy. A pre-transmission write failure may
restart once; a failure after transmission is `uncertain` and is never blindly
replayed.

Plugins are installed local bundles, not a second runtime. Enabling a bundle
transactionally contributes definitions to the agent/skill registry, direct
subprocess tools to the broker, and bundled MCP declarations to the MCP
registry. Plugin code is trusted local code with the user's OS authority; v1
provides containment through validation, least-privilege broker grants, limits
and cancellation, not an OS sandbox.

## 3. Dynamic Execution & Hot-Swapping

When a supervisor requests delegation, the `WorkflowEngine` validates and commits new execution-scoped nodes and edges. Graph changes do not hot-reload global agent definitions. The separately managed plugin registry can publish or remove definitions for future dispatches; active attempts retain their immutable grants. See [RFC-045](../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md) for restart and external-effect semantics.
