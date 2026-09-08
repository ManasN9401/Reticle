# Reticle architecture, agent lifecycle and engineering recommendations

Date: 8 September 2026. Based on checkout `9a5fd070fef92da7826698e58ac37ae245a9c252` and the [64 grouped audit findings](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/01-findings.md).

These are design recommendations, not claims that new components are already implemented. They are ordered around the observed defects. No cloud provider, production scale, regulatory requirement, GPU type or budget has been assumed. Where those choices matter, the recommendation identifies the required decision or evidence.

## 1. Preserve the useful split, simplify its contracts

Keep Go responsible for durable execution state, scheduling and resource ownership; keep Python for specialist logic and model/tool adapters; keep Studio as a client of authoritative state. The current language split is reasonable. Most failures arise at ownership and protocol boundaries rather than from the choice of languages.

The first architecture change should be a small, reliable execution core. A distributed scheduler, Kubernetes deployment, many more agent types or autonomous self-modification would amplify the present races and contract drift. Start with a single local service and a transactional local store; SQLite is a reasonable candidate to evaluate for the desktop use case. Revisit a remote database/job queue when actual multi-host ownership, throughput or availability requirements justify it.

Proposed component boundaries:

| Component | Owns | Must not decide implicitly |
|---|---|---|
| Admission/compiler | Validated intent, workflow version, agent bindings, effective settings | Whether arbitrary text counts as authorization |
| Execution coordinator | State transitions, attempts, dependencies, cancellation, result commits | Model/tool-specific shell behavior |
| Resource manager | Provider slots, processes, containers, GPU jobs, locks, time/spend budgets | Whether a syntactically valid artifact proves quality |
| Tool broker | Filesystem/network/cloud permissions and typed tool execution | New permissions requested only by a model |
| Worker SDK | Framing, model calls, tool requests, structured results | Cross-run state changes or host credentials by default |
| Artifact/memory service | Immutable outputs, scoped state, provenance and retention | Treating arbitrary HTML as trusted application UI |
| Control API and Studio | Authenticated commands, snapshots, event replay, human decisions | Reconstructing missing authoritative state as fact |

This directly addresses F01–F06, F20–F33 and F38–F55.

## 2. Give execution state one owner and make results transactional

Use a coordinator that serializes transitions per execution. A single service-wide command loop is the simplest initial implementation; per-execution actors or careful transactional locking can follow if measured throughput requires them. Observer callbacks should receive immutable facts after a committed transition. They should not independently mutate workflow state in response to partially ordered events.

A successful attempt should commit its validated result, artifact references, memory changes, graph revision and terminal state together. Only then should the coordinator schedule successors. Reject duplicate or stale attempt results by identity. Publish notifications from the committed record, with retryable delivery to observers.

For the initial local store, model at least:

| Record | Important fields |
|---|---|
| Workspace | Stable ID, canonical root, configured trust/execution policy |
| Run | Unique ID, workspace ID, immutable workflow version, effective settings, state |
| Node | Run ID, node ID, immutable agent version, dependency set, outcome |
| Attempt | Unique ID, node ID, model/environment binding, timestamps, budget, outcome |
| Resource lease | Attempt/run ID, resource type, external ID, expiry, cleanup state |
| Artifact | Content hash/reference, media type, size, producer attempt, provenance |
| Approval | Action/plan hash, target identity, requester, decision, expiry, approver |
| Event | Durable monotonic sequence, schema version, run/attempt identity, safe payload |

Do not use display labels or restarting integer counters as globally unique identities. Existing `exec-001` data needs an explicit migration mapping; do not silently reuse its directory.

Acceptance evidence: concurrent fan-in starts each successor once; one output/memory/graph result is visible atomically; duplicate results are harmless; two runs of one workflow never share mutable parameters; restarting at each commit boundary produces a consistent state. Run concurrency tests with the race detector in a supported build environment.

## 3. Define the complete agent lifecycle

An agent definition is an immutable capability description. A node is a scheduled use of that definition. An attempt is one concrete execution. Keeping these separate prevents retry statistics, executable paths and cancellation state from leaking between runs.

