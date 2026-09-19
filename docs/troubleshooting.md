---
title: Reticle troubleshooting
document_type: Guide
authority: Informative
status: Accepted
version: 1.0.0
scope: Operations
stability: Stable
owner: Reticle Project
created: 2026-09-19
updated: 2026-09-19
purpose: Diagnose current Forge, worker, provider and environment failures.
audience:
  - End Users
  - Contributors
---

# Reticle troubleshooting

## A run stops making progress

Open Studio's Problems, Activity, and Logs views and find the last structural event for the selected run. Current Python workers do not perform hidden Tenacity retries: LiteLLM retries are disabled, and the Go dispatcher owns the bounded attempt policy. A terminal `WorkerFailed`, `WorkflowFailed`, `RuntimeOverloaded`, or `RuntimePersistenceFailed` event should explain the stop.

- `RuntimePersistenceFailed` interrupts active work and stops new admission. Restart Forge, inspect the durable execution state, and reconcile any external effect before retrying.
- `RuntimeOverloaded` means the required-event queue reached its hard limit. Restart Forge and reduce event volume or concurrency.
- An execution recovered as `interrupted` is not assumed successful. Reconcile it explicitly.
- If the process is alive but no terminal event appears, preserve `runtime.log` and the relevant `.reticle/executions/state.json` before restarting.

For a first end-to-end check, choose **single agent** under Workflow depth in Studio and submit a small file task. This still exercises dynamic compilation, model routing, generated-worker startup, workspace tools, verification and durable completion while avoiding unrelated image, RAG, deployment or human-approval dependencies. Use **balanced** for ordinary work and **deep** only when the task benefits from several independently owned outputs.

Restart Forge after rebuilding it. Running processes keep their loaded runtime code, and already compiled sessions keep a snapshot of the worker SDK and generated agents that existed when the session was created. A retry of an old session therefore cannot prove that a newly built router or compiler fix is active.

## Provider rate limits, authentication, or context errors

Forge classifies retryable provider failures and attempts at most the configured `-retries` value, bounded to 1–15 and defaulting to 3. A retry is allowed only when the worker reports that no effect started. Routing uses enabled models, capability estimates, provider capacity, cooldown, and the per-agent outcome score. Compiler diagnostics must remain on stderr; otherwise the dispatcher cannot see the retry marker or provider error.

Provider SDK deprecation warnings are diagnostic and do not fail a task. For example, Gemini's warning about moving sampling guidance into system instructions is separate from a later HTTP failure. A safe `400 Bad Request` lowers the rejected model's routing score, a safe `403 Forbidden` cools down the affected key even when an SDK wraps it as `BadRequestError`, and a safe upstream streaming timeout retries through the router. An account-wide OpenRouter free-tier limit cools all OpenRouter credential slots so retries can move to another provider. A forced model is recorded and penalized but is not replaced automatically. A model shown as temporarily locked is in cooldown after a provider failure; it is different from a model the user disabled.

1. Check the selected worker's failure line and model in Studio.
2. Confirm the named environment variable exists in the repository-root `.env`; never paste a secret into a manifest or prompt.
3. Check the provider's own quota and model access. Reticle does not infer account balance reliably.
4. Reduce `-batch` when the provider rate limit is lower than the configured concurrency.
5. Reduce the Studio context or output-token setting when the provider reports a context-window or tokens-per-minute limit.

Do not edit generated workers to remove token limits as a first response. Settings are configurable and are recorded in execution memory.

## Docker is unavailable

Container execution is the default. Forge now exits with a direct error when `docker info` fails; it does not prompt silently. Start Docker, or enable native execution explicitly for a trusted workspace. Native mode runs generated commands with the current OS user's permissions.

## A dependency installation fails

Studio's Activity view shows base and agent dependency operations with execution, task, attempt, and operation identity. Read the failure under the selected run. Environments are stored under `.reticle/envs`; skill dependency policies determine whether packages are profile-managed, floating, or exact locked pins.

Check that:

- Python or `uv` is available;
- the package/version exists for the active Python version and OS;
- a locked skill uses exact `name==version` pins;
- an ML profile matches the selected OS and device support.

Do not interpret successful environment creation as proof that a CUDA or ROCm tensor operation works. Follow `docs/ml-environments.md` and run the profile smoke checks.

## A generated agent or skill is missing

Agent manifests require `id`, `name`, `version`, `runtime`, and an existing entrypoint. Unknown fields, runtimes, capabilities, or missing entrypoints fail registry loading. Unknown skill IDs are retained as a v1 compatibility case: they are logged and skipped, leaving that worker in a degraded configuration. Fix the ID or install the skill before relying on the result.

## Studio cannot reconnect or shows stale state

Confirm Forge is using the same repository root and port configured in Studio. The loopback server requires `.reticle/control-token`; Studio reads it in the main process. On connection, Forge replays `WaitlistUpdated` and authoritative `WorkflowSnapshot` records. Historical timings still depend on the bounded in-memory event history.

The Explorer rejects paths outside the configured checkout, symlinks, sensitive files, and oversized reads. A workspace error can therefore be an intentional containment decision; the message should identify the rejected operation.

## Human approval does not resume

Approval uses a request Markdown document and a separate hash-bound JSON decision under `.reticle/approvals`. Editing status text in the Markdown preview does not authorize anything. Use Studio's Review action so the decision matches the exact request hash. Old decisions cannot authorize a retry because every request has a fresh identity.

## AWS, cloud, or ML work cannot start

Capabilities describe what a worker may request; they do not create credentials, install provider CLIs, or authorize external changes. See `docs/environment.md` for AWS identity options and `docs/ml-environments.md` for OS/GPU support. Cloud apply and other external effects require an implemented adapter, a durable operation identity, explicit authorization, and reconciliation after interruption.
