# Reticle project audit — defects and contradictions

Date: 8 September 2026. Reviewed checkout: `9a5fd070fef92da7826698e58ac37ae245a9c252`.

## Assessment

Reticle has a workable separation between a Go orchestrator, Python workers, and an Electron interface, but the implementation does not yet support the reliability and isolation promised in several documents. The most consequential problems are an unauthenticated control interface coupled to unsafe filesystem operations, broken specialist registration and generation, asynchronous workflow state changes without adequate synchronization, ineffective approval boundaries, and inconsistent cancellation and recovery.

The cloud DevOps and ML features are particularly incomplete. The actual registry does not load their definitions. Even after that is repaired, the definitions, execution environments, tool implementations, approval flow, and success criteria require further changes. Merely renaming their manifest files would expose additional defects rather than make these specialists operational.

This report records **64 grouped findings**, including smaller related observations within each group. These are not 64 independently scored vulnerabilities. Priorities describe engineering urgency, not CVSS scores:

| Priority | Meaning |
|---|---|
| P0 | Close before allowing untrusted clients or meaningful host/cloud access. |
| P1 | Core correctness, isolation, or release blocker. |
| P2 | Significant reliability, usability, maintainability, or documentation defect. |
| P3 | Smaller inconsistency or repository hygiene issue. |

Evidence labels: **Reproduced** means an offline probe or build demonstrated the behavior; **Source-confirmed** means the relevant implementation and callers establish the defect; **Conditional** identifies a defect in a path currently unregistered or dependent on particular deployment/input conditions. No remote exploit, cloud change, real model call, training run, or destructive cleanup was performed.

The separate [architecture and lifecycle report](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/02-recommendations.md) proposes the repair sequence and longer-term design.

## Coverage and validation

The inventory contains 384 tracked files. Coverage combined component-level source review, cross-referencing documentation and manifests, static searches, all-Python syntax checks, Go compilation/vetting, Studio checks, and focused offline probes. It is not a claim that every possible execution path or every dependency has been proven correct.

| Area | Tracked files | Review emphasis |
|---|---:|---|
| Runtime source and embedded UI | 27 | Events, scheduling, workers, memory, routing, telemetry, browser rendering |
| CLI and queue lifecycle | 42 | Startup, persistence, sessions, attachments, maintenance/probe scripts |
| Compiler, agents and workflows | 41 | Manifest loading, generated code, specialist contracts and tools |
| Studio | 79 | Build, IPC, processes, connections, projection, files, settings, packaging |
| Examples | 47 | Module compilation, workflow shapes, claimed demonstrations |
| Skills | 38 | Dependency declarations, instructions, actual runtime integration |
| Documentation/governance | 77 | RFC status, promised contracts, standards, guides and historical claims |
| Other repository files | 33 | Schemas, root scripts, placeholders, duplicate source and configuration |

The [file inventory](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/evidence/tracked-files.csv) lists every tracked path. Generated caches, installed dependencies, private sessions and credentials were not treated as project source. No applicable `AGENTS.md` was found in the repository scan. Repository skills were reviewed as project artifacts; they were not taken as authorization to execute their operational instructions.

| Check | Result |
|---|---|
| `go test ./...` and `go vet ./...`, runtime module | Passed compilation/vetting; all packages reported no test files |
| Same checks, `cmd/forge` module | Passed; no test files |
| `go test ./...`, examples 01–04 | All four failed compilation against changed constructors |
| Studio `npm run typecheck` | Failed: TS2741, missing `PAUSED` mapping |
| Studio `npm run lint` | Exit 0, 12 warnings; warnings are not all demonstrated defects |
| Studio `npm audit --json` | Zero known vulnerabilities reported for the resolved npm dependency tree at audit time |
| Syntax parsing all 68 tracked Python files | No syntax errors in checked-in scripts; invalid-escape warnings present |
| Generator fixtures | Coder valid; Hermes produces invalid Python; Quant/OSINT/Browser raise `NameError`; scaffolder loses fields |
| Runtime fixtures | Registry omissions, incompatible workflow, shared definitions, EOF hang, rejected optional output, accepted wrong response ID, removed subscription still firing |
| File-helper fixtures | Traversal and absolute writes succeed outside `src`; literal `\n` is corrupted |
| Studio projection fixtures | Pause/kill ignored; old nodes survive session change; out-of-order replay loses an eligible event |
| Go race detector | Unavailable: `-race` requires cgo; neither GCC nor Clang was found in the checked command environment |

Reproducible probes and results: [Python](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/evidence/check_python.py), [Python results](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/evidence/python-results.json), [Go runtime](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/evidence/check_runtime.go), [runtime results](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/evidence/runtime-results.json), [Studio](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/evidence/check_studio.cjs), [Studio results](C:/Users/nathm/code/Reticle/docs/audit/2026-09-08/evidence/studio-results.json). These use synthetic fixtures; generated Python is syntax-checked without running its model or shell functionality. Docker command inspection uses a recorder rather than starting containers.

No tracked Terraform configuration, Dockerfile/Compose deployment, Kubernetes deployment, or functioning CI workflow was found. Consequently there is no basis here to claim that a particular live cloud account, IAM policy, cluster, network, remote state backend, GPU, dataset or deployment is configured correctly or incorrectly. Go/Python vulnerability scans, full Electron interaction, installer execution, real provider compatibility, GPU training, and hostile network tests remain unperformed. The npm result is not evidence of general security.

## Control surface, files and human approval

### F01 — P0 — The control server is unauthenticated and listens beyond localhost

**Source-confirmed.** [main.go:216](C:/Users/nathm/code/Reticle/cmd/forge/main.go:216) supplies `:port`; [server.go:25](C:/Users/nathm/code/Reticle/runtime/telemetry/server.go:25) accepts every WebSocket origin; [server.go:202](C:/Users/nathm/code/Reticle/runtime/telemetry/server.go:202) starts an unrestricted listener. The printed localhost URL does not restrict the bind address. No authentication protects enqueue, remove, pause, resume, kill, model settings, outputs or uploads. A client that can reach the port can exercise these functions; actual network reachability depends on the host firewall and environment. Cross-origin WebSocket acceptance is also relevant to hostile web pages, subject to browser network restrictions.

Bind loopback by default, authenticate control and data requests, validate allowed origins, and make remote access an explicit configured mode. Test unauthorized HTTP/WS access and listener addresses before exposing it.

### F02 — P0 — Queue attachments permit arbitrary source-file copying and deletion

**Source-confirmed; no destructive probe run.** [waitlist.go:538](C:/Users/nathm/code/Reticle/cmd/forge/waitlist.go:538) trusts attachment `Path` and `Filename` supplied through queue commands. It opens the source, joins the destination without containment validation, and removes the source even when destination creation or copying fails. There is no requirement that the source be a server-issued staging file. Combined with F01, this exposes host-readable files and can delete them. Destination traversal adds overwrite risk within the service account's permissions.

Accept opaque upload IDs only; resolve them server-side within staging, validate destination names, and remove a source only after a checked, durable copy. Reject caller-supplied absolute paths. Test malicious paths and simulated copy failure using disposable fixtures.

### F03 — P1 — Generated content reaches executable same-origin browser surfaces

