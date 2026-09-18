---
Status: Review
Author: Codex
Date: 2026-09-18
Scope: Runtime, Forge, worker SDK, Studio, schemas, and project documentation
---

# Logic and documentation review

## Verdict

The project has a credible runtime core and substantially better operational documentation than the earlier audit baseline, but the documentation set is not consistently current enough to be treated as authoritative without checking the code. The current environment, runtime-orchestration, memory-lifecycle, durable-effects, ML-environment, and Studio-workspace documents are generally useful. The root README, Studio README, troubleshooting guide, several accepted RFCs/ADRs, the Studio IPC specification, and multiple runtime diagrams describe behavior that no longer exists.

The most serious code issue is the handling of execution-state persistence failure. Some failure paths interrupt an execution without releasing its waitlist slot; other persistence failures change in-memory state and return without publishing any terminal or fatal event. Those paths can leave a run and queue capacity stuck until restart. The frozen task schema also rejects fields that the runtime itself emits.

No P0 defect was found. Three P1 findings, seven P2 findings, and four lower-severity robustness/maintenance findings require attention. The documentation findings are listed separately because some documents accurately describe the intended contract while the implementation violates it.

## Method and evidence

This review traced the admission, compilation, dispatch, retry, memory, graph-mutation, completion, persistence, workspace-inspection, dependency-activity, and approval paths against the current specifications and user documentation. It also inspected the agent/skill registry, JSON schemas, worker SDK, Studio projections, RFCs, ADRs, diagrams, and audit history.

Validation performed on the reviewed tree:

- Runtime: `go test ./...` and `go vet ./...` passed.
- Forge: clean-cache `go test ./... -count=1` passed outside the filesystem sandbox; `go vet ./...` passed. The sandboxed run failed only because Windows `EvalSymlinks` returned access denied during the attachment security test. Re-running without that filesystem restriction confirmed this was not a product failure.
- Python: all 14 discovered unit tests passed; byte-compilation passed for the Python sources that exist.
- Studio: type checking, 10 unit tests, lint, and production build passed. Lint reports seven warnings. Vite reports a future configuration warning and large output chunks.
- `go test -race` was not available in this Windows environment because the active Go toolchain requires CGO for race instrumentation. This review therefore does not claim race-detector coverage.
- No live provider, AWS account, container daemon, GPU workload, or external effect was exercised. Conclusions about those paths are based on source, contracts, and existing tests rather than assumed external state.
- AMD's current Windows compatibility and limitations pages were checked on 18 September 2026. They continue to support the cautious RX 7800 XT guidance in `docs/ml-environments.md`: the card is not listed in the current Windows support matrix, and AMD still documents Windows training limitations.

The worktree already contained an uncommitted one-line change in `runtime/agent/worker.go`. This review did not modify it.

## Code findings

### P1-01 — A fatal persistence failure can leave the waitlist running forever

**Source-confirmed.** `runtime/agent/workflow_engine.go:124-134` marks running or paused executions interrupted and emits `RuntimePersistenceFailed`. `runtime/agent/dispatcher.go:68-81` cancels dispatcher-owned work on that event. The waitlist subscribes to `RuntimeOverloaded`, `WorkflowCompleted`, and `WorkflowFailed` at `cmd/forge/waitlist.go:96-183`, but not to `RuntimePersistenceFailed`.

After interruption, a resulting `WorkerFailed` is ignored because `runtime/agent/workflow_engine.go:358-360` accepts failures only for running or paused executions. The waitlist item can therefore remain `RUNNING`, continue to count against the batch limit, and never call `Pump` to admit replacement work. This contradicts the fatal-admission contract in `docs/specifications/event-taxonomy/v1/001-events.md`.

**Required change:** make the waitlist consume `RuntimePersistenceFailed`, terminalize affected compilation and execution items, release their capacity, and prevent further admission. Include affected execution IDs in the event rather than forcing projections to infer them.

### P1-02 — Several persistence failures are silent to lifecycle consumers

**Source-confirmed.** The graph engine has multiple direct `persistLocked` branches that mutate state and return without `WorkflowFailed` or `RuntimePersistenceFailed`:

