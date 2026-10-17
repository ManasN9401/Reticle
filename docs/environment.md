# Environment configuration

Copy `.env.example` to `.env` only when no configuration exists. Preserve
populated keys. Built-in discovery recognizes OpenRouter keys 1–3, Groq keys
1–3 and one Gemini key; none is mandatory when a supported local provider is
configured.

Additional OpenAI-compatible services do not require source changes. Put the
secret under any valid environment-variable name in `.env`, then bind that name
to a provider profile in Studio under **Settings → Models → Provider profiles**.
The profile supplies the base URL, `/models` discovery path and optional static
model IDs. Catalog discovery excludes entries that explicitly advertise only
non-chat endpoints. Select **Static models only** when a provider's compatibility
catalog omits or misreports endpoint metadata; that mode requires at least one
explicit model ID and does not request `/models`. Forge saves the non-secret metadata in the ignored local file
`.reticle/providers.json`; it never copies the key value there. `.env` is loaded
at process start, so restart Forge after adding or changing a secret. Profile
changes and model discovery can be refreshed while Forge is running.

V1 profiles use bearer authentication and the OpenAI-compatible models/chat
contract. “Any key” therefore means any credential for a service implementing
that contract; a service with different authentication or request semantics
requires a reviewed adapter. Remote profiles must use HTTPS. Redirects are not
followed during model discovery, and static model IDs can be supplied when a
provider has no compatible `/models` endpoint.

`OLLAMA_HOST` and `COMFYUI_HOST` are endpoint settings, not secret values. `HF_TOKEN` and `WANDB_API_KEY` are optional for experiments that actually need those services. A blank optional key does not mean the core application is broken.

Local Ollama and ComfyUI work shares one GPU coordination lock by default. Before an Ollama request, Reticle asks an idle ComfyUI server to unload its models and cached executor state. Before a ComfyUI request, it unloads every model reported by Ollama, and it asks ComfyUI to release its model after the image has been collected. This prevents Reticle workers from loading both stacks concurrently; it cannot control unrelated requests submitted directly to either server. Set `RETICLE_LOCAL_GPU_COORDINATION=false` to disable serialization, or disable either unload direction with `RETICLE_OLLAMA_AUTO_UNLOAD=false` or `RETICLE_COMFY_AUTO_UNLOAD=false`. `RETICLE_LOCAL_GPU_LOCK_TIMEOUT` controls how long a worker waits for the shared lock.

