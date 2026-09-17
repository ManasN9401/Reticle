# Reticle architecture, agent lifecycle and engineering recommendations

Original audit date: 8 September 2026

Current-workspace reassessment: 17 September 2026

This document was originally based on checkout `9a5fd070fef92da7826698e58ac37ae245a9c252` and the [64 grouped audit findings](01-findings.md). It has now been reassessed against the current workspace after the repairs recorded in [03-repair-status.md](03-repair-status.md), the shared-memory work in [04-shared-memory-review.md](04-shared-memory-review.md), and [RFC-044](../../rfc/RFC-044-Memory-Lifecycle-Recovery-Evaluation.md).

The original direction remains broadly sound, but several recommendations are now implemented foundations rather than proposed work. The 17 September implementation added durable local execution snapshots, attempt identity and idempotent result commits, typed capability grants, an external-effect ledger, graph mutation admission, ML profiles and in-process device leases. The remaining priority is to make these separate durable records transactional, implement provider-specific adapters, prove hardware profiles, and finish release evidence.

No cloud provider, production scale, regulatory requirement, GPU target or spending budget is assumed. Where those choices matter, the required decision and acceptance evidence are stated explicitly.

## Current assessment

| Area | Current position | Recommendation status |
|---|---|---|
| Local control and Studio boundary | Authenticated loopback control, origin checks, IPC validation, bounded telemetry and contained uploads are implemented. | Preserve and regression-test. |
| Worker protocol | One EOF-terminated request, one validated response, bounded output and a shared worker SDK are implemented for most specialists. | Foundation complete; consolidate the remaining custom model loops. |
| Execution isolation | Workflow definitions and task parameters are copied per execution; cancellation and pause cover compilation and workers; retries have random attempt IDs. | Durable local recovery exists; cross-file transactionality and remote ownership remain. |
| Result commit | Worker memory mutations and an optional artifact commit idempotently by attempt before completion is published. | Partial; memory and execution snapshots are separate commits rather than one transaction/outbox. |
| Shared memory | Scoped values, retention, expiry, compare-and-set revisions, acknowledged writes, restart snapshots and quality fixtures exist. | Suitable for local bounded use; optimize persistence only from measured need. |
| Event history and restart | The event bus is bounded; queue, memory, executions and effects have atomic local snapshots. Running work recovers as interrupted. | Replace separate files with a transactional journal/outbox when crash-boundary tests justify it. |
| DevOps | Safe validation tools, typed grants and an effect ledger exist; protected apply/scan operations remain refused. | Implement a provider-specific plan/authorize/apply/reconcile adapter only after a target is selected. |
| ML | Explicit profiles, resource limits and in-process GPU admission exist; heavy dependencies are lazy. | Prove an exact host/image/framework combination and checkpoint recovery. |
| Routing | Provider, endpoint, price, tool-support provenance and observation time are explicit; leases, cooldown and empirical scores remain separate. | Add context limits and independently verified task outcomes. |
| CI and packaging | Linux and Windows Go/Python gates are configured; Studio remains Linux-only. | Run remote CI, clean-install/package acceptance and dependency/security gates. |
| Governance and licensing | Core specifications and contribution guidance exist, but historical documents still conflict with current behavior. `LICENSE` is empty. | Documentation reconciliation and an actual license are required before calling the project open source. |

## 1. Preserve the current language split and local-first boundary

Keep Go responsible for authoritative execution state, scheduling, resource ownership and result admission. Keep Python responsible for specialist logic and model/tool adapters. Keep Studio as a client of authoritative state. The audit found boundary and ownership defects, not a reason to rewrite the system in one language.

Continue treating a single-user local service as the supported product boundary until a remote or multi-user requirement is explicit. A distributed scheduler, Kubernetes control plane, remote shared filesystem or autonomous self-modification layer would add ownership problems before the current local recovery model is complete.

The intended boundaries should remain:

| Component | Owns | Must not decide implicitly |
|---|---|---|
| Admission/compiler | Validated intent, immutable workflow version, agent bindings and effective settings | Whether arbitrary prompt text grants authorization |
| Execution coordinator | Run/node/attempt transitions, dependencies, cancellation and committed outcomes | Provider-specific shell or cloud behavior |
| Resource manager | Provider leases, processes, containers, GPU jobs, external operation IDs and budgets | Whether artifact-shaped output proves quality |
| Tool/capability broker | Typed filesystem, network, cloud and active-scan operations | New permissions requested only by a model |
| Worker SDK | Framing, model turns, tool requests, structured results and no-effect reporting | Host-wide credentials or cross-run state by default |
| Artifact/memory service | Scoped state, immutable output versions, provenance and retention | Workflow lifecycle or authorization state |
| Control API and Studio | Authenticated commands, snapshots, replay and human decisions | Reconstructing missing authoritative state as fact |

