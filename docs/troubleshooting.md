---
title: Reticle troubleshooting
document_type: Guide
authority: Informative
status: Accepted
version: 1.0.1
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

For a first end-to-end check, choose **single agent** under Workflow depth in Studio and submit a small file task. This still exercises dynamic compilation, model routing, generated-worker startup, workspace tools, verification and durable completion while avoiding unrelated image, RAG, deployment or human-approval dependencies. Use **balanced** for ordinary work (2 to 5 nodes) and **deep** only when the task benefits from several independently owned outputs: the architect must then split the work into at least 3 and at most 16 nodes, and a graph with fewer is rejected.

Restart Forge after rebuilding it. Running processes keep their loaded runtime code, and already compiled sessions keep a snapshot of the worker SDK and generated agents that existed when the session was created. A retry of an old session therefore cannot prove that a newly built router or compiler fix is active.

## Provider rate limits, authentication, or context errors

Forge classifies retryable provider failures and attempts at most the configured `-retries` value, bounded to 1–15 and defaulting to 3. A retry is allowed only when the worker reports that no effect started, or that it changed only workspace files (`write_file`, image tools), in which case the node resumes on another route and is told to build on the existing files. A worker that ran a terminal command or a brokered tool cannot be resumed.

A node waiting for your approval has no time limit and does not fail after 30 minutes or at the task deadline; leave it waiting for as long as you need. Studio announces it with a toast, a system notification and a taskbar flash when it is in the background (Settings, Appearance, Approval notifications), and the **Awaiting approval** list in the Runs view always shows it. If Forge itself is restarted the waiting node is recovered as interrupted and the approval has to be requested again.

The worker's workspace is the session's `src` directory, so `src/main.js` and `main.js` name the same file. If a run left both `src/index.html` and `src/src/main.js`, it predates this rule (older sessions are still readable). When a node keeps being refused with "No successful verification has been recorded", the message now lists the changed files that still need a `read_file`; listing a directory or running `ls`/`cat` does not count. Six identical refusals with nothing changed stop the node with `Agent stalled: completion repeatedly rejected`, after which another model takes it over. The same applies to `Agent stalled: repeated identical tool requests for 3 consecutive iterations` and `Agent iteration budget exhausted` when the node had only written files; the turn budget grows with the number of files the node must produce (12 + 4 per file, minimum 30), or set `llm_max_iterations` to override it. An agent that owns several nodes receives each node's own task, files and neighbours, not a merged list. A node that ends with `Task deadline of 7200 seconds reached while attempt N (model) was still running` ran out of its time, not out of routes: the deadline (`task_timeout_seconds`, at most 7200) is shared by every attempt of the node, so one attempt that hangs leaves nothing for the rest. A model stream that goes silent for 300 seconds after it has started (`Model stream went silent for 300 seconds (timed out)`, tunable with `llm_stream_idle_timeout_seconds`), or that is still open when the agent's own hour is up, is now cut off by the worker and, when only files were written, resumed on another route. Earlier builds waited on a silent stream indefinitely. A generated worker's full prompt is the `<id>.instructions.md` file beside its entrypoint in the session's `agents/<id>/workers/` folder. To see which skills an agent was actually given, open its LLM tab: the first lines read `Skills applied: ... (N KB)`; see `docs/specialized_skills.md` for how the architect chooses them. Routing uses enabled models, capability estimates, provider capacity, cooldown, and the per-agent outcome score. Compiler diagnostics must remain on stderr; otherwise the dispatcher cannot see the retry marker or provider error.

Once a worker has written files the dispatcher cannot replay it, so the worker repairs the failed model request itself and says so with a status line in the LLM tab: "Rate limited by the provider; repeating the same request in Ns", "Provider unavailable ... repeating", or "Request too large for the model; lowered the reply budget from 4096 to 3470 tokens" / "shortened earlier turns" / "dropped N earlier messages". These are bounded and never repeat a tool call; see RFC-052. Providers such as Groq count the reply budget (`max_tokens`, default 4096) against the per-minute token limit, so a 3,500-token prompt can be rejected as "Requested 8498" against an 8,000-token limit. Lower `max_tokens` in Studio's model settings, or prefer a route with a larger limit. Before any file is written none of this happens: the error goes to the dispatcher, which excludes the route and tries another.

Provider SDK deprecation warnings are diagnostic and do not fail a task. For example, Gemini's warning about moving sampling guidance into system instructions is separate from a later HTTP failure. A safe `400 Bad Request` lowers the rejected model's routing score, a safe `403 Forbidden` cools down the affected key even when an SDK wraps it as `BadRequestError`, and a safe upstream streaming timeout retries through the router. An account-wide OpenRouter free-tier limit cools all OpenRouter credential slots so retries can move to another provider. A forced model is recorded and penalized but is not replaced automatically. A model shown as temporarily locked is in cooldown after a provider failure; it is different from a model the user disabled.

If the LLM tab says it is loading a local model or waiting for first output, no response token has reached Reticle yet. The tab shows the selected model, attempt count and elapsed time. Provider reasoning is a separate field and appears only when the model/provider emits it; ordinary response tokens still appear live. Small local models receive a compacted version of long skill references, with fenced examples omitted, so the task and tool contract fit more reliably in an 8K context. A request that remains completely silent past **First Output Timeout** fails and may route elsewhere when it is still retry-safe.