- worker completion: `runtime/agent/workflow_engine.go:321-325`;
- graph mutation: `runtime/agent/workflow_engine.go:269-272`;
- execution resume: `runtime/agent/workflow_engine.go:471-473`;
- interrupted-execution retry: `runtime/agent/workflow_engine.go:511-513`;
- node admission: `runtime/agent/workflow_engine.go:618-622`.

The completion and admission cases set the in-memory execution to failed but do not notify the waitlist. The graph-mutation case rolls back the requested delegation and returns; a later completion event can then finish the original supervisor path without the requested graph expansion. Resume and reconcile failures silently restore a prior status without an operator-visible failure reason.

**Required change:** route every execution-store write through one transition helper that either commits state and emits the requested lifecycle event or emits one typed fatal persistence event with the affected execution and node. Do not publish success or continue graph processing after an uncommitted mutation.

### P1-03 — The frozen task schema rejects real runtime messages

**Source-confirmed.** `schemas/task.schema.json` sets `additionalProperties` to `false`, but it does not declare `attempt_id`, `memory_metadata`, or `capabilities`. All three are serialized by `runtime/agent/worker.go:47-60`, and the dispatcher populates attempt and capability data in `runtime/agent/dispatcher.go:139-140,311`.

Any consumer validating a real runtime task against the declared v1 contract will reject it. That makes the schema unsuitable for generated workers and external integration even though in-tree Python workers currently deserialize the JSON permissively.

**Required change:** update and version the schema, define the capability and memory-reference shapes, and add a contract test that marshals a real `Task` and validates it against the published schema.

### P2-01 — Omitted capabilities silently grant a broad compatibility profile

**Source-confirmed and documented only in the low-level specification.** `runtime/agent/registry.go:102-104` replaces an omitted capability list with `defaultWorkerCapabilities`. That default includes workspace read/write, network, container/native execution, memory, delegation, image, and RAG permissions (`runtime/agent/capability.go:42-49`). The public example in `docs/custom_agent_guide.md` omits capabilities, so following the guide grants the broad profile.

The registry also requires only ID, runtime, and entrypoint even though the JSON schema requires name and version. Unknown skill IDs are logged and skipped at `runtime/agent/registry.go:330-337` rather than rejecting the manifest. A typo can therefore remove intended instructions while still launching a broadly capable worker.

**Required change:** make the guide explicit, validate manifests against the same schema the docs publish, fail on unknown declared skills, and use an explicit compatibility-version switch for legacy broad defaults. New definitions should declare least-privilege capabilities.

### P2-02 — Admission memory writes are ordered but not acknowledged

**Source-confirmed.** `cmd/forge/waitlist.go:559-704` publishes the prompt, workspace, attachment, history, and effort values as asynchronous `MemoryWriteRequested` events, then starts submission in a goroutine. FIFO bus ordering makes the write handlers run first, but the waitlist does not receive or inspect write results. The comment that publication “commits memory first” therefore overstates the guarantee: a rejected or failed memory persistence operation can be followed by compilation with missing controls.

**Required change:** use an acknowledged batch write or a direct transactional admission API. Start the compiler only after the required execution memory has been durably accepted.

### P2-03 — Explorer refreshes can overlap and show a previous run

**Source-confirmed.** `studio/src/features/explorer/ExplorerSidebar.tsx:32-64` starts an asynchronous load immediately and every three seconds without cancellation, a request generation, or an in-flight guard. `studio/electron/workspace/reader.ts:218-255` may inspect up to 20,000 entries and performs filesystem work for every entry. A slow scan can overlap the next interval, and a response from a previously selected run can arrive later and overwrite the new run's summary and tree.

`docs/studio-workspaces.md` accurately describes the three-second refresh and safety limits but does not describe this consistency limitation.

**Required change:** allow one scan at a time, bind each result to the requested run ID, discard stale generations, and schedule the next refresh after the current scan completes. Consider incremental filesystem watching for large workspaces.

### P2-04 — Dependency activity from concurrent runs is merged