The current Go event bus can remain the in-process notification mechanism. It should not become the durable source of truth.

## 2. Make the entire run durable, not only selected data

The most important remaining architecture change is a transactional local execution journal. The graph engine now persists workflow execution, node states, attempts and dynamic graph revisions, while memory, queue and external effects use their own atomic snapshots. A process crash no longer silently resumes running work, but a crash between those files can preserve a result separately from its authoritative node transition.

Evaluate SQLite in WAL mode for the desktop/local service. It is a candidate, not a predetermined production database. Retain the current file snapshots until the journal is proven through migration and crash tests. A remote database or job queue is justified only by an actual multi-process or multi-host ownership requirement.

Persist at least:

| Record | Required fields |
|---|---|
| Workspace | Stable ID, canonical root, trust policy and configuration revision |
| Run | Random ID, immutable workflow version, effective settings, state and cancellation intent |
| Node | Run/node ID, immutable agent version, dependencies, state and outcome |
| Attempt | Random ID, node ID, model/environment binding, timestamps, budget, effect state and outcome |
| Graph revision | Parent revision, validated mutation, resulting nodes/edges and proposer attempt |
| Resource lease | Attempt ID, resource type, external ID, expiry, cleanup/reconciliation state |
| Artifact | Content reference/hash, size, media type, producer attempt and provenance |
| Approval | Exact action/plan hash, target identity, configuration revision, decision, approver and expiry |
| Event/outbox | Monotonic sequence, schema version, run/attempt identity and redacted payload |

A successful attempt should transactionally commit its accepted memory/artifact references, graph revision, attempt outcome, node outcome and any terminal run transition. Notifications should be emitted from a durable outbox after commit. Duplicate or stale attempt results must be harmless.

Do not migrate memory into a database merely for architectural symmetry. Its current bounded snapshots are adequate for light local writes. The journal should first solve lifecycle recovery and idempotency; memory persistence can move later if measured write latency, snapshot size or multi-process demand warrants it.

Acceptance evidence:

- Crash and restart at every transition boundary resumes, reconciles or terminates the run without guessing.
- A committed result and its node state are never observed separately.
- Duplicate delivery and late success after cancellation cannot alter terminal state.
- A dynamically changed graph resumes from the exact committed revision.
- Concurrent fan-in starts each successor once.
- Migration preserves existing queue and memory files or rejects them with a recoverable, documented path.

## 3. Keep the public lifecycle small; add attempts and uncertainty

The current execution states (`running`, `paused`, `completed`, `failed`, `cancelled`) and node states (`pending`, `running`, `done`, `failed`) are sufficient for the present UI, but they cannot represent restart uncertainty, approval waits, retries or blocked successors precisely. Do not add every conceivable state to the public API at once. Add states only with defined ownership, persistence and tests.

The implemented additions and remaining public-state candidates are:

| State/record | Purpose |
|---|---|
| Attempt | Implemented: separates one concrete execution from the logical node and gives retries unique identity. |
| Waiting for approval | States that no protected action is running and identifies the bound approval request. |
| Retrying | Records the classified failure, remaining budget and next-attempt time. |
| Cancelling | Distinguishes requested cancellation from verified resource cleanup. |
| Interrupted | Implemented for recovered executions, nodes, attempts and effects. |
| Blocked/skipped node | `blocked` is implemented for failed dependencies; a distinct skipped policy state remains optional. |

Preserve cancellation intent from admission through compilation, provider waiting, backoff, process execution and cleanup. Derive attempt contexts and deadlines from one run context. On cancellation, stop admission, signal owned work, wait for a bounded grace period, terminate remaining owned resources, then verify or record uncertain cleanup.

Retries remain an orchestrator decision. The architect's hidden Tenacity loop and credential-bearing keyword logging have been removed. The shared worker SDK disables provider-library retries and reports whether effects started; each dispatcher try receives a distinct attempt ID.