**Source-confirmed; exploitability depends on opening the affected view.** [server.go:64](C:/Users/nathm/code/Reticle/runtime/telemetry/server.go:64) serves the complete session tree, including generated HTML, on the control server's origin. That content can make same-origin control requests. The current embedded UI interpolates uploaded `file.name` into HTML at [index.html:3066](C:/Users/nathm/code/Reticle/runtime/telemetry/ui/index.html:3066); legacy embedded pages also insert artifact names into `innerHTML`. The legacy artifact names can originate from generated output. Some surrounding values are escaped, but escaping is inconsistent.

Use DOM text/property assignment, remove obsolete served UIs, and serve previews from an isolated origin or constrained sandbox. Arbitrary generated HTML must not inherit control-plane authority.

### F04 — P1 — Worker filesystem tools escape their advertised workspace

**Reproduced.** [ML forge_utils.py:75](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/ml-agent/workers/forge_utils.py:75) joins untrusted paths with `src` without checking the resolved destination. Both `../outside-src.txt` and an absolute temporary path were written successfully. Reads, directory listing and replacements use comparable unchecked joins, and copies of these helpers exist across specialists/generated workers. These operations execute on the host even when terminal commands use Docker. Container mode therefore does not contain all worker file access.

The same helper converts literal backslash-n sequences into actual newlines. A valid Python string literal became invalid Python in the fixture. Preserve content exactly; centralize canonical path containment, including symlinks/junctions, and verify both read and write targets.

### F05 — P1 — Approval is derived from model-visible text rather than protected authorization

**Source-confirmed; specialist checkpoint path is currently broken separately.** [architect.py:50](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/architect-agent/workers/architect.py:50) treats the substring `-auto-approve` anywhere in a prompt as approval bypass. A prompt discussing that flag can therefore remove the proposed checkpoint. [hitl.py:75](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/hitl-agent/workers/hitl.py:75) finds the first unanchored `status:` in a file that contains the task and proposed plan before its authorization section. Embedded `STATUS: APPROVED` can be interpreted as approval. The approval file is also writable outside a trusted authorization service.

Represent approval as an authenticated command bound to a specific plan/action hash and execution. A prompt, generated plan, editable output file, or model's completion claim cannot supply that authority.

### F06 — P1 — The HitL worker does not implement the worker/UI contract

**Reproduced and source-confirmed.** [hitl.py](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/hitl-agent/workers/hitl.py) expects a payload filename in `argv[1]`; the runtime sends JSON on stdin. The fixture exits immediately with “No payload provided.” Its checkpoint path is hardcoded to `d:\Reticle\runtime\checkpoints`. It expects top-level prompt/context, prints UI markers to stdout while Studio observes worker stderr logs, and exits successfully after approval without producing the artifact the runtime requires. Rejection feedback is unstructured and does not implement the documented replanning transaction.

Repair the entire request, response, storage, waiting-state and rejection path together. A manifest rename alone will not make approval work. Prefer an orchestrator-owned waiting state over a Python process polling a Markdown file.

## Compiler, cloud DevOps, ML and specialist agents

### F07 — P1 — Most advertised specialists are not registered

**Reproduced.** [registry.go:67](C:/Users/nathm/code/Reticle/runtime/agent/registry.go:67) accepts only `.yaml`. Twelve specialist definitions use `.yml`, including DevOps, ML, HitL, frontend, RAG and graphify. Only architect, coder, scaffolder, writer and mock stress tester registered in the fixture. The [ML definition](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/ml-agent/definition.yml) and [DevOps definition](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/devops-agent/definition.yml) also omit runtime/entrypoint and declare input/output objects where the loader expects strings. Directly parsing ML under the current type produced two unmarshal errors.

Unify manifest schema and extension support, validate required runtime fields, and fail startup with a complete list of unusable definitions. Show runnable status in Studio. Do not simply accept ignored fields silently.

### F08 — P1 — Workflow formats disagree and scaffolding discards execution settings

**Reproduced.** [ML example workflow](C:/Users/nathm/code/Reticle/examples/06_ml_training_pipeline/workflows/ml_pipeline.yaml) wraps its definition under `workflow`, while the loader expects a top-level ID; loading reports “missing ID.” The writing example uses the same incompatible form. [scaffolder.py](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/scaffolder-agent/workers/scaffolder.py) emitted `parameters: {}` and omitted the input node's `modality: coding` and `effort: high` in the fixture. Generated workflows consequently lose routing/execution intent.

The scaffolder manually constructs YAML and filenames rather than serializing a validated object; quoted/newline-containing fields and unvalidated agent IDs are additional unsafe inputs. Adopt one schema, preserve supported fields, and round-trip adversarial strings and all examples through the actual loader.

### F09 — P1 — Four specialist generators cannot produce usable workers

**Reproduced with synthetic DAGs.** [quant.py:574](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/quant-agent/workers/quant.py:574), [osint.py:537](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/osint-agent/workers/osint.py:537), and [browser.py:490](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/browser-agent/workers/browser.py:490) interpolate `{missing}` while constructing their outer generated-code f-string, raising `NameError` in the generator. Hermes exits zero but emits an unexpectedly indented worker and an unterminated helper string. Parsing only the checked-in generators misses all four failures.

Replace duplicated nested code strings with a shared tested worker implementation plus declarative configuration. Until then, generate fixtures and syntax-check every emitted Python file in CI, including Hermes; verify runtime behavior separately.

### F10 — P1 — DevOps advertises tools that it cannot execute

**Conditional, source-confirmed.** [devops.py:155](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/devops-agent/workers/devops.py:155) dispatches terminal, write, read, list and completion, but imported tool schemas also advertise search, replacement and URL reading. Those calls return “unsupported ... or mocked.” `write_file` refuses overwriting and tells the agent to use `replace_file_content`, precisely a missing dispatch branch. An ordinary correction to an existing Terraform file can therefore become an unproductive retry loop.

Generate tool schemas from the same implementation registry used to execute them. Add one contract test per advertised tool and reject unsupported tool names as explicit typed errors.

### F11 — P1 — DevOps preflight and fallback do not establish a usable deployment environment

**Conditional, source-confirmed.** [devops.py:29](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/devops-agent/workers/devops.py:29) checks `shutil.which` on the host for Terraform, Docker and kubectl regardless of the requested operation. Terminal commands may instead run in a minimal container. No required version check runs, despite [the skill:11](C:/Users/nathm/code/Reticle/skills/devops-infrastructure/SKILL.md:11). Missing binaries produce a model warning requesting approval; no enforced approval operation is invoked.