**Source-confirmed.** Provisioning events produced by `runtime/agent/environment.go:51-67` include agent/dependency information but no execution or task identity. Studio intentionally lets every environment event bypass run scoping at `studio/electron/forge/store.ts:309-315`. The Activity reducer keys operations only as `base` or `agent:<id>` (`studio/src/features/activity/dependencyActivity.ts:17-31`). Concurrent runs using the same agent can overwrite each other's start/completion state, and a selected run can show unrelated provisioning.

**Required change:** attach an environment/provisioning operation ID and execution ID where applicable, project global cache work separately, and key Activity records by operation rather than agent alone.

### P2-05 — “Verified completion” is too weak for several agent kinds

**Source-confirmed.** In `cmd/forge/compiler/lib/worker_sdk.py:279-293`, any successful allowed DevOps command can set `verified`, including `terraform version`, `docker version`, or `bandit --version`. Writing, RAG, and frontend agents can satisfy verification with any successful `read_file` or `list_dir`. A worker can therefore complete after proving that a tool exists or a directory is readable without validating its produced artifact.

**Required change:** define verification evidence by task and artifact. Examples include validating the changed Terraform directory, running the relevant test/build command, or recording a structured review of the exact produced files. Keep the proof in the result envelope for Studio and audit history.

### P2-06 — The current stderr filter can hide relevant failure context

**Source-confirmed, uncommitted worktree change.** The local diff changes `runtime/agent/worker.go:155` from filtering lines that start with `[LLM_STREAM]` to filtering any line that contains the marker. Full `WorkerLog` publication still occurs first, but the bounded stderr attached to a worker failure can now drop an ordinary diagnostic that quotes or mentions the marker. `docs/specifications/studio-logging-architecture.md` describes prefix filtering.

**Required change:** retain prefix-based recognition of the structured stream record, or parse the record type explicitly. Add a test covering a normal error message that merely contains the marker.

### P2-07 — Event handlers can permanently kill bus dispatch

**Source-confirmed robustness gap.** The local event bus invokes subscribers directly and has no panic boundary around handlers. A panicking built-in or extension handler terminates the dispatch goroutine while publishers can continue enqueueing work; shutdown may then wait indefinitely and later lifecycle events will never be delivered.

**Required change:** either recover and quarantine/report a failed subscriber or make handler panic a documented process-fatal invariant and terminate immediately. Silent loss of the sole dispatch loop is the unsafe state.

### P3-01 — Model preference persistence fails silently and is non-atomic

**Source-confirmed.** `runtime/routing/router.go:82-88` ignores directory-creation and file-write errors and writes the live routing matrix directly to its destination. A full disk, permission change, or crash during the write can lose or truncate learned preferences. Loading also silently ignores invalid JSON. Routing still works from defaults, so this is not an execution blocker, but the UI/log claim that preferences are persisted becomes unreliable.

**Required change:** use the same temporary-file, sync, and replace pattern as other durable snapshots; log write/load failures and expose the degraded state.

### P3-02 — Routing outcome has two timing-dependent owners

**Source-confirmed.** The router subscribes to `WorkerCompleted` and `WorkerFailed` at `runtime/routing/router.go:91-116`, while the dispatcher directly calls `UpdateProbability` at `runtime/agent/dispatcher.go:348-350,387`. The first path to acquire the router consumes and deletes the in-flight record; the second becomes a no-op. This currently prevents double scoring, but outcome ownership depends on asynchronous scheduling and is easy to break when either path changes.

**Required change:** choose one owner for score updates. Prefer the committed terminal event so every observer sees the same source of truth.

### P3-03 — Workspace creation errors are ignored at admission

**Source-confirmed.** `cmd/forge/waitlist.go:610-611` computes the isolated workspace and discards the `MkdirAll` error. It then publishes that path into memory and may start attachment processing or compilation. The eventual error will be later and less specific, and a no-attachment workflow can be admitted with a workspace that was never created.

**Required change:** fail the waitlist item immediately, persist the failure reason, release capacity, and call `Pump` when workspace creation fails.

### P3-04 — Attachment cleanup failure leaves a poisoned destination