On a single GPU, simultaneous Ollama and ComfyUI residency can consume most VRAM and make model loading or prompt evaluation appear stalled. Reticle serializes its own local GPU operations and dynamically unloads the inactive service. Set Ollama keep-alive to `0` for the lowest idle VRAM use, or retain the default `5m` when repeated LLM turns matter more than reload cost. Direct jobs submitted outside Reticle are not covered by its lock.

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

Confirm Forge is using the same repository root and port configured in Studio. The status-bar connection chip shows the address Studio is using and opens a form to change it; launching Forge from Studio on another port saves that port as the default and retargets the REST client, but a Forge started some other way on a different port has to be connected to by hand. The loopback server requires `.reticle/control-token`; Studio reads it in the main process. On connection, Forge replays `WaitlistUpdated` and authoritative `WorkflowSnapshot` records. Historical timings still depend on the bounded in-memory event history.

The Explorer rejects paths outside the configured checkout, symlinks, sensitive files, and oversized reads. A workspace error can therefore be an intentional containment decision; the message should identify the rejected operation.

## Human approval does not resume

Approval uses a request Markdown document and a separate hash-bound JSON decision under `.reticle/approvals`. Editing status text in the Markdown preview does not authorize anything. Use Studio's Review action so the decision matches the exact request hash. Old decisions cannot authorize a retry because every request has a fresh identity.

## AWS, cloud, or ML work cannot start

Capabilities describe what a worker may request; they do not create credentials, install provider CLIs, or authorize external changes. See `docs/environment.md` for AWS identity options and `docs/ml-environments.md` for OS/GPU support. Cloud apply and other external effects require an implemented adapter, a durable operation identity, explicit authorization, and reconciliation after interruption.

# Model request settings

The Studio **Ollama Context Window** setting controls Ollama's per-request `num_ctx` value. Reticle does not send this provider-specific option to Groq, Gemini, OpenRouter, or OpenAI-compatible llama.cpp servers. Those services determine context capacity from the selected model or server configuration. **Max Output** and **Temperature** remain completion settings for every provider that accepts them.

Studio launches `cmd/forge/forge.exe` exactly as built and never rebuilds it, so a fix made in the Go runtime (`runtime/` or `cmd/forge/*.go`) does nothing until the binary is rebuilt. When Forge starts and the binary is older than the newest Go source, the status bar shows "forge build is out of date" and Settings → Forge names the file that changed. Rebuild with `cd cmd/forge && go build -o forge.exe` (on Windows a running `forge.exe` cannot be overwritten, but it can be renamed first). Python workers and skills are read from source on every run and are never stale.

If most nodes of a run begin with the same two or three failed attempts (the same models returning 404, or the same provider reporting `Request too large ... Limit 8000, Requested 11399`), the runtime is older than the route-learning fix: such a route is now tried once and then skipped by later nodes. Logs showing `Route marked unavailable for other tasks` or `Route request limit learned` confirm it is active. `Attempt time limit of N seconds reached (timed out)` means one attempt hung and was stopped so the node could try another route; a node that ends there with `failure_not_retry_safe_or_unclassified` had run a terminal command first and cannot be replayed.

`Agent stalled: model replied with text but no tool call` means the model printed code in its reply instead of calling `write_file`; text is not saved. The node moves to another model. If a resumed node previously ended with `completion repeatedly rejected` on files that already existed, that was a bug fixed in the same change: an existing required file counts once the node has read it.

A run's `.jpg` textures or `.glb` models that open as plain PNG (Studio shows a note on a `.glb` that holds image data) were written by an older `generate_local_asset`, which saved ComfyUI's PNG output under whatever name the model chose. It now encodes `.jpg`/`.jpeg`/`.webp` paths as that format and refuses a `.glb` or other non-image name before using the GPU, saying how to proceed. Studio opens `.glb` and self-contained `.gltf` files in a 3D viewer (a `.gltf` that references separate `.bin` or texture files cannot be shown) and sniffs image bytes, so a mislabelled file still displays.

A generated site that "finished" but does not load usually passed no check. The completion gate used to bind only the maintained frontend agent; nodes of a generated workflow run as `coding`, so an integrator could complete without ever running `check_web_page`, and a resumed attempt that wrote no web file itself was exempt even then. The gate now binds every node: once the workspace has an `.html` page, a node that wrote a web file (or, on a resume, declares one that already exists) cannot finish until `check_web_page` reports no blocking problems. Scripts with no page to check, such as modules written before `index.html` exists, are not held up. The checker also blocks an import map that sends `three`, `gsap` and similar names to a UMD build (`three.min.js`): it has no ES exports, so `import * as THREE from 'three'` is an empty module that hides the global.

A QA or testing node cannot run a browser, read a console or measure frame rates; no worker can. The architect is told so, and such a node's outputs should be a written report of what static checks found. A node that is briefed otherwise tends to spend its turns on shell commands and never write the report. The LLM tab shows what each tool call was about (`execute_terminal_command node --check main.js`), which the log previously omitted. A worker is also told, at 75 % of its turns and again with three left, which required files are still missing. Terminal commands that only inspect (`ls`, `cat`, `grep`, `find` without `-exec`/`-delete`, `node --check`, `tsc --noEmit`, `git status|log|diff`, with no redirection or chaining) do not make a node unreplayable; anything that runs code still does.

Groq errors such as `Limit 8000, Used 6424, Requested 6635, please try again in 4.5s` describe a partly used minute, not an exhausted key. Only that route is cooled, for the time the provider gave (at least 5 s, at most 15 min), and a request that alone takes more than 60 % of the window is kept off the route for tasks of that size. When a provider returns "not found for account" for three or more distinct routes of one credential, its routes that have never worked are tried after the ones that have.