Recommended states and meanings:

| State | Meaning and allowed next actions |
|---|---|
| Queued | Validated and durable; may be cancelled or admitted |
| Preparing | Resolve environment, context and resource needs; still cancellable |
| Waiting for approval | No protected action can run; approve, reject, expire or cancel |
| Ready | Dependencies and policy satisfied; eligible for resource admission |
| Running | A tracked attempt owns live resources |
| Pausing | Stop admitting new work and reach the documented safe boundary |
| Paused | No new admission; UI explicitly states whether existing work remains active |
| Retrying | A classified failure permits another attempt within the remaining budget |
| Cancelling | Cancellation requested; resource cleanup is in progress |
| Succeeded | Verified result and deliverables committed; terminal |
| Failed | Policy, validation or execution failed; terminal |
| Cancelled | Owned work stopped or remaining external work explicitly reported; terminal |
| Interrupted | Restart/disconnection left external execution uncertain; requires reconciliation |
| Blocked/skipped node | A dependency or policy prevents execution; not successful work |

A cancelled run must retain its cancellation intent before, during and after compilation, routing, backoff and spawn. Compiler activity should be a phase or child of the same run, not a loosely related ID that lifecycle commands overlook.

Use a run context from admission onward. Derive node/attempt contexts, deadlines and bounded retry delays from it. For cancellation, stop new work, signal current work, allow a short configurable grace period, terminate remaining owned resources, then verify cleanup. On Windows use a process ownership mechanism suitable for descendant cleanup; Docker/remote jobs need their own tracked cancellation APIs.

Retries should be explicit transitions, not hidden nested loops. Classify failures into permanent configuration, transient provider, validation failure, cancellation and uncertain side effect. A timed-out deployment is not automatically safe to repeat. Keep partial outputs available without calling the run successful.

Acceptance evidence: kill before process creation, during model wait, during shell execution, during compiler work and between retries; restart after a crash; late success after cancellation; approval expiry; dependent-node failure. Every case must have one unambiguous final outcome and released resource accounting.

## 4. Replace generated worker programs with a shared worker SDK

The generated f-string templates are a large defect multiplier. Most generated workers differ in instructions, tools and limits, not in their transport or lifecycle. Generate validated agent configuration and call a shared Python runtime instead of emitting hundreds of lines of duplicated Python.

The SDK should implement a versioned request/response protocol, explicit completion without an artifact when appropriate, progress events, typed failures and tool calls. Include run/node/attempt IDs in every message. Reject wrong IDs, unknown versions, malformed fields and unsupported tool names. Use a single unambiguous framing rule, with bounded message sizes and safe pipe drainage.

A tool's schema and callable implementation should come from one registry. A specialist cannot advertise a replacement tool that falls through to a mocked response. Keep model messages separate from authoritative tool results; validation evidence should reference captured commands and outputs rather than the model's prose.

Move common file, shell, URL, ComfyUI and cloud behavior behind tested adapters. Preserve file bytes exactly. Route all file operations through the same containment policy, including direct reads/writes outside shell execution. Set one documented workspace layout and cwd in native and container modes.

Migration: first wrap the existing coder behavior in the SDK and demonstrate equivalent offline fixtures; then move DevOps/ML and remaining specialists one at a time. Leave incompatible agents explicitly unavailable until their contract tests pass. This is preferable to making all `.yml` files load before their safety and runtime assumptions are repaired.

## 5. Make capability and authorization policy executable

Model instructions should describe good behavior; a trusted tool boundary must enforce what the worker may actually do. Assign each run capabilities such as workspace read, workspace write, internet fetch, local process, specific cloud read, plan, apply or active scan. Resolve these from user intent and configured policy, never from an agent's assertion that it needs them.

Apply canonical path containment, with symlink/junction handling, at the operation boundary. Use read-only input mounts and narrowly writable output paths where practical. Protect agent definitions, authorization state and credentials from generated code. Restrict environment variables to the adapter's needs. Network adapters should validate schemes/endpoints and bound redirects, body size and duration according to the granted capability.