**Source-confirmed.** `cmd/forge/waitlist.go:811-826` copies with exclusive destination creation, then removes the staged source. If source removal fails, the function returns an error but leaves the completed destination in place. A retry with the same names then fails at `O_EXCL`, even though the copy itself succeeded.

**Required change:** either treat post-copy staging cleanup as a separately reported warning after accepting the destination, or remove the destination on cleanup failure so the operation is retryable. Record which side owns cleanup.

## Documentation findings

### D1 — The root README overstates or misstates current behavior

`README.md:3,12-13` promises “massive concurrency,” randomized failover across API keys, exponential backoff, injection of the entire `src/` tree, and “flawless” iteration. The dispatcher has a fixed 16-slot process limit, routing is score/capacity based, provider retries are centrally classified without the claimed exponential delay, and context is bounded and selected. These claims should be replaced with measurable behavior and links to the runtime specification.

### D2 — The troubleshooting guide gives obsolete operational advice

`docs/troubleshooting.md` describes silent Python Tenacity loops, ten attempts over ten minutes, a fixed 3,000-token request, a C99 Kimi engine, and specific free-tier cascade behavior. The SDK disables provider-library retries, Forge owns a bounded attempt policy, and the referenced local engine does not exist in this tree. This document is unsafe as an operational guide because it directs users to wait for behavior that will not occur and edit obsolete code paths.

### D3 — Studio's README and IPC specification describe an older product

`studio/README.md:60-61` says there is no state snapshot, while `runtime/agent/workflow_engine.go:393-407` emits `WorkflowSnapshot`. It lists an obsolete four-state node model at line 93 and describes an older Markdown-based approval flow despite the separate, hashed decision documents implemented by `runtime/agent/approval.go`. Its mock-output discussion reflects the superseded worker protocol.

`docs/specifications/studio-ipc-architecture.md:7,24-26` calls Studio Electron-like or a web client and specifies `workflow_events`, `worker_logs`, and `memory_sync` channels plus nonexistent `NodeStarted`, `NodeCompleted`, and `NodeFailed` events. The actual application is Electron and uses typed IPC projections plus the runtime WebSocket/event taxonomy.

### D4 — Several diagrams are architectural fiction unless marked historical

- `docs/diagrams/runtime/subprocess_lifecycle_state_machine.md` references nonexistent `executor.go`, Tenacity exhaustion, and `CommitToWorkspace`.
- `docs/diagrams/runtime/session_isolation_architecture.md` shows shared physical files and `FileLockRequested`; current runs use isolated sessions and the worker protocol explicitly rejects the legacy stdin lock scheme.
- `docs/diagrams/runtime/e2e_orchestrator_sequence.md` invents `WorkspaceCreated`, `DAGGenerated`, `WorkflowGenerated`, and `AgentFinished` events that are absent from the event taxonomy.

These diagrams should be rewritten from the current event types or moved under the historical boundary.

### D5 — Accepted RFCs and ADRs are not reliably superseded

Examples include `docs/rfc/RFC-029-Python-Tenacity-Retry-Policy.md` and `docs/decisions/ADR-006-Strict-Logging-Suppression.md`, which still describe Python-owned retry loops as accepted behavior. RFC-026 retains the old mock fallback protocol; RFC-033 requires `session_id` in every event and legacy file-lock requests. Some RFCs contain supersession notes, while others with equally obsolete contracts remain apparently active.

The project needs one visible authority order: current versioned specifications, current operational guides, accepted design records, then explicitly historical material. An accepted RFC must be marked superseded when a later contract replaces it.

### D6 — Documentation governance is not being followed

There are 93 Markdown files under `docs`; 79 do not begin with YAML frontmatter. `docs/governance/RFC_PROCESS.md` requires frontmatter for every RFC and specification. More significantly, status alone is not enough: planning material, accepted-but-obsolete RFCs, and current specifications are all reachable without a clear warning about which one controls behavior.

`docs/README.md` is a useful start but does not define authority, version policy for guides, owners, review dates, or the boundaries of `docs/planning`, `docs/decisions`, `docs/rfc`, and `docs/diagrams`.

### D7 — Skill documentation promises stricter failure than the code provides