Acceptance evidence should cover cancellation before spawn, during model wait, during tool execution, between retries and during compilation; approval expiry; restart with an uncertain external operation; dependent-node blocking; and late results after every terminal state.

## 4. Finish consolidating workers around the shared SDK

The original recommendation to replace generated worker programs with a shared SDK has largely been implemented. Generated agents now call the common runtime, and the main specialists use the same request, tool and result contract. Keep this design.

The next work is narrower:

- Move the architect's model call, failure classification, timeout handling and logging onto the same SDK primitives or a small compiler-specific layer built on them.
- Keep code generation limited to declarative instructions, skills, tool grants and limits. Do not regenerate transport, retry or credential-handling code.
- Version progress events if they become machine-consumed. Continue reserving stdout for exactly one final response and stderr for bounded diagnostics.
- Derive tool schemas and implementations from one registry so advertised tools cannot fall through to mocks.
- Capture verification evidence as structured command/tool results. A model's statement that work passed is not verification.
- Add contract fixtures for every worker that bypasses the common `run(...)` entrypoint.

The v1 stdin/stdout contract is appropriate for bounded subprocess workers. Do not replace it with streaming RPC merely because it is more sophisticated. Introduce a v2 transport only when a concrete need such as long-lived interactive tools, mid-turn cancellation acknowledgements or remote workers cannot be met safely by the current boundary.

## 5. Turn permissions into explicit capability grants

Local control security, path containment, environment allowlisting and exact approval hashes are meaningful improvements. They do not yet form a complete capability system. Native shell-capable workers still execute with the user's OS authority when native execution is enabled, and cloud/active-scan operations remain intentionally unavailable.

Define typed grants such as:

- workspace read and bounded workspace write;
- public HTTP fetch with endpoint/size/time limits;
- local process execution in a declared environment;
- container execution with an image digest and resource limits;
- cloud read, plan and apply for a named account/region/resource scope;
- passive security analysis and separately authorized active scanning;
- GPU device lease and experiment artifact write.

Resolve grants from user intent plus configured policy before a worker starts. A model may request a capability but cannot grant it. Bind protected approval to the exact target, plan, configuration revision and budget. If any of those change, invalidate the approval.

Keep ordinary reversible work free of redundant confirmation. The broker is for operations where credentials, external side effects, elevated access or difficult rollback create a meaningful boundary.

## 6. Build DevOps as typed adapters around plan, apply and reconciliation

The DevOps specialist can generate and validate files through common tools, but there is no trustworthy deployment path yet. Keep protected cloud mutation disabled until the following stages exist:

1. **Generate:** produce IaC or pipeline files with no cloud write identity.
2. **Validate:** use the pinned tool version intended for execution and retain its output.
3. **Resolve target:** identify the exact account/project, region, state backend, workspace/cluster and credential source.
4. **Plan:** obtain only read/plan capability and produce machine-readable plus reviewable output.
5. **Authorize:** bind a decision to the saved plan hash, target, configuration revision and limits.
6. **Apply:** use a provider-specific adapter with idempotency or recorded external operation IDs.
7. **Reconcile:** after timeout or disconnection, inspect remote state before deciding whether retry is safe.
8. **Verify:** check declared health conditions and retain failures without relabelling file generation as deployment success.

Do not make a generic terminal worker the cloud adapter. Terraform, AWS APIs, Docker and Kubernetes have different identities, state and cancellation semantics. Implement the first provider only after the user selects the target and intended operations. AWS profile/SSO, temporary credentials and instance roles are valid credential sources; long-lived keys in `.env` are not a prerequisite and should not be the default.

Terraform's saved-plan workflow remains a useful model for binding review to application, while still requiring drift and uncertain-outcome handling. See the [Terraform plan documentation](https://developer.hashicorp.com/terraform/cli/commands/plan).

Acceptance evidence:

- Generation and validation work without cloud write credentials.
- Target identity is visible and cannot change after approval.
- Denied, expired or changed approval prevents application.
- Duplicate delivery cannot duplicate a protected operation.
- A disconnected operation is reconciled before retry.
- Cleanup and rollback expectations are explicit for each supported operation.

## 7. Treat ML as a managed job with selectable environment profiles

Reticle should support three clearly labelled outcomes: generated training code, a bounded smoke run, and a real experiment. The result must state which occurred. A tiny synthetic run validates wiring, not model quality or production fitness.

Replace unconditional experiment dependencies with explicit environment profiles:

| Profile | Intended use | Required admission check |
|---|---|---|
| CPU | Classical ML, tests and small smoke runs | Framework import and bounded CPU/RAM/disk |
| AMD ROCm | Supported Radeon/Linux or WSL combinations | Exact OS/driver/ROCm/framework match, device visibility and tensor smoke test |
| NVIDIA CUDA | Supported CUDA workloads | Exact driver/CUDA/framework match, device visibility and tensor smoke test |
| User-supplied | Existing venv/container/toolchain | Declared immutable identity or captured environment manifest plus smoke test |

The Ryzen 9 9900X and RX 7800 XT are plausible hardware for light ML, but documentation is not proof of compatibility or performance. Native Windows is now the selected target, and the runtime detects native Windows, WSL2 or Linux and emits compatibility warnings without changing the selected profile. AMD's current native Windows matrix does not list the RX 7800 XT and its Windows guidance does not support training, so this target remains warning-gated for experimental inference and CPU work until the matrix changes or the user selects WSL2/Linux. Linux/WSL AMD profiles use the required device mappings rather than Docker's NVIDIA GPU option.

Each experiment should record data revision/splits, model/tokenizer revision, environment identity, resource request, training parameters, evaluation baseline and thresholds, checkpoints, metrics and output provenance. Checkpointing must include optimizer/scheduler/scaler/RNG state needed by the advertised resume behavior. Validate an interrupted small run against an uninterrupted fixture before claiming recovery.

Add a runtime-owned GPU lease before increasing parallel ML execution. A user-selected profile and budget should override automatic suggestions; detection may recommend but must not silently download a large stack, switch accelerator families or fall back to CPU.

Keep large checkpoints and datasets outside event payloads. Store references and stream bounded previews. Maintain RAG indexing as a separate versioned data job with source and embedding revisions.

Relevant implementation details and current limitations remain in [ml-environments.md](../../ml-environments.md). PyTorch's [reproducibility guidance](https://docs.pytorch.org/docs/stable/notes/randomness.html) explains why fixed seeds do not promise identical results across arbitrary platforms.

## 8. Keep shared memory bounded and improve it from measurements

The earlier memory recommendations are now substantially implemented: scope isolation, clone-on-read/write, versioned compare-and-set updates, bounded retention, execution cleanup, restart snapshots and a paired quality harness all exist.

Retain these design rules:

- Shared memory contains scoped task facts and references, never credentials.
- Workers receive a dispatch snapshot rather than direct mutable access.
- Conflicting writes fail through expected revisions instead of silently overwriting newer values.
- Terminal execution cleanup and expiry remain explicit.
- Offline quality tests validate contracts; only real-model runs support reasoning-quality claims.

Prioritize the remaining work by evidence:

1. Continue expanding quality cases from real failures. Stale and contradictory fixtures now exist, and the harness reports prompt size, provider token usage when available, latency and task accuracy.
2. Benchmark write latency and snapshot growth under representative artifact sizes before adopting a journal for memory.
3. Add an execution index for artifact queries if profiles confirm full-series scans matter.
4. Shorten lock holds or use immutable payload references if mixed read/write benchmarks show contention.
5. Add a replicated backend only if the product gains genuine multi-process writers or remote workers.

The existing microbenchmark shows stable scoped reads through 100,000 entries, but it is not a write-heavy soak or tail-latency result. Do not turn the database recommendation for run recovery into an unsupported claim that all memory must move immediately.

## 9. Make routing metadata explicit and test selection quality

Continue separating capability filtering, resource admission, model selection and empirical learning. Modality, tool support, context limits and endpoint compatibility are hard eligibility checks. Cost, latency and quality rank eligible candidates only.

The router treats endpoint settings separately from API keys and releases provider leases independently of learning. Catalog entries now expose provider, endpoint reference, separate input/output prices, metadata source, tool-support provenance and observation time. The remaining weak point is inferred modality and capability: model-name fragments and parameter-size guesses are not reliable quality evidence.

Recommended next changes:

- Represent provider, endpoint, authentication reference, modality, tool support, context limit and known pricing as explicit catalog fields with provenance and observation time.
- Preserve unknown cost or capability as unknown rather than manufacturing a confident value.
- Probe only cheap, safe capabilities during discovery; keep live functional checks opt-in and budgeted.
- Record task class, model/version, latency, usage, transport outcome and independent verification result.
- Compare the empirical score against fixed and simple rule-based baselines before adding more adaptive complexity.
- Exclude a failed candidate for the current task within a bounded attempt budget so fallback does not cycle.
- Keep administrative disable, permanent incompatibility and temporary cooldown as separate reasons.