Bind the local API to loopback and require a session credential. Validate origins and IPC senders. Serve generated HTML on an isolated origin or a constrained preview. Stream large artifacts and use opaque upload IDs instead of accepting arbitrary host paths. Add deadlines and bounded per-client queues so telemetry cannot stall execution.

For human approval, prepare the concrete reviewable action first. Show its target, scope, diff/plan, resource/cost limits and relevant risks. Bind the decision to that exact version. If the action changes, invalidate the approval. The worker can request approval but cannot write its own approval record. Routine reversible work already authorized by the user should not gain redundant prompts; the goal is a reliable boundary at genuinely protected actions.

## 6. Build DevOps as a plan/validate/apply pipeline

The current specialist is primarily a prompt plus generic tools. To make it dependable, separate distinct operations:

1. **Generate:** produce IaC/pipeline files using a pinned toolchain; no cloud write credential needed.
2. **Validate:** run syntax, formatting, schema and appropriate static checks in the same environment intended for execution.
3. **Plan:** resolve target account/project/region/cluster and workspace; obtain read/plan capabilities; produce a machine-readable and human-readable change plan.
4. **Authorize:** show the concrete plan and bind approval to its hash, target and configuration revision.
5. **Apply:** use a narrowly scoped execution adapter, record external operation IDs and reconcile uncertain outcomes.
6. **Verify:** inspect the resulting state and required health conditions; preserve evidence and any remaining cleanup work.