`docs/specialized_skills.md:5` says missing declared instruction files fail generation/configuration. The generated coder warns and skips missing files (`cmd/forge/compiler/agents/coder-agent/workers/coder.py:24-28`), and the runtime logs and skips unknown skill IDs. Some hard-coded specialists do fail on a missing instruction, so the statement is only true for part of the system.

### D8 — A Studio process comment documents a removed startup hang

`studio/electron/forge/process.ts:22-25,179-187` says `-native` must always be passed because Forge otherwise prompts on stdin and hangs. The argument is actually conditional, and `cmd/forge/main.go:128-132` now exits with an error when Docker is unavailable. The behavior is sound; the prominent maintenance warning and “non-negotiable” inline comment are stale.

## Documentation quality by area

| Area | Assessment | Reason |
|---|---|---|
| Environment and secrets | Good/current | `docs/environment.md` distinguishes local credentials, AWS identity, durable state, and external-effect reconciliation without claiming credentials are bundled. |
| ML environments | Good/current | Hardware profiles are selectable; Windows is detected and warned about; the RX 7800 XT qualification still matches AMD's published Windows matrix and limitations. |
| Runtime orchestration | Mostly good | The central retry and durable-attempt description matches the main path, but persistence-failure behavior violates its lifecycle guarantees. |
| Memory lifecycle | Good with one admission gap | Retention, CAS, TTL, durable restart, and evaluation are documented and tested; admission writes are not acknowledged before compilation. |
| Durable effects/capabilities | Good design, partial enforcement | The effect model is careful. Broad omitted-capability defaults, schema drift, and weak manifest validation reduce the protection in practice. |
| Frozen schemas | Not current | The task schema cannot validate current tasks; runtime agent validation does not fully match the published schema. |
| Studio workspace/activity guide | Mostly current | The feature exists and safety boundaries are accurate; concurrency/stale-result behavior and cross-run dependency merging are omitted. |
| Studio README/IPC architecture | Poor/outdated | Snapshot, statuses, approval flow, event names, and IPC organization are stale. |
| Troubleshooting | Poor/outdated | It describes removed retry and local-model implementations and should not be used operationally. |
| Root README | Poor as a product contract | It contains unmeasured marketing claims and several technically false descriptions. |
| RFCs, ADRs, planning, diagrams | Mixed and ambiguously authoritative | Valuable history is interleaved with current material and often lacks supersession metadata. |
| Audit history | Good | Dated reports preserve their baseline and repair follow-ups instead of rewriting history. |

The current docs are therefore **not up to date as a whole**. They contain a strong, mostly current core, but the surrounding material is large enough and contradictory enough that a new contributor can easily implement the wrong protocol or debug the wrong retry system.

## Recommended order of work

1. Fix the persistence-failure lifecycle and add fault-injection tests for admission, completion, mutation, resume, and reconciliation. Assert that every affected waitlist item becomes terminal and capacity is released.
2. Bring the task and agent schemas into exact agreement with serialized runtime types and registry validation. Contract-test real marshaled messages.
3. Make new agent manifests explicit and least privilege; reject unknown skills and ambiguous manifests.
4. Make execution admission memory transactional or acknowledged before submitting a workflow.
5. Prevent stale/overlapping workspace scans and add execution-scoped dependency operation identities.
6. Replace generic “a tool succeeded” verification with artifact-specific evidence.
7. Rewrite the root README, troubleshooting guide, Studio README, and Studio IPC specification from the current contracts. Update or archive the three misleading diagrams.
8. Add `Superseded-By`, owner, and last-reviewed metadata; apply it first to every document reachable from `docs/README.md`, then to the RFC/ADR backlog. State the authority order at the top of the documentation index.
9. Add CI checks for schema conformance, internal documentation links, required RFC/spec metadata, and references to nonexistent event types/files.

## External references checked

- AMD, Windows compatibility matrix: <https://rocm.docs.amd.com/projects/radeon-ryzen/en/latest/docs/compatibility/compatibilityrad/windows/windows_compatibility.html>
- AMD, Radeon/Ryzen limitations: <https://rocm.docs.amd.com/projects/radeon-ryzen/en/latest/docs/limitations/limitationsrad.html>