Ollama model residency remains configurable. `RETICLE_OLLAMA_KEEP_ALIVE` defaults to `5m`; use `0` to unload after each response, accepting a reload before every subsequent agent turn. Ollama documents `keep_alive: 0` as immediate unloading in its [generate API](https://docs.ollama.com/api/generate). ComfyUI's local server accepts `POST /free` with `unload_models` and `free_memory`; the request is applied by the queue when it is able to do so. Reticle treats cleanup failure as a diagnostic because either local service may legitimately be absent.

`RETICLE_LLM_FIRST_TOKEN_TIMEOUT` defaults to 180 seconds and bounds the time from request dispatch to the provider's first stream event. Studio exposes this value as **First Output Timeout**. A silent timeout is retry-safe only before the worker has started an external effect. The complete task deadline remains a separate runtime limit.

Text-to-coder fallback is disabled by default. This prevents an ordinary planning node from silently selecting a model whose catalog modality is `coding` after all text models fail. Enable **Allow coder fallback for text** in Studio or set `RETICLE_ALLOW_TEXT_TO_CODING_FALLBACK=true` when that tradeoff is intentional.

The audit repair inspected variable names and whether values were populated, without printing values. All currently catalogued hosted-provider variables were populated. Additional missing secret values require the user's integration/key names and an identified credential source; they cannot be reconstructed or invented.

The local control service creates `.reticle/control-token`. Studio reads this in its trusted main process. The browser interface asks for that access code. Never commit this file, forward it to an agent, or paste it into task prompts.

Worker dependency provisioning and job environments are separate from credentials. Cloud deployments must use a scoped, explicitly configured execution identity; installing a Python SDK does not establish authorization or a working cloud CLI.

## AWS authentication

EC2 provisioning needs an authenticated AWS identity with the required permissions. It does not necessarily need long-lived secrets in `.env`:

- For a local operator, configure an AWS CLI IAM Identity Center (SSO) profile, authenticate with `aws sso login --profile <profile>`, and select it using `AWS_PROFILE`. Set `AWS_REGION` for SDKs and `AWS_DEFAULT_REGION` for the CLI as needed. Keep profile/cache files outside the repository.
- When supplying temporary environment credentials, use `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` together. A session token is required for temporary credentials, not ordinary long-lived access keys. Environment access keys can override profile credentials; choose one credential path deliberately.
- For software already running on EC2, attach an appropriate IAM instance profile and let the AWS credential provider obtain temporary role credentials. The operator creating that instance still needs their own identity.

An EC2 SSH private key is a separate connection credential, not an AWS API credential. SSM access has its own IAM and instance configuration requirements.

The commented AWS examples describe operator/adapter configuration only. Reticle currently does **not** forward these credentials to generic workers, mount profile files into job containers, provide an AWS command adapter, or perform protected EC2 deployment. Filling `.env` cannot supply those missing capabilities. The deployment adapter must bind an approved action to its account, region and resource scope before credentials are made available to that action. Actual secret values must come from the configured credential source; none were invented or added during this repair.

Sources: [AWS static/temporary credential settings](https://docs.aws.amazon.com/sdkref/latest/guide/feature-static-credentials.html), [CLI credential precedence](https://docs.aws.amazon.com/cli/latest/topic/config-vars.html), [EC2 role credentials](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html).

Workers provision dependencies lazily, with a cancellation-aware deadline. Each registry skill declares a dependency policy: `locked` requires exact `name==version` pins, `floating` accepts safe package names for compatibility but is not reproducible, and `profile` delegates its complete stack to the selected prepared image or user environment and therefore declares no Python packages. Cache identity includes the interpreter version, OS, architecture, base dependencies and skill dependencies. Packages the shared base environment (`.reticle/envs/forge-base`: litellm, requests, tenacity) already provides are never installed again; only what a skill adds beyond them is installed, into `.reticle/envs/libs-<hash>`, a folder named by the set of extra requirements and the interpreter rather than by the agent, so every agent that needs the same extras shares one copy. (Per-agent folders used to repeat litellm's whole dependency tree for each generated agent, about 130 MB each, which reached several gigabytes after a few runs.) Each folder records when it was last used; once per process, folders unused for 14 days are removed, then the least recently used until the total fits 2 GB, and a folder used within the last 6 hours is never removed. Tune with `RETICLE_ENV_CACHE_MAX_AGE_DAYS` and `RETICLE_ENV_CACHE_MAX_GB` (0 disables that limit). Folders from the older per-agent naming are removed after a day unused. Session folders under `.reticle/sessions` hold a run's output and are not pruned.

Native commands use the worker interpreter's directory on `PATH`. Container commands use the configured image instead of host-installed packages. Exact direct pins do not lock every transitive wheel; release images still need a reviewed full lock and digest.

For a prepared job image, set `RETICLE_WORKER_IMAGE` (prefer a digest), `RETICLE_MEMORY_LIMIT`, `RETICLE_CPU_LIMIT`, and `RETICLE_COMMAND_TIMEOUT` (1–3600 seconds; default 120). Select `RETICLE_ML_PROFILE` as `cpu`, `amd-rocm`, `nvidia-cuda` or `user`. The runtime automatically distinguishes native Windows, WSL2 and Linux and emits profile-specific warnings; it never silently changes the selected profile. Accelerator profiles require numeric `RETICLE_GPU_DEVICES`, and the runtime leases those devices exclusively between its own attempts. Linux/WSL AMD containers receive `/dev/kfd`, `/dev/dri` and the video group, while NVIDIA uses Docker's GPU option. Native Windows AMD execution requires the user's explicit native-execution setting because Linux device mappings cannot be applied to that path. The lease does not coordinate other programs or prove that the host, driver, image and framework are compatible. Validate device visibility and a tensor smoke run before a full experiment.

`COMFYUI_CHECKPOINT` must match a checkpoint installed on the configured local ComfyUI server. Image requests have a deadline; an already-running server job may outlive the requesting worker. Inspect the server queue after cancellation. Dynamic unloading affects model residency and caches, not installed checkpoint files.

`HF_TOKEN` and `WANDB_API_KEY` are passed only to the builtin ML worker. They are not automatically injected into arbitrary generated workers or container commands. Configure any such transfer deliberately in a scoped adapter.

## Memory lifecycle and recovery

With `RETICLE_ROOT` configured, retained shared memory and artifacts are recovered from `.reticle/memory/state.json`. The `.reticle` directory is ignored by Git and the snapshot is written with owner-only permissions where the operating system supports them. Do not store credentials in shared memory: the snapshot contains values in readable JSON.

Defaults limit runtime state to 10,000 entries and 16 MiB, expire orphaned execution memory after 24 hours, release execution scratch memory at terminal workflow events, and retain the latest 100 versions of each artifact. Override these using `RETICLE_MEMORY_MAX_ENTRIES`, `RETICLE_MEMORY_MAX_MIB`, `RETICLE_EXECUTION_MEMORY_TTL_HOURS`, and `RETICLE_ARTIFACT_MAX_VERSIONS`. Set `RETICLE_MEMORY_PERSISTENCE=false` to run without restart recovery.

Conditional memory updates carry `expected_version`. Use zero for create-if-absent or the revision returned by a read/update for compare-and-set. A mismatch rejects the mutation; it does not overwrite the newer value. Unconditional updates remain available for facts whose last writer is intentionally authoritative.

Run `tools/eval_memory_quality.py` without a model for an offline contract check. It reports prompt bytes for the no-memory, full-context and selected-memory conditions. Supplying `--model <litellm-model-id>` runs paired calls, records provider token usage when returned, latency and task accuracy, and can consume provider quota. Store reviewed result JSON outside committed source unless it is an intentional evaluation baseline.

## Durable lifecycle files

Workflow state is stored in `.reticle/executions/state.json`; external operation records are stored in `.reticle/effects/state.json`. Both are local single-writer snapshots. Running work recovers as `interrupted`. Inspect and reconcile the real external system before retrying an interrupted effect. These files may contain target identifiers and task metadata and should remain uncommitted and access-controlled.

## Routing recovery

Forge's `-retries` option is a total per-node attempt budget (default 3, range
1–15), not a promise to try every provider. Recognized failures require explicit
no-effects proof before the dispatcher can restart the worker, or file-effects-only
proof (workspace writes through replay-safe tools) before it resumes the node on
another route. Unknown failures and workers that ran terminal or brokered tools remain terminal.
A stall, empty-reply loop or exhausted turn budget after file-only effects is
resumed on another route like a provider failure, within the same attempt budget. A model
the credential cannot reach (404 "not found for account") is skipped by every later task for
two hours, and a route that reported a per-request size limit is skipped by tasks that cannot
fit it. A single attempt of a Python worker is limited to 3,900 s (`attempt_timeout_seconds`
overrides it) so one hung attempt cannot consume the node's shared deadline.

The router remembers rejected requests for the current task, favours untried
eligible routes and moves away from providers with repeated catalog/request
failures when an alternative is available. Temporary connection/service failures
cool the affected slot without marking its API key rate-limited. An explicit
output-token limit can lower `llm_max_tokens` once for that route using another
attempt; other routes keep the original budget. This is not automatic context
truncation or recovery of a live conversation.

The terminal error includes `Recovery stopped`, the stop reason, attempts and
providers tried, task exclusions, and eligible routes/providers still untried.
Use those diagnostics before increasing the attempt budget. Native protocols
other than the configured OpenAI-compatible protocol still require an adapter;
adding a key alone does not establish model/tool compatibility.

Explicit billing failures such as `Insufficient balance`, `insufficient_quota`
and HTTP 402 are classified as exhausted quota on the selected credential.
Other eligible routes can use the remaining attempt budget. The retry-safety
requirement still applies; a generic `APIError` alone does not authorize replay.

### What survives a restart

Facts about routes and credentials are written to `.reticle/route_health.json` with an expiry on each
entry, so a restarted Forge does not pay the same dead attempts again: routes a provider does not serve
to the credential (2 h), models that cannot take chat or tool requests (24 h), and credentials with an
exhausted quota or balance (until the provider-reported reset, or 2 h for an empty balance). A credential
entry is tied to a digest of the key it was learned for, so replacing the key voids it. Authentication
and access failures (a `401`, a `403`, `PERMISSION_DENIED`) are kept out for the running process only,
because restarting after fixing a key is the normal remedy. The file never holds a secret. To start
without it, set `RETICLE_FORGET_ROUTE_HEALTH=1` for one start or delete the file.

Models that only parse documents, score or embed (`deplot`, `kosmos`, `nvclip`, `parse`, `reward` in
the id) are never offered for chat, coding or image-request work.

## Tools a worker is offered

File tools: `read_file` (pages long files by line and says where the next part starts), `write_file`
(create; `overwrite=true` replaces a file the worker has read completely and that has not changed),
`replace_file_content`, `apply_edits` (several edits to one file, all or nothing), `find_files`,
`move_path`, `delete_path`, `list_dir` and `search_codebase`. `execute_terminal_command` is offered with
`process.container` (or `process.native` with native execution enabled) and by default to agents with no
explicit capability list. In container mode it runs Python 3.12 with pip and network access and no node,
npm or git, in an image (`RETICLE_WORKER_IMAGE`) that is pulled before the command; the time limit is
`RETICLE_COMMAND_TIMEOUT` (default 120 s).

How much one tool reply may carry follows the model's context window when it is known (the catalogue's
`contextLimit`, or `llm_num_ctx` for Ollama): 20% of the window, between 6,000 and 60,000 characters,
and 20,000 when nothing is known. This is a per-reply ceiling, not congestion control: the router's AIMD
only adjusts how many requests run at once per credential.

## Specialist agents (RFC-055)

- **`osint-agent`** searches through an official API. Set one of `SERPER_API_KEY` (Google results), `BRAVE_API_KEY` or `SEARXNG_URL` (a SearXNG server with JSON output) in the environment Forge starts in; `RETICLE_SEARCH_PROVIDER` (`serper`, `brave`, `searxng`, `wikipedia`) forces a choice. These variables reach `osint-agent` and no other worker. With none set `web_search` says so; `provider="wikipedia"` needs no key but searches Wikipedia only. Pages it reads are kept in the node's workspace under `research/`.
- **`browser-agent`** uses `browse_web`, which needs Playwright (installed as the optional `browser.run` dependency) and a Chromium-based browser; `RETICLE_BROWSER` and `RETICLE_BROWSER_BACKEND` now reach workers. What it reads is saved under `browse/`. It is read-only unless a task asks it to submit a form.
- **`quant-agent`** needs `yfinance`, `pandas` and `numpy`, which the `quant-finance` skill pins (`yfinance==1.7.0`, `pandas==3.0.6`, `numpy==2.5.4`) in a shared environment folder. Data comes from Yahoo Finance without a key and is saved under `data/`. Custom analysis uses the terminal (`uv run --with pandas python analysis.py`).
- **`hermes-coder-agent`** needs `execute_terminal_command`: Docker running (or native execution enabled). Without a working terminal it says the code was not run instead of claiming success.

## Browser for `run_page`

A worker granted the `browser.run` capability can load its site in a headless browser with `run_page` ([RFC-053](rfc/RFC-053-Run-Page-Browser-Observation.md)). Two drivers exist. **Playwright** is used whenever its package is importable in the agent's environment; it drives the system Chrome or Edge (nothing is downloaded when one is installed). Otherwise a built-in **DevTools client** drives the Chrome or Edge already installed, with nothing to install. If Playwright is chosen automatically but cannot start, the call falls back to the system browser and the report says so. A system browser is looked for at `RETICLE_BROWSER` (a path to a chrome, chromium or msedge executable), the usual Edge and Chrome install locations, and `PATH`.

- `RETICLE_BROWSER` selects a specific executable.
- `RETICLE_BROWSER_BACKEND` is `playwright` or `cdp` and forces a driver; a forced driver that cannot start is an error, not a silent switch. An agent granted `browser.run` receives `playwright` (pinned) as an *optional* dependency, installed on first use into a shared `libs-<hash>` folder under `.reticle/envs`. If it cannot be installed (no network, a platform without a wheel) the failure is logged, not retried for an hour, and the built-in client is used; it never fails the node's environment setup. Skills can declare their own best-effort packages with `optional_dependencies`.
- WebGL runs on software rendering (SwiftShader), so it works without a GPU but frame rates are lower than on a real one.
- The page can read public CDN files but cannot send data out or reach other local services; see the RFC for the full rules.