The claimed Python fallback is not generally a replacement: `python-terraform` wraps the Terraform CLI, and the Docker SDK needs a daemon. Installing a host-side SDK also does not install it inside the command container. These facts are explicit in the [python-terraform project](https://github.com/beelit94/python-terraform) and [Docker Engine SDK documentation](https://docs.docker.com/reference/api/engine/sdk/).

Preflight only the capabilities needed for the selected operation, inside its actual environment. Record versions, endpoint identity, credentials scope and availability. Model-name substring bans (`mini`, `:free`, etc.) cannot substitute for an action policy and currently reject harmless generation too.

### F12 — P1 — ML execution has no dependable training environment or resource contract

**Conditional; command shape reproduced.** [ML helper:3](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/ml-agent/workers/forge_utils.py:3) gives terminal commands a 120-second timeout and starts a generic Python container without GPU passthrough, training dependencies, CPU/memory limits or a checkpoint/resume protocol. Native commands run from `workspace/src`; Docker commands run from `/workspace`, so `python train.py` points to different locations. Native `python` is resolved from PATH, not necessarily the provisioned worker interpreter.

[ml_engineer.py:48](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/ml-agent/workers/ml_engineer.py:48) correctly warns about CPU-only sandbox training and limits it to a small smoke run. That is useful disclosure, but it is not an implementation of a real training pipeline. The following fixed 16 GB VRAM assumption and accumulation factor cannot establish actual capacity. Docker defaults do not impose resource limits; see [Docker resource constraints](https://docs.docker.com/engine/containers/resource_constraints/).

Separate code generation, bounded smoke training and real training jobs. Discover hardware; choose a pinned environment; specify resources, maximum duration, checkpoint location and resume semantics before launch.

### F13 — P2 — The ML reproducibility guarantee is false and the example is incomplete

**Source-confirmed documentation/implementation mismatch.** [ml-engineering/SKILL.md:21](C:/Users/nathm/code/Reticle/skills/ml-engineering/SKILL.md:21) demands “perfectly deterministic” scripts. Its snippet defines a seed function without invoking it and does not cover deterministic algorithms, worker RNGs, all relevant backend settings or environment versioning. The worker prompt requires only Torch/NumPy seeds, a weaker policy again.

PyTorch explicitly limits reproducibility across versions, platforms and devices and describes additional controls beyond seeds: [PyTorch reproducibility](https://docs.pytorch.org/docs/2.9/notes/randomness.html). Define a reproducibility envelope, call the initialization, record dependencies/device/data versions and verify tolerances. Mandatory gradient accumulation is also overbroad: it supports effective batches under memory constraints, not a universal OOM or correctness guarantee; see [Transformers gradient accumulation](https://huggingface.co/docs/transformers/grad_accumulation).

### F14 — P1 — Iteration exhaustion can be reported as specialist success

**Conditional, source-confirmed.** DevOps loops 25 times, ML 30 and pentester 20, then still construct success-shaped artifacts even if `mark_task_complete` was never reached. See [DevOps loop and result](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/devops-agent/workers/devops.py:129) and [ML loop](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/ml-agent/workers/ml_engineer.py:155). An artifact containing “you must finish” or partially written code is treated as successful work by the runtime. A model's summary also does not establish that validation actually ran.

Return explicit exhausted/incomplete status and preserve partial artifacts separately. Success should reference actual validation evidence: IaC validation/plan status, command exit results, or bounded training/evaluation metrics. The coder generator's stricter exhaustion behavior illustrates inconsistent lifecycle contracts across agents.

### F15 — P1 — RAG knowledge is shared across unrelated workspaces

**Conditional, source-confirmed.** [RAG helper:17](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/rag-agent/workers/forge_utils.py:17) uses fixed `d:\Reticle\runtime\vector_db` and collection `workspace_index`. Queries do not restrict workspace or execution, and reset deletes the collection for every user of that path. An agent can retrieve another project's indexed source or erase its index. The drive path is also nonportable.

Namespace storage and queries by stable workspace identity and index version; authorize index/read/delete separately. Store embedding-model identity and source provenance with every index.

### F16 — P2 — RAG updates retain stale data and deletion uses the wrong filter semantics

**Conditional, source-confirmed.** [RAG helper:26](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/rag-agent/workers/forge_utils.py:26) upserts chunk IDs based on path/offset but does not remove old trailing chunks or reconcile deleted files. A shortened or removed document can remain searchable. [line 155](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/rag-agent/workers/forge_utils.py:155) uses metadata `$contains` as a substring match on a string `source`. Current Chroma documents that operator for membership in metadata arrays, not string substring search: [metadata filtering](https://docs.trychroma.com/docs/querying-collections/metadata-filtering).

Use exact source IDs and delete/rebuild a source transactionally. Reconcile a source manifest on each scan. Bound per-file ingestion, exclude generated environments, include intended source extensions such as TSX, and cache compatible clients/embedding functions rather than reconstructing them for each operation.

### F17 — P1 — ComfyUI can wait forever and is not coherently routed

**Conditional, source-confirmed.** [frontend helper](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/frontend-agent/workers/forge_utils.py) hardcodes `127.0.0.1:8188`, assumes a named checkpoint is installed, and polls indefinitely while swallowing errors; some requests lack timeouts. It does not implement cancellation, GPU admission or safe output path limits. The documented configurable host is not consistently used. [models.go](C:/Users/nathm/code/Reticle/runtime/routing/models.go) includes ComfyUI only in fallback catalog behavior, and a routed image identifier is not itself a LiteLLM text-completion implementation.

Give image generation a distinct provider adapter, bounded job lifecycle and capability discovery. Verify endpoint health, checkpoint availability and output containment before accepting work; preserve failed and cancelled statuses.

### F18 — P1 — Other specialists also use incompatible request/response conventions

**Conditional, source-confirmed.** [frontend.py](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/frontend-agent/workers/frontend.py) and [rag.py](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/rag-agent/workers/rag.py) expect file-argument payloads and emit plain final text instead of the runtime's stdin/artifact contract. Their model defaults bypass normal routed parameters. [graphify.py](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/graphify-agent/workers/graphify.py) reads `context.workspace` instead of the supplied memory workspace, can therefore target the process working directory, installs a global tool during execution, and can wrap tool failure text in an apparently successful artifact.

Use one worker SDK and explicit environment provisioning. Test each specialist through `Worker.Execute`, not merely by invoking its custom standalone entry point.

### F19 — P1 — Pentest scope restrictions are prompt instructions, not enforcement

**Conditional, source-confirmed.** [pentester.py](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/pentester-agent/workers/pentester.py) promises restricted/read-only activity but exposes arbitrary shell execution. Command-word filtering is bypassable by case, alternate tools or Python; detecting `dast`/`zap` in text does not establish authorization and can match a prohibition. The instruction that Bandit `-ll` selects only high findings is inaccurate; it includes medium severity as well.

Authorize actual targets and operation classes in a tool broker, and distinguish passive source analysis from active network testing. Treat model instructions as guidance rather than a security boundary. Correct the severity description and verify allowed/denied operations with harmless fixtures.

## Runtime correctness and lifecycle

### F20 — P1 — Graph state is mutated concurrently without synchronization

**Source-confirmed; race detector unavailable.** [bus.go:62](C:/Users/nathm/code/Reticle/runtime/events/bus.go:62) runs typed subscribers in separate goroutines. [workflow_engine.go](C:/Users/nathm/code/Reticle/runtime/agent/workflow_engine.go) reads/writes `Executions`, node states, artifacts and graph definitions from these handlers and external submissions without a mutex or single owner. Readiness checking and transition to running are not atomic. Concurrent results can race, dispatch a successor twice, or cause concurrent-map failure.

Serialize execution transitions under one owner or consistently lock all state transitions. Add concurrent fan-in, pause/result, graph-mutation/result and cancellation tests under `go test -race` on a cgo-capable runner. Ordinary `go vet` success does not establish race freedom.

### F21 — P1 — Executions share mutable workflow definitions and model parameters

**Reproduced for definitions; source-confirmed for parameters.** [workflow_execution.go:30](C:/Users/nathm/code/Reticle/runtime/agent/workflow_execution.go:30) keeps the caller's workflow pointer. Changing one execution's graph changes another's definition while their node-state maps remain separate. [workflow_engine.go:288](C:/Users/nathm/code/Reticle/runtime/agent/workflow_engine.go:288) reuses node parameter maps, and [dispatcher.go:160](C:/Users/nathm/code/Reticle/runtime/agent/dispatcher.go:160) writes routed model/API-key parameters into them. A later run can see the old model as explicitly forced and inherit settings or credentials.

Make workflow versions immutable. Copy per-attempt parameters and maintain graph changes as execution-owned revisions. Test two executions of the same definition with different routes and mutations.

### F22 — P1 — Outputs, memory and graph mutations do not commit atomically

**Source-confirmed.** [worker.go:268](C:/Users/nathm/code/Reticle/runtime/agent/worker.go:268) publishes completion, artifact, memory writes and graph mutation as separate events; asynchronous handlers can observe them in different orders. The graph advances on stored artifacts. A successor can run before required memory is committed, and a workflow can announce completion before the mutation adding more nodes is processed. Sleeps in callers cannot establish ordering.

Validate and commit one attempt result transaction containing outputs, memory changes and graph revision before evaluating successor readiness. Publish observer events after committing state. Test an output-producing node that also updates memory and extends the graph.

### F23 — P1 — Failed/completed executions have no authoritative terminal state

**Source-confirmed.** [workflow_execution.go:14](C:/Users/nathm/code/Reticle/runtime/agent/workflow_execution.go:14) defines only running, paused and cancelled execution states. Failure handlers announce failure without consistently terminalizing the execution or cancelling siblings. Late artifacts can still alter node state; resume lacks a complete terminal-state guard. Multiple output/version events can revisit completion logic.

Define succeeded, failed, cancelled and interrupted outcomes, plus blocked/skipped successors. Make terminal transitions idempotent and reject stale attempt results. Choose and document fail-fast versus continue-independent-branches behavior.

### F24 — P1 — A missing worker can leave a node running indefinitely

**Source-confirmed.** [dispatcher.go:43](C:/Users/nathm/code/Reticle/runtime/agent/dispatcher.go:43) logs and returns when no worker is found, without publishing a terminal failure. Registry/provisioning errors are frequently ignored upstream, so this is not limited to malformed manual requests. The graph has already transitioned the node to running.

Validate every node binding before admission. If a binding disappears or provisioning fails, emit a typed terminal failure and advance queue accounting. Include an unavailable-agent test.

### F25 — P1 — Worker protocol accepts wrong identities but rejects documented valid results

**Reproduced.** [worker.go:199](C:/Users/nathm/code/Reticle/runtime/agent/worker.go:199) requires a nonempty artifact ID to recognize a response. Artifact-free `result` output exits zero but is rejected as `invalid_json`, making the later legacy result fallback unreachable for that shape. A response with the wrong task ID and missing artifact name/type is accepted and hydrated with current task context. RFC-027 describes an optional artifact.

Version the protocol and validate the complete response against the active execution/node/attempt. Either support no-artifact success deliberately or remove that promise and migrate callers. Reject malformed output with field-specific diagnostics, not a JSON error containing `<nil>`.

### F26 — P1 — Pipe handling can deadlock and misclassifies timeouts

**EOF hang reproduced; other pipe risks source-confirmed.** [worker.go:212](C:/Users/nathm/code/Reticle/runtime/agent/worker.go:212) closes stdin only after reading a result. A worker following the documented read-to-EOF pattern blocks until killed. The result scanner stops at the first recognized artifact and then waits for process exit without draining later stdout; sufficient trailing output can block exit. Stderr uses the default scanner token limit, does not robustly propagate scanner failure, and is collected in an unsynchronized buffer read by the main execution path. A long stderr line can stop drainage. The fixture's deadline is reported as `exit_non_zero` rather than timeout.

Choose one request framing convention, drain both pipes safely, bound retained logs rather than abandoning drainage, join reader goroutines, and map context cancellation/deadline separately. Go documents the relevant pipe and wait sequencing in [os/exec](https://pkg.go.dev/os/exec).

### F27 — P1 — File-lock grants are a stub

**Source-confirmed.** [worker.go:173](C:/Users/nathm/code/Reticle/runtime/agent/worker.go:173) publishes a lock request but immediately writes `FileLockGranted`; it does not acquire the available mutex store or wait for a real owner decision. Parallel workers can both be told they hold the same lock. This contradicts strict-isolation claims.

Implement a scoped lease/lock service with ownership, cancellation and release-on-exit, or remove the grant protocol and use isolated worktrees plus an explicit merge step. Test simultaneous requests and owner termination.

### F28 — P1 — Removing an automation does not unsubscribe it

**Reproduced.** [subscription_manager.go:98](C:/Users/nathm/code/Reticle/runtime/agent/subscription_manager.go:98) deletes its map entry, but the bus still owns the registered closure; publishing the trigger still creates a task. Re-registering an ID can leave duplicate handlers. Triggering artifact/event data is announced in telemetry but is not reliably supplied as the generated task's inputs or memory, undermining artifact-auditing automations.

Return a subscription handle from the bus; dispose it on remove/replace and shutdown. Carry a validated trigger reference into each task. Test removal, replacement, restart and duplicate delivery.

### F29 — P2 — Memory/artifact APIs expose mutable state and incomplete indexes

**Source-confirmed.** [runtime_state.go](C:/Users/nathm/code/Reticle/runtime/memory/runtime_state.go) returns values that can contain mutable maps without defensive copying; access through a read method does not prevent mutation as comments suggest. Colon-concatenated keys can collide when constituent identifiers contain colons. [artifact_store.go](C:/Users/nathm/code/Reticle/runtime/memory/artifact_store.go) exposes pointers and does not comprehensively rebuild secondary indexes when later versions change indexed metadata. Long-lived state lacks a defined retention lifecycle.

Use structured keys and immutable values, or explicitly document ownership and clone boundaries. Keep indexed identity fields immutable or update indexes transactionally. Add lifecycle cleanup and concurrent-version tests.

### F30 — P2 — The event bus lacks bounded work, unsubscribe and orderly shutdown

**Source-confirmed.** [bus.go](C:/Users/nathm/code/Reticle/runtime/events/bus.go) has a 1,000-event buffer but spawns unbounded handler goroutines. It has no public close/drain or unsubscribe mechanism. Wildcard subscribers run synchronously and can block the dispatcher. Event IDs are allocated before concurrent enqueue, so numerical ID order is not necessarily delivery order despite chronological-auditing comments. Payload references remain mutable.

Separate authoritative state commands from best-effort observers. Bound work, define backpressure and shutdown behavior, and assign durable sequence numbers at the serialization point. See F49 for the replay consequence and F64 for related UI/documentation details.

### F31 — P1 — Cancellation misses compilation and model-selection/retry windows

**Source-confirmed.** [dispatcher.go:125](C:/Users/nathm/code/Reticle/runtime/agent/dispatcher.go:125) routes and sleeps before registering a cancellable process context. A kill in that interval has no persistent cancellation marker. Contexts are recreated per attempt. Compiler tasks use `compile-exec-...`; queue controls generally target `exec-...`, so killing the visible run does not reliably kill its compiler. Pause stops new graph dispatch but does not suspend already-running work.

Own cancellation at the run level from admission onward and derive all compilation, routing, execution and backoff contexts from it. Explicitly distinguish “pause admission” from process suspension. Test kill before spawn, during routing, during retry delay and during compilation.

### F32 — P1 — Killing a worker is not reliable cleanup of its work

**Source-confirmed.** Runtime `CommandContext` and [Studio process handling](C:/Users/nathm/code/Reticle/studio/electron/forge/process.ts) operate on immediate processes. Shell children, background jobs and work started through a Docker daemon can outlive the killed process. There is no general process-tree/job-object/container ownership contract. Some operations can wait indefinitely because the runtime's default context has no deadline.

Track every resource under the run: Windows Job Object or equivalent process-group handling, container IDs, external job IDs, temporary files and leases. Cancel gracefully, then terminate after a deadline, verify cleanup, and record resources that remain. Go's default cancellation calls the process kill operation; see [os/exec](https://pkg.go.dev/os/exec).

### F33 — P1 — Retry policy is inconsistent, expensive and unsafe for side effects

**Source-confirmed.** [dispatcher.go:119](C:/Users/nathm/code/Reticle/runtime/agent/dispatcher.go:119) defaults to 15 attempts, while Python completions often retry seven times with waits up to 120 seconds. There is no shared wall-clock/token/spend budget. `max_retries` is accepted only as a Go `int` from selected memory; many configuration paths will not supply that type/key. A value of zero or less skips the loop and dereferences nil `lastFailure` at [line 250](C:/Users/nathm/code/Reticle/runtime/agent/dispatcher.go:250). Retrying a whole side-effecting worker can repeat writes or deployment actions.

Validate configuration; retry at one appropriate layer; classify permanent, transient and uncertain side-effect outcomes. Use attempt IDs, idempotency and a total budget. Stop immediately on cancellation or exhausted validation failures.

## Model routing

### F34 — P2 — “Bayesian confidence” is an uncalibrated moving average and fallback bypasses it

**Source-confirmed.** [router.go:178](C:/Users/nathm/code/Reticle/runtime/routing/router.go:178) computes an EMA with alpha 0.2 and optimistic initial 0.90. It is not the add-0.05/halve rule described in RFC-040, nor a Bayesian posterior. Selection fallback ignores the confidence threshold and can choose the same failed model again. Process/artifact success is the learning signal, so incomplete but well-formed specialist output can improve the score.

Rename the current score honestly or implement an evaluated probabilistic model. Track task class and verified outcome, separate transport failures from quality, exclude failed candidates within a bounded attempt policy, and expose when constraints are relaxed.

### F35 — P1 — Routing capacity accounting leaks and cooldown can undo user choices

**Source-confirmed.** [router.go:122](C:/Users/nathm/code/Reticle/runtime/routing/router.go:122) returns before releasing in-flight entries when Bayesian updates are disabled; empty-provider entries are not decremented. Killed tasks can bypass the update path. Forced tracking expects `Model.Key()` but receives bare model IDs from the dispatcher. Router event listeners assert plain strings while worker event values use named Go ID types; the dispatcher's direct updates mask this in common paths, but event-driven accounting is not dependable.

[PenalizeProvider](C:/Users/nathm/code/Reticle/runtime/routing/router.go:373) re-enables matching models after cooldown without preserving manual/permanent-disabled reasons. The embedded UI's `update_settings` message is absent from the server command allowlist, so the intended routing toggle is itself not reliably applied.

Use a lease released in every terminal path, independently of learning. Separate user-disabled, incompatible and temporary-cooldown state. Test forced routes, cancellation, disabled learning and manual disable during cooldown.

### F36 — P2 — Catalog heuristics and fallback do not establish model capability or cost

**Source-confirmed.** [models.go:40](C:/Users/nathm/code/Reticle/runtime/routing/models.go:40) infers capability from name fragments and a size regex that does not normalize million-versus-billion units. Price/capability values are inconsistent across providers, and output-token cost is not a complete selection input. Final generic fallback can choose a non-image model for an image requirement. Default entries may be used when no functioning provider credentials/catalog were established. Discovery errors and malformed/non-200 responses are not consistently handled or closed, and telemetry reads/toggles the shared catalog outside its usual lock.

Validate tool use, modality, context, endpoint and credentials explicitly; keep required capabilities as hard constraints. Normalize units and separate real prices from unknown/free assumptions. Reserve model routing for actual model tasks, not deterministic scaffolding or approval waits.

### F37 — P1 — Custom Ollama discovery does not reliably configure worker inference

**Source-confirmed integration mismatch.** [models.go:314](C:/Users/nathm/code/Reticle/runtime/routing/models.go:314) discovers through `OLLAMA_HOST` but stores that environment variable in the API-key field. Worker completion calls use the routed value as `api_key`, not an explicit matching `api_base`. Discovering models at a nondefault endpoint therefore does not establish that execution reaches that endpoint. LiteLLM documents the Ollama endpoint through `api_base`: [Ollama provider documentation](https://docs.litellm.ai/docs/providers/ollama).

Represent endpoint separately from credential reference and test discovery/inference against the same local fake endpoint. Verify actual library-version environment fallback before relying on it.

## Queue, sessions, environments and credentials

### F38 — P1 — Persisted queue IDs reset and session storage can collide

**Source-confirmed.** [waitlist.go:290](C:/Users/nathm/code/Reticle/cmd/forge/waitlist.go:290) emits `exec-%03d`, while [line 367](C:/Users/nathm/code/Reticle/cmd/forge/waitlist.go:367) restores the counter with `wl-%d`. Existing entries do not advance the counter after restart. Independently created workspace queues also start at `exec-001`, but sessions and containers are named in a common root from that ID. New work can overwrite or inherit another run's files and UI history.

Use globally unique persistent run IDs, with workspace/session identity as explicit fields. Migrate old queue references and reject existing-directory collisions. Test two workspaces, a restart and imported state.

### F39 — P1 — Queue grouping can mix unrelated tasks and ignore sequential mode

**Source-confirmed.** [waitlist.go:406](C:/Users/nathm/code/Reticle/cmd/forge/waitlist.go:406) enforces sequential mode only for nonempty groups. Studio submits an empty group, so selecting sequential there does not enforce serialization. History/file inheritance compares group equality even when both groups are empty, so unrelated default tasks inherit previous completed or failed tasks. Paused items are omitted from running counts even though their active workers may continue, releasing capacity prematurely.

Make continuation relationships explicit and opt-in. Define sequential behavior without relying on hidden group values, and account separately for admitted runs and live resource usage. Test unrelated prompts, sequential submissions and pause under load.

### F40 — P1 — Queue recovery, removal and error handling can strand or orphan work

**Source-confirmed.** [waitlist.go](C:/Users/nathm/code/Reticle/cmd/forge/waitlist.go) allows removal of a running item without cancelling its work. Persisted running items become failed after load, paused items lack a complete recovery path, and pending recovery is not automatically pumped at initialization. A workflow-load failure in the compilation-completion path sets failed status without consistently pumping the next item. Live item pointers are also published for asynchronous serialization after state may change.

Make queue transitions transactional and emit immutable snapshots. Define remove as dismissing history only or as a cancel-and-confirm operation. Reconcile persisted resources and schedule eligible pending work on startup. Every terminal branch must release capacity exactly once.

### F41 — P1 — Compiler context and global execution settings are silently dropped

**Source-confirmed.** [main.go:232](C:/Users/nathm/code/Reticle/cmd/forge/main.go:232) builds available-agent text before loading compiler built-ins; the root agent directory supplies no corresponding runnable manifests. Architect can be told to create everything while being instructed to inject HitL. Its required memory includes prompt/available agents but not the uploaded context that queue code publishes. The dispatcher injects declared keys plus only `user_prompt` and `workspace_dir` as universal keys at [dispatcher.go:87](C:/Users/nathm/code/Reticle/runtime/agent/dispatcher.go:87).

Consequently attachments, IDE context, history, effort or native-execution settings can exist in memory without reaching the agent that needs them. `agent_complexity` is retained in queue data without a complete effective propagation path. Publishing global `allow_native_execution` does not guarantee every worker receives it.

Define a typed task context and explicit configuration precedence. Validate all user-facing controls end-to-end; show effective settings, not merely submitted values.

### F42 — P1 — Compiled workers are registered globally instead of per execution

**Source-confirmed.** [waitlist.go:105](C:/Users/nathm/code/Reticle/cmd/forge/waitlist.go:105) loads session-generated agents into a shared registry and rebuilds worker mappings. Definitions are keyed by agent ID, not execution/version. Two runs choosing the same generated agent ID can replace one another's executable path; rebuilding all workers also repeats provisioning for unrelated definitions. Shared registry/worker-map access is not consistently synchronized with dispatch.

Resolve an immutable agent-version binding during admission and attach it to the execution. Keep generated files and dependencies run-scoped or content-addressed. Never replace another live run's worker binding by a display-name collision.

### F43 — P1 — Startup paths and cleanup can damage or misplace user work

**Source-confirmed; cleanup was not executed.** [main.go:164](C:/Users/nathm/code/Reticle/cmd/forge/main.go:164) automatically retains only three legacy workspaces, independent of whether an older workspace was explicitly requested. Paths depend on starting in `cmd/forge`; later `os.Chdir(workspaceDir)` changes the meaning of further relative `../../` paths. Fresh cleanup lacks cross-process ownership protection. The interactive `exit` command breaks the read loop but the following normal-EOF path still waits for an interrupt signal, contradicting “Shutdown Orchestrator.”

Resolve roots once and pass absolute paths explicitly. Make retention recoverable and ownership-aware; protect active/selected workspaces. Use one orderly shutdown path for exit, EOF and signals.

### F44 — P2 — File inheritance and completion ignore I/O failures

**Source-confirmed.** [waitlist.go:470](C:/Users/nathm/code/Reticle/cmd/forge/waitlist.go:470) ignores open/create/copy errors in inheritance; invalid file handles and partial copies are possible. Completion is announced before all output copying/cleanup is verified, and helper copy routines do not consistently report all failures. A green queue entry does not establish that deliverables were preserved. F02 describes the more serious attachment-deletion variant.

Stage copies, verify all errors, commit the result directory atomically where possible, and include artifact persistence in success criteria. Distinguish cleanup warnings from missing deliverables. Test disk-full, permission and interrupted-copy cases.

### F45 — P1 — Dependency provisioning is mutable and disconnected from command execution

**Source-confirmed.** [environment.go:30](C:/Users/nathm/code/Reticle/runtime/agent/environment.go:30) uses a shared base Python environment with unpinned base dependencies and per-agent `--target` directories. Agent-library installation is outside the base mutex and has no cross-process coordination. Cache identity does not fully encode interpreter/platform/locked dependency versions; replacing a requirements list can leave stale target packages. Provisioning failures skip the affected worker while overall setup continues without failing admission. The registry advertises a Go runtime type without a complete corresponding worker-build case.

`PYTHONPATH` affects the worker, not necessarily a native shell's Python or the separate Docker image. Use immutable, tested environment identities and execute tools through their declared environment. Build failures must make an agent unavailable with a clear reason.

### F46 — P1 — Workers inherit broad credentials and logs retain sensitive task data

**Source-confirmed exposure paths; no credentials read or leak attempted.** [worker.go](C:/Users/nathm/code/Reticle/runtime/agent/worker.go) inherits the parent environment. Generated code and shell-capable workers can therefore access unrelated provider/cloud credentials available to Forge. [orchestrator.go](C:/Users/nathm/code/Reticle/runtime/orchestrator/orchestrator.go) suppresses heavy artifact data selectively but logs other task/memory payloads; full stderr is buffered/logged again. Prompts, input text or secret-valued memory can persist in logs. Logs have no complete rotation/retention/redaction policy.

Pass credential references through narrowly scoped adapters and allowlist environment variables. Redact before logs/events cross process boundaries, cap retained output, and define retention. Do not claim this audit found a committed secret or observed exfiltration; it found exposure mechanisms.

## Studio and telemetry reliability

### F47 — P1 — Studio cannot pass its required build type check

**Reproduced.** [status.ts:45](C:/Users/nathm/code/Reticle/studio/src/design/status.ts:45) declares `Record<ExecutionStatus, NodeStatus>` but omits `PAUSED`, causing TS2741. `build` runs `tsc -b` first and `package` runs build, so this blocks the declared release pipeline. Runtime fallback in the lookup does not satisfy the type contract.

Define the intended paused display state and make lifecycle mappings exhaustive. Verify typecheck/build after repairing both the type error and F48's missing reducer behavior.

### F48 — P1 — Studio's run projection ignores pause and kill

**Reproduced.** [projection.ts:45](C:/Users/nathm/code/Reticle/studio/src/shared/projection.ts:45) has no paused/cancelled run states and does not reduce the corresponding execution events. A started run remains `running` after both `ExecutionPaused` and `ExecutionKilled` in the saved fixture. This is distinct from the type-check failure. HitL waiting detection also depends on stderr markers the existing HitL worker writes to stdout.

Share a versioned lifecycle contract with the runtime and test every state transition in the projection. Distinguish requested cancellation from confirmed termination and pause from waiting for approval.

### F49 — P1 — Studio history is neither fully recoverable nor session-isolated

**Reproduced and source-confirmed.** [projection.ts:478](C:/Users/nathm/code/Reticle/studio/src/shared/projection.ts:478) replaces the session label but keeps runs keyed only by execution ID. An old node survives a new session's `exec-001` in the fixture. [replayTo:518](C:/Users/nathm/code/Reticle/studio/src/shared/projection.ts:518) stops at the first ID above the requested cutoff; an eligible later-delivered event is omitted. The bus does not guarantee that ordering. [store.ts](C:/Users/nathm/code/Reticle/studio/electron/forge/store.ts) keeps event/log rings in memory, despite “durable” wording; reconnection only receives a partial backend snapshot. The projection can continue growing even while old events are evicted.

Use global run identity, durable state snapshots plus ordered cursor replay, and an explicit retention horizon. Indicate incomplete history rather than reconstructing certainty from missing events. Test disconnect, restart, eviction and out-of-order delivery.

### F50 — P2 — Packaged Studio cannot reliably repair a missing repository root

**Source-confirmed; installer not run.** [paths.ts](C:/Users/nathm/code/Reticle/studio/electron/paths.ts) caches root discovery, including null. `setRepoRoot` exists but is not wired to settings updates in [main.ts:253](C:/Users/nathm/code/Reticle/studio/electron/main.ts:253). Selecting a Forge cwd does not consistently rebind readers, keys and agent paths. [package.json](C:/Users/nathm/code/Reticle/studio/package.json) packages Studio distributions without the Forge runtime/worker assets, so detached installations need a correctly configured external checkout.

Choose a supported packaging model and implement one explicit root selector that updates all services. Test a packaged app installed outside the repository with no `RETICLE_ROOT`, then select a checkout and start Forge successfully.

### F51 — P1 — Electron trust boundaries are incompletely enforced

**Source-confirmed; exploitation not attempted.** Studio enables context isolation, renderer sandboxing and disables Node integration, which are good controls. However [main.ts:97](C:/Users/nathm/code/Reticle/studio/electron/main.ts:97) passes window-open URLs directly to `shell.openExternal`, unlike the separate IPC method's HTTP(S) filter. Navigation is not comprehensively denied and privileged IPC handlers do not validate sender origin/frame or runtime payload schemas. TypeScript annotations do not validate IPC data. [paths.ts](C:/Users/nathm/code/Reticle/studio/electron/paths.ts) checks lexical containment without resolving symlinks/junctions; generic workspace reads can also reach `.env` inside the root, bypassing the intended reveal-specific API.

Centralize sender validation, operation schemas, URL policy and canonical filesystem authorization. Electron's own guidance calls out sender validation and untrusted external URLs: [Electron security guidance](https://github.com/electron/electron/blob/main/docs/tutorial/security.md).

### F52 — P2 — Socket and process ownership can become inconsistent

**Source-confirmed.** [client.ts:48](C:/Users/nathm/code/Reticle/studio/electron/forge/client.ts:48) suppresses duplicate connect only for an OPEN socket; repeated connect while CONNECTING creates another. Old socket callbacks can clear or alter the newer connection because they do not check identity. [process.ts:116](C:/Users/nathm/code/Reticle/studio/electron/forge/process.ts:116) records spawn error without immediately clearing `child`; failed spawn need not emit the normal exit event used for cleanup, leaving restart blocked. Studio always supplies `-native`, although effective propagation is inconsistent as described in F41.

Model connecting/running/closing resources explicitly, guard callbacks by owned instance, use close/failure cleanup, and make native execution an explicit effective setting. Test repeated connect, target change during handshake, nonexistent/nonexecutable binaries and restart after failure.

### F53 — P2 — Concurrent key edits can lose updates

**Source-confirmed.** [keys.ts](C:/Users/nathm/code/Reticle/studio/electron/env/keys.ts) performs read-modify-write operations using a shared temporary filename without serializing edits. Two IPC requests can each read old content and overwrite one another or collide on the temporary file. Atomic rename alone does not make the whole update transactional.

Serialize key mutations, use unique staging names and restrictive permissions, and preserve unrelated keys/comments. Test concurrent set/remove operations using a fixture `.env`; no real credential file is needed.

### F54 — P2 — Uploads and output endpoints are unsuitable for large ML artifacts

**Source-confirmed.** [Studio rest.ts](C:/Users/nathm/code/Reticle/studio/electron/forge/rest.ts) reads selected files wholly into memory and creates a Blob without preserving a reliable image MIME type. The server's image branch depends on that type. [server.go:114](C:/Users/nathm/code/Reticle/runtime/telemetry/server.go:114) uses `ParseMultipartForm(50 << 20)`, which limits multipart memory usage, not total request size; see [Go net/http](https://pkg.go.dev/net/http). There is no complete body cap, staging expiry or streaming policy. [server.go:71](C:/Users/nathm/code/Reticle/runtime/telemetry/server.go:71) recursively reads output files as strings into JSON, including potentially huge binary checkpoints.

Stream uploads/downloads with explicit limits, preserve validated MIME metadata, paginate file listings and expose binary artifacts by reference. Validate execution IDs and Windows path separators. Do not infer that ordinary slash traversal bypasses Go's HTTP path normalization without a dedicated test.

### F55 — P2 — Slow telemetry clients can stall orchestration

**Source-confirmed.** [server.go:257](C:/Users/nathm/code/Reticle/runtime/telemetry/server.go:257) broadcasts synchronously to clients while holding its mutex, without write deadlines. Because it is a wildcard bus observer, one slow client can block delivery and eventually publishers when the queue fills. HTTP server timeouts and WebSocket message-size limits are absent. Bind failures are printed asynchronously even though `Start` can already report success.

Give clients bounded outbound queues, deadlines and disconnect policies; never block authoritative execution on telemetry. Return startup failures synchronously and configure header/read/write/idle limits. Test a client that connects and stops reading.

## Tests, documentation, skills and repository consistency

### F56 — P1 — The examples and automated verification do not establish a working release

**Reproduced.** Examples 01–04 call `telemetry.NewServer` and `routing.NewRouter` with obsolete two-argument signatures; both now require three. All four fail compilation. No tracked Go `_test.go` exists; `.github/workflows` and `tests` are placeholders. Several root/Forge `test_*.py` files are top-level provider experiments rather than isolated assertions and can call external services on execution/import. They were not blindly run.

Create offline integration tests that use fake workers/providers, compile all modules, and execute generated-worker contract fixtures. Add opt-in live tests with bounded costs and separate credentials. A local `go test` pass currently mostly proves compilation, not runtime behavior. ML examples should clearly distinguish synthetic smoke data from real benchmark evaluation.

### F57 — P1 — Advertised schemas are empty

**Reproduced by file census.** [agent.schema.json](C:/Users/nathm/code/Reticle/schemas/agent.schema.json), [task.schema.json](C:/Users/nathm/code/Reticle/schemas/task.schema.json), and [plugin.schema.json](C:/Users/nathm/code/Reticle/schemas/plugin.schema.json) are zero bytes. They cannot validate manifests or support editor tooling. This helps explain incompatible specialist definitions and mismatched task assumptions, though empty files alone are not the sole cause.

Implement versioned schemas backed by runtime validation and generated language types. Validate all committed/generated examples and fail on unknown or unsupported fields.

### F58 — P2 — README commands and core execution claims are wrong

**Source-confirmed.** [README.md:46](C:/Users/nathm/code/Reticle/README.md:46) uses `-auto-approve`, but the CLI does not define that flag. The command fails argument parsing. Prompt substring bypass is a separate unsafe mechanism, not that CLI feature. [README.md:11](C:/Users/nathm/code/Reticle/README.md:11) claims `file-writer-node` is automatically injected into all compiled workflows; scaffolding emits a helper file without consistently registering/inserting that node. “JSON-RPC over STDIO” describes a custom JSON/line protocol lacking a JSON-RPC envelope/method contract.

Replace commands with tested invocations and describe the actual wire protocol and file-writing path. Broad integration claims need an implemented adapter and a working example, rather than inference from using JSON.

### F59 — P2 — Agent guides describe roles and paths that the implementation does not provide

**Source-confirmed.** [custom_agent_guide.md](C:/Users/nathm/code/Reticle/docs/custom_agent_guide.md) places an agent manifest under `compiler/agents` with `workers/...` entrypoint, but instructs placing the worker under `compiler/workers`; manifest-relative resolution points elsewhere. [core_agents.md](C:/Users/nathm/code/Reticle/docs/core_agents.md) describes coder as the coding worker and writer as a technical author, while the implemented coder generates workers and [writer.py](C:/Users/nathm/code/Reticle/cmd/forge/compiler/agents/writer-agent/workers/writer.py) writes supplied file maps. The writing example asks this file writer to author prose. Several specialist listings also omit their unregistered/incomplete status.

Document generators separately from executable task workers and file aggregators. Test the custom-agent tutorial verbatim in a temporary project. Derive role/capability/runnable tables from validated manifests.

### F60 — P2 — RFCs and troubleshooting disagree with current behavior

**Source-confirmed; document status matters.** The following are concrete divergences, not an instruction to implement every draft:

| Document | Contradiction |
|---|---|
| RFC-027 Worker Runtime Contract, Stable | Read-to-EOF and optional artifact conflict with F25/F26 |
| RFC-026 Worker Stdout Protocol, Draft | Describes last-valid output and fallback behavior unlike current first-artifact/failure handling |
| RFC-032 Go Memory Bus State Transfer | Describes broad memory transfer unlike declared-key plus base-key injection |
| RFC-033 Isolated Session Workspaces, Accepted | Locking/strict isolation claims exceed F04/F27/F38/F42 |
| RFC-040 Bayesian Model Routing Matrix | Update rule differs from the EMA and terminology overstates statistical meaning |
| RFC-041 Agent Execution Lifecycle Control | Process/kill/pause guarantees exceed F23/F31/F32/F48 |
| RFC-042 Native ComfyUI Integration | Host/catalog/lifecycle integration is incomplete, F17 |
| `docs/troubleshooting.md` | Retry counts and token settings drift; some suggested complexity controls are ineffective |

The troubleshooting claim that Groq GPT-OSS generally lacks tool use is too broad. Current official tables distinguish GPT-OSS tool support from Compound's different tool model: [Groq tool-use overview](https://console.groq.com/docs/tool-use/overview). The shelved RFC-030 is not treated as a promised implemented deployment. Historical audit/planning documents should retain their dates and mark findings resolved, superseded or still open; the previous audit's implication that a slice race necessarily causes Go's concurrent-map panic is inaccurate.

Link each normative requirement to a test and implementation version. Make status machine-readable and identify historical observations visibly.

### F61 — P2 — Governance standards and templates cannot enforce their stated process

**Source-confirmed/census.** Several standards are empty or only headings; [AGENT_STANDARD](C:/Users/nathm/code/Reticle/docs/standards/007%20AGENT_STANDARD.md) is short evolution guidance rather than a manifest/agent contract, and [SKILL_STANDARD](C:/Users/nathm/code/Reticle/docs/standards/008%20SKILL_STANDARD.md) does not define a usable standard. Multiple templates, docs indexes and diagrams are zero-byte placeholders. Governance refers to specification locations that are absent, and metadata casing/fields differ between the written standard and documents. `LICENSE` and `CONTRIBUTING.md` are empty; they do not provide the information their filenames imply.

Finish a minimal enforceable standard, label intentional placeholders and remove dead navigation claims. Add schema/link checks. The empty license is a missing project artifact; this report makes no legal conclusion about permissions.

### F62 — P2 — Skills are not automatically loaded into agent instructions as documented

**Source-confirmed.** [specialized_skills.md](C:/Users/nathm/code/Reticle/docs/specialized_skills.md) describes agents reading skill instructions automatically. [registry.go:102](C:/Users/nathm/code/Reticle/runtime/agent/registry.go:102) loads YAML skill metadata/dependencies, but there is no universal instruction-content injection. Some generated workers have optional file-reading tools; that does not ensure a referenced skill is actually read. Specialist prompts duplicate only selected rules, creating the DevOps/ML inconsistencies above. Several standalone skill directories have no registry wrapper or runtime hook integration.

Define whether a skill is a manual document, an injected instruction module, an environment bundle or a runtime capability. Validate and report the effective loaded content/version. Missing wrappers are an integration gap when automatic use is promised, not proof that every manual skill is intrinsically invalid.

### F63 — P2 — Compaction and learning hooks do not perform their advertised lifecycle functions

**Source-confirmed.** [suggest-compact.sh:31](C:/Users/nathm/code/Reticle/skills/strategic-compact/suggest-compact.sh:31) uses `/tmp/claude-tool-count-$$`. Separate hook invocations normally have different process IDs, so each starts its own counter and never accumulates toward the threshold; files also lack a session cleanup policy. [evaluate-session.sh:58](C:/Users/nathm/code/Reticle/skills/continuous-learning/evaluate-session.sh:58) emits a suggestion to evaluate a session; it does not extract or persist learned patterns. Configured extraction/approval options do not implement that missing behavior. These hooks target a different host's session conventions and are not wired into Reticle's lifecycle.

Either label these as manual/reference integrations or implement stable run/session keys and explicit hook invocation. Keep learned guidance proposed and versioned until validated, with provenance and revocation; do not imply autonomous learning has already occurred.

### F64 — P3 — Smaller inconsistencies and unfinished surfaces accumulate maintenance risk

**Source-confirmed/census; lint items remain warnings.** These should be tracked after the blockers:

- Root `src/features/runs/Composer.tsx` and `src/features/editor/FileViewer.tsx` duplicate Studio paths but are outside its build configuration. Editing them does not update the application.
- Studio's environment badge checks an agent-named directory while provisioning creates `<agent>-libs`, so a provisioned agent can appear unprovisioned. See [reader.ts](C:/Users/nathm/code/Reticle/studio/electron/workspace/reader.ts).
- The current embedded UI checks `items.length` before its null guard; a null payload can fail rendering. Dynamic graph IDs are also inconsistently escaped in HTML attributes.
- Embedded old UIs, scratch generators and one-off patch scripts are retained beside maintained code without a clear supported/archived boundary. Some patch/probe scripts have top-level effects; they should not be discoverable as normal tests.
- Python helper strings contain invalid escape sequences that already generate warnings; future interpreter behavior may become stricter.
- Studio lint reports effect-driven state updates and a React-compiler compatibility warning around virtualization. Review each for actual stale-state/render behavior; do not mechanically equate the 12 warnings with 12 bugs.
- Comments describe the socket allowlist as enqueue/remove only, although the server also accepts lifecycle actions. Other comments call in-memory state durable and unimplemented lock grants serialized. Comments should reflect contracts, not aspirations.
- Statistics skill guidance should explain test assumptions, estimands and alternatives more carefully; automatically substituting a rank-based test changes the question being tested in some cases. This is a recommendation for methodological precision, not a demonstrated incorrect analysis produced by this repository.
- Empty diagrams/templates and misspelled planning filenames make discovery harder. Consolidate index pages and preserve redirects/references when renaming.

## Immediate repair order

1. Close the unauthenticated control and unsafe attachment/file paths; replace text-derived approval authority.
2. Establish validated manifests and one working worker contract; repair generator failures and Studio/example build blockers.
3. Repair execution state ownership, result commits, identity and cancellation before expanding concurrency.
4. Introduce reproducible environments and capability-specific DevOps/ML execution with verified outcomes.
5. Add offline regression gates, then update documentation from tested behavior.

The companion report expands these steps into an architecture and acceptance criteria. No production source was modified during this audit; all added files are under `docs/audit/2026-09-08/`.