Do not add a Codex/ChatGPT-subscription adapter to this roadmap unless it becomes a current product requirement. If added later, treat it as an agent execution backend with its own lifecycle, not as a LiteLLM API-key model.

## 10. Make graph generation economical, valid and conflict-aware

Dynamic delegation is limited to a specific mutation shape and an execution-owned graph. It now requires the active attempt, validates the target against the registry, enforces a node cap and persists the graph revision. Per-run wall-time, token and monetary budgets and overlapping-write conflict detection remain.

The architect prompt now requests the smallest useful graph, permits linear work and caps generated plans at 16 nodes. Keep the following task-derived decomposition rules:

- Use one agent when the work is tightly coupled or primarily edits one shared area.
- Parallelize only independent work with clear inputs, outputs and file or subsystem ownership.
- Add an integration/review node only when it resolves real cross-branch work.
- Bound nodes, attempts, wall time, model usage and writable scope per run.
- Reject unknown agent IDs, duplicate node IDs, invalid dependencies, cycles and mutations exceeding the remaining budget before commit.
- Treat overlapping writes as a planning conflict. Prefer isolated workspaces plus an explicit merge/integration step for substantial parallel coding.

Persistent learning should remain a reviewed promotion process. Store candidate guidance with its source run, motivating failure, scope and regression test; evaluate it on held-out tasks; then promote an immutable version with rollback. Never learn credentials, private prompt content or a model's unsupported conclusion.

Compaction should preserve intent, accepted decisions, constraints, current lifecycle state, unresolved questions and artifact references. Durable records, rather than a prose summary, remain authoritative for approvals and effects.

## 11. Add external-effect reconciliation as a first-class subsystem

Process-tree and labelled-container cleanup are implemented. A durable external-effect ledger now records typed operation identity, target, request hash, external identifiers, cleanup ownership and terminal state. Prepared/running records recover as interrupted. Provider-specific observation and cancellation adapters are still required before this can safely drive ComfyUI, cloud or remote training retries.

Every effectful adapter should record before execution:

- a stable attempt and operation ID;
- target identity and request hash;
- idempotency support, if any;
- expected completion and cancellation mechanisms;
- observable remote states;
- cleanup owner and deadline.

After timeout, crash or restart, transition the operation to `interrupted` and reconcile. Only retry when the adapter can establish that the first operation did not occur or that duplicate execution is safe. Otherwise present the uncertain state for operator resolution. This subsystem is a prerequisite for cloud apply, long-running image generation and remote training.

## 12. Strengthen release gates and reproducibility

The repository has meaningful offline Go, Python and Studio checks. CI now pins Go 1.21 and Python 3.12 and configures Go/Python checks on Ubuntu and Windows. Node remains pinned to major 26 for Studio on Ubuntu. Locked skills require exact direct pins, profile-managed skills use a selected prepared environment, and legacy floating skills remain explicitly non-reproducible. Cache identity includes interpreter/platform details, but transitive Python resolutions and job images are not fully locked.

Use the following gates:

| Gate | Current status | Next evidence |
|---|---|---|
| Go/runtime | Linux tests and race checks configured across modules/examples | Add supported Windows tests; retain race execution on a compatible runner |
| Python workers | Offline contract, memory and credential-literal tests | Lock specialist dependencies; test clean provisioning and every custom worker path |
| Studio | Typecheck, projection tests, lint and build | Package/install/start/stop acceptance outside a source checkout on supported OSes |
| Lifecycle | Cancellation, result ordering and overload tests exist | Crash/restart journal tests, attempts, blocked successors and external reconciliation |
| Security | Path/auth/origin/IPC regression coverage exists | Capability-broker policy fixtures and dependency/vulnerability scans for Go and Python |
| DevOps | Safe generation/validation boundary | Fake-provider plan/apply/reconcile tests, then explicit opt-in live smoke |
| ML | Documentation and generic limits | Profile resolver, device smoke test, GPU lease and interrupted/resumed fixture |
| Memory | Unit tests, benchmark and small quality eval | Representative write/size soak and broader real-task paired evaluation |
| Documentation | Schemas, standards and indexes populated | Link/status/CLI checks and removal of contradictory current claims |