Terraform supports saving a plan for later application; a speculative preview alone is not the same artifact as the final action. The proposed approval design should use the saved plan and handle changes to external state rather than silently replanning under an old approval. See [Terraform plan documentation](https://developer.hashicorp.com/terraform/cli/commands/plan).

Preflight actual tools inside the selected environment and only for the requested operation. Terraform CLI, Docker daemon access and Kubernetes API access are different capabilities. An SDK wrapper is not a universal fallback for missing binaries or credentials. Show unavailable capability errors before scheduling the agent.

Use separate provider adapters; do not pick AWS, Azure, GCP or a cluster topology without project requirements. Before a real deployment, gather target identity, existing state/backend ownership, permitted changes, credentials strategy, rollback/recovery expectations and spending constraints. Remote state locking, drift handling, backups and destructive-change policy need to be defined for the chosen deployment, not guessed from this repository.

Acceptance evidence: generation/validation work with no cloud write credentials; the approved plan cannot change unnoticed; a denied/expired approval blocks apply; duplicate delivery does not duplicate a protected operation; a disconnected apply is reconciled before retry; verification failures remain failures even when file generation succeeded.

## 7. Treat ML training as a managed job, separate from code generation

Support three clear products: generated training code, a bounded smoke run, and a real experiment. The UI and result schema should identify which was performed. A 100-sample single-epoch test can validate wiring; it cannot establish real dataset accuracy or production fitness.

Each real experiment should declare and record:

| Dimension | Required evidence/configuration |
|---|---|
| Data | Source/version/hash, split procedure, preprocessing, access scope and leakage checks |
| Model | Architecture/base revision, tokenizer, initialization and trainable parameter policy |
| Environment | Python/framework/CUDA dependencies, image or environment hash, device identity |
| Resources | CPU/RAM, GPU and memory needs, disk, duration and optional spend ceiling |
| Training | Batch/accumulation/precision, optimizer/scheduler, seeds and stopping rule |
| Evaluation | Baseline, held-out split, metrics, acceptance thresholds and uncertainty where relevant |
| Recovery | Checkpoint contents, interval, storage, interruption and resume test |
| Outputs | Metrics, logs, checkpoints, final model reference and provenance |

Discover real hardware and enforce a resource lease before launch. Fixed 16 GB assumptions and a mandatory accumulation factor should become configurable defaults informed by measured memory. Avoid silently falling back to CPU for a requested GPU experiment; report the alternative mode and expected limits. Use a pinned ML image/environment with verified accelerator availability. Docker supports explicit resource and execution settings; they must be supplied rather than assumed: [container execution reference](https://docs.docker.com/engine/containers/run/).

Do not pass model checkpoints through JSON events or recursively stringify them in an outputs endpoint. Store artifacts by reference and stream them. Separate durable experiment logs from high-volume transient console output.

For reproducibility, record a defined environment and verify repeatability within stated tolerances. Seed all relevant RNGs, call the initialization, configure deterministic behavior where feasible, and document unsupported operations or performance tradeoffs. Do not promise identical results across arbitrary hardware/library versions; [PyTorch's reproducibility guidance](https://docs.pytorch.org/docs/2.9/notes/randomness.html) describes these limits.

Checkpointing must preserve the state needed for the advertised resume behavior, not merely weights. Validate the chosen Trainer/custom-loop settings against the pinned implementation; Hugging Face documents training, saving and resume controls in [Trainer](https://huggingface.co/docs/transformers/en/main_classes/trainer). Test interrupted versus uninterrupted training on a small fixed fixture before claiming resumability.

Keep RAG indexing as another versioned data job: workspace-scoped collection, source manifest, embedding revision, exact deletion and rebuild semantics. Never mix a global repository index with unrelated execution memory.

Acceptance evidence: resource admission prevents oversubscription; cancelled jobs stop and release resources; a small run resumes correctly; changed data/embedding revisions do not reuse stale results; a failed validation cannot be converted to success by a model summary; metrics and outputs survive an orchestrator restart.

## 8. Make routing capability-aware and measurable

Split the current router into capability filtering, resource admission, model selection and outcome learning. Required modality/tool support/context size are hard filters. Cost/latency/quality preferences rank only eligible candidates. An image request must fail clearly when no image adapter is available rather than silently choosing text.

Use explicit endpoint and credential references; a host URL is not an API key. Normalize price units and account for input/output usage separately, while preserving unknown prices as unknown. Do not describe a provider/model as free based on a hardcoded zero unless that has been verified for the configured account and use.

Allocate provider capacity through a lease released independently of learning, in success, failure, cancellation and exception paths. Track administrative disable, permanent incompatibility and temporary cooldown separately. Add bounded per-task candidate exclusion so fallback does not repeat the same known failure indefinitely.

Initially call the EMA an empirical score and measure whether it improves routing against a simple baseline. If probabilistic confidence is wanted, define the modeled outcome, calibration procedure and uncertainty. Record model-version, task class, verification result, latency and cost. Keep transport errors separate from output quality. Do not learn “good model” from any artifact-shaped response.

Acceptance evidence: every attempt releases its slot; disabling learning leaves accounting intact; disabled models remain disabled through cooldown; custom endpoints are used for inference; fallback preserves required capabilities; a benchmark shows benefit over fixed routing before adding further adaptive complexity.

## 9. Keep agent adaptation bounded, reviewable and reversible

Dynamic graph extension can be useful for discovered subtasks, but it should modify an execution-owned graph revision through validated proposals. Check acyclicity, dependencies, duplicate IDs, resource budgets and authorization before commit. A worker must not rewrite a globally shared agent definition while other runs execute it.

Use a short evidence-driven lifecycle for ordinary development work: inspect, propose a bounded plan, execute, verify, and report. Add separate specialist/reviewer nodes when they provide independent evidence or distinct permissions. Avoid multiplying nodes that only repeat the same model's assertions.

For persistent learning, store candidate guidance with source run, motivating failure, affected scope and a regression test. Evaluate it against a held-out task set, then promote an immutable version with rollback. Decay or retire stale guidance. Never promote credentials, private source content, prompt injection or a model's speculative conclusion into global instructions. A hook that counts messages should be labeled a reminder until extraction and validation actually exist.

Compaction should retain task intent, accepted decisions, current state, constraints, unresolved questions and artifact references under a stable run/session ID. The durable store should remain the source of truth; a textual summary should not become the only record of approvals, cancellations or completed actions.

## 10. Establish release gates and living documentation

The repository needs tests at its boundaries more urgently than a large unit-test count. Start with the saved audit probes and turn each repaired defect into a maintained regression test in the appropriate module.

| Gate | Minimum checks |
|---|---|
| Static/build | All Go modules/examples compile; vet; Studio typecheck/build/lint; Python syntax |
| Generated contracts | Every registered agent loads; every generator emits valid code/config; every worker handles the actual protocol |
| Lifecycle | Fan-in, duplicates, cancellation windows, approval, restart and resource release |
| Files/security | Traversal/absolute/junction fixtures, byte preservation, opaque uploads, origin/auth and IPC validation |
| Cloud | Offline plan/tool adapters, denied/changed approval, uncertain outcome reconciliation; opt-in live smoke |
| ML | Tiny deterministic-enough fixture, resource admission, interruption/resume, evaluation/artifact validation |
| UI/state | Every lifecycle event, reconnect/snapshot/replay, session identity, history eviction and large artifacts |
| Packaging | Installed app outside checkout; root selection; Forge assets/dependencies; start/stop/restart |
| Documentation | Schemas, examples, links, declared implementation status and tested CLI commands |

Use Linux and Windows CI for the supported runtime paths. Run Go race checks on a configured cgo-capable runner. Keep external-provider tests explicitly opt-in, with clear budgets and isolated credentials. Pin Python environments and image digests; retain lockfiles and dependency update review. Scan Go/Python as well as npm rather than interpreting one ecosystem's audit as complete coverage.

Simplify governance to a few enforceable artifacts: one protocol specification, one manifest schema, one lifecycle table, an accurate quick start, and an implementation-status matrix. Each normative requirement should name its test. Mark drafts and historical audits clearly. Complete the license/contribution information, remove or label placeholders, and move scratch probes out of normal test discovery.

## 11. Suggested delivery sequence

These are dependency-ordered work packages, not time estimates. Actual duration depends on the required supported environments and available engineering capacity.

| Stage | Work | Exit criteria |
|---|---|---|
| A: Contain unsafe access | F01–F06, F46, F51, F54–F55 | Authenticated local control, safe uploads/files, protected approvals, bounded telemetry |
| B: Make one path dependable | F07–F10, F18, F25–F26, F47, F56–F59 | One validated compiler-to-worker-to-result flow; all advertised runnable agents contract-tested; examples and Studio build |
| C: Repair execution ownership | F20–F24, F27–F33, F38–F44, F48–F49 | Unique durable runs, atomic results, reliable cancellation/recovery, no observed race in regression suite |
| D: Environments and specialists | F11–F19, F37, F45 | Reproducible DevOps/ML environments, executable policy, managed jobs and verified outputs |
| E: Routing and product reliability | F34–F36, F50–F55 | Correct capacity/capabilities, working packaged root flow, bounded artifact transport, tested reconnection |
| F: Maintainable evolution | F60–F64 and governance work | Accurate generated docs, versioned skills, regression gates, controlled learning proposals |

Some build repairs can proceed alongside containment, but do not activate previously skipped shell/cloud-capable specialists before their policy and lifecycle are ready. Maintain migration fixtures for existing sessions and manifests. Avoid a single rewrite that changes protocol, persistence, worker behavior and UI simultaneously without compatibility checkpoints.

## 12. Evidence needed before selecting a cloud/ML production architecture

The audit does not require guesses about these matters. Record them before making provider-specific deployment commitments:

- Supported operating systems and whether Studio is a standalone installer or a checkout companion.
- Single-user local execution versus remote/multi-user use; identity and trust boundaries.
- Target cloud accounts/projects/clusters, existing IaC/state ownership and allowed action scope.
- Actual CPU/RAM/GPU capabilities, representative datasets, experiment durations and concurrency.
- Data sensitivity, retention, permitted external model services and credential ownership.
- Availability/recovery expectations, maximum acceptable lost work and spending limits.
- Whether generated code runs only on trusted developer machines or must handle untrusted inputs/users.

Until this evidence exists, the most defensible next milestone is a secure local execution service with one verified end-to-end workflow and honest capability reporting. That provides a sound base for either a desktop-focused product or a later managed cloud runner.