Pin release toolchain versions deliberately and use dependency update review. Keep external-provider tests opt-in with explicit cost limits and isolated credentials. Save machine-readable verification evidence without committing secrets or large generated environments.

## 13. Reconcile documentation and licensing

Documentation now has stronger specifications, but the workspace still contains competing narratives. The roadmap lists planned RFCs whose numbering or behavior no longer matches current files. Historical RFCs describe mock fallbacks, internal retries, pessimistic locks, seamless scaling or automatic evolution that the repaired contracts reject. Planning documents may remain as history, but readers need one clear current path.

Recommended documentation work:

- Make `docs/specifications/` plus accepted ADR/RFC decisions the normative source; label planning and superseded RFC material clearly.
- Update the roadmap to list actual documents and statuses rather than future placeholders that already have different implementations.
- Keep one current lifecycle table, worker protocol, manifest schema, environment guide and implementation-status matrix.
- Make normative requirements point to a test or an explicitly unverified acceptance criterion.
- Use repository-relative links inside project documentation.
- Move scratch migration scripts and manual probes out of normal product/test paths or label them clearly.
- Correct Studio comments that still describe unavailable mock fallback behavior.

`LICENSE` currently has zero bytes. The repository should not claim an open-source license until the owner selects one and the full standard text plus appropriate copyright notice is committed. Copyright exists automatically in original work even without registration; choosing a license grants permissions to others. MIT or Apache-2.0 are reasonable permissive options, while GPL-family licenses impose reciprocal distribution terms. The choice and attribution name are owner decisions, not something the code can infer.

## 14. Updated delivery sequence

This replaces the original defect-number sequence, most of which has been completed. The stages are dependency-ordered, not time estimates.

| Stage | Status | Work | Exit criteria |
|---|---|---|---|
| 1. Remove remaining contract contradictions | Implemented foundation | Keep one retry owner, bounded task-derived graphs, active-attempt mutation checks and registered targets | Every remaining custom worker path is contract-tested |
| 2. Durable execution journal | Partial | Local snapshots now persist runs, nodes, attempts and graph revisions; consolidate approvals, leases and an outbox transactionally | Crash-boundary tests recover without duplicate or guessed work |
| 3. Reconciliation and capability broker | Partial | Typed grants and external operation records exist; implement exact provider adapters and observed reconciliation | Interrupted effects are inspectable; unsafe retry is impossible by default |
| 4. Reproducible environments | Partial | Exact direct pins and platform-aware cache identity exist; add transitive locks, pinned images and clean-machine tests | An environment identity reproduces the advertised worker behavior |
| 5. First real specialist adapter | Blocked on product target | Choose either one DevOps provider path or one ML hardware profile from actual requirements | Bounded live acceptance test with retained evidence and cleanup |
| 6. Product and release hardening | Partial | Windows CI is configured; run it, add packaged Studio acceptance, dependency scans and a selected license | Supported platforms install and run from documented inputs |
| 7. Measured optimization | Partial | Routing metadata and memory prompt/usage metrics exist; run routing comparisons and representative performance soaks | Complexity is added only where measurements show benefit |

Do not activate protected cloud operations, active scanning or concurrent GPU jobs merely because their agent definitions load. Their adapters, resource ownership and recovery evidence are the release boundary.

## 15. Decisions and evidence still required

Record these before selecting a cloud or ML production architecture:

- Supported operating systems and whether Studio remains a checkout companion or becomes a standalone installed product.
- Single-user local execution versus remote/multi-user use, including identity and trust boundaries.
- Target cloud account/project, region, IaC/state ownership, permitted actions, credential source and rollback expectations.
- Exact native Windows AMD driver/PyTorch combination, supported workload, representative acceptance fixture and concurrency. WSL2/Linux remain alternative targets if native support is insufficient.
- Data sensitivity, retention, permitted hosted model services and credential ownership.
- Recovery expectations, maximum acceptable lost work and spending limits.
- Whether generated code is trusted developer automation or must safely handle untrusted users and inputs.
- Open-source license family and attribution name.

Until those decisions exist, the most defensible product is a secure, single-user local orchestrator with durable runs, bounded workers, honest capability reporting and opt-in external adapters. That foundation supports both a reliable desktop tool and later remote execution without pretending that local process control already solves cloud or accelerator operations.
