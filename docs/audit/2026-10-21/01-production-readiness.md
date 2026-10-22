---
status: audit
owner: Reticle Project
updated: 2026-10-21
---

# Reticle production readiness and competitive audit

Baseline: `ca04c9e`, 17 October 2026. Review performed 21 October 2026. This is an assessment of the local checkout, with focused code inspection, offline tests and disposable probes. It is not a complete penetration test, dependency vulnerability scan, live-provider benchmark or certification. No production code was changed. The pre-existing untracked `test_list_models.py` was not read, modified or executed.

## 1. Assessment

**Reticle is a substantial development-stage, local-first orchestration system. It is not yet a production-ready general coding harness, and the repository does not provide evidence that it outperforms current alternatives.** Its strongest potential differentiator is auditable, local-first work across several models and specialist workflows, with visible execution state, explicit human checkpoints and recoverable work.

The immediate bottleneck is dependable execution and verification. More tools, skills and rules can help, but they cannot compensate for a permission that is enforced only in an SDK, a false verification result, or context lost between attempts. The most valuable next investment is a coherent act-observe-repair loop with enforceable policies and measured outcomes.

Reticle already has useful foundations:

- Go-owned workflow validation, bounded DAGs, attempt identities, cancellation, route health and bounded retries.
- Local durable workflow/memory snapshots, duplicate-result suppression, restart interruption and explicit reconciliation.
- A loopback, authenticated control plane; an attempt-scoped tool broker; local stdio MCP; validated local plugins.
- Scoped SDK tools, paged file reads, atomic single-file editing, explicit overwrite checks, static page checks, actual browser observation and specialist workers.
- Studio projections, artifacts, logs, workspace views and hash-bound human approval requests.
- A meaningful contract-test suite and an unusually useful record of earlier audits and design decisions.

These are implemented mechanisms, not proof of end-to-end task quality. In particular, a successful worker process is not the same as a correct deliverable.

### Documentation authority used throughout

The order in [the documentation index](../../README.md) was followed: versioned specifications, current operational guides, accepted ADRs/RFCs, then historical planning and dated audits. Earlier repaired findings were not automatically treated as still open.

The principal anchors were [runtime orchestration](../../specifications/runtime-orchestration.md), [worker protocol v1](../../specifications/worker-protocol/v1/001-protocol.md), [agent definitions](../../specifications/agent-definition/v1/001-schema.md), [workflow definitions](../../specifications/workflow-definition/v1/001-schema.md), and RFCs [044](../../rfc/RFC-044-Memory-Lifecycle-Recovery-Evaluation.md), [045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md), [050](../../rfc/RFC-050-Attempt-Scoped-Tool-Broker.md), [052](../../rfc/RFC-052-Request-Level-Inference-Recovery.md), [053](../../rfc/RFC-053-Run-Page-Browser-Observation.md), [054](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) and [055](../../rfc/RFC-055-Specialist-Agents.md). The September [repair ledger](../2026-09-18/02-repair-status.md) and [older remaining limitations](../2026-09-08/03-repair-status.md) were rechecked against current code.

## 2. Findings requiring attention

Priority meanings: **P1** should block a general production-readiness claim; **P2** should be resolved in the next hardening cycle. Priority reflects product impact, not a CVSS assessment. Reproduced means a local fixture exercised the behavior; source-confirmed means the relevant path was inspected but a full integration was not run.

### A01 — P1: Final worker responses bypass capability checks

**Reproduced.** `runtime/agent/worker.go:299` checks memory scope but does not require `memory.execution`. Its graph-response handling at lines 308 and 344 does not require `graph.delegate`. The graph engine at `runtime/agent/workflow_engine.go:236` checks attempt identity, target existence and node budget, but not the originating worker's grant or an allowed delegation target set.

A real child-process fixture with only `workspace.read` successfully returned a memory mutation and a delegation. Its memory write was accepted and the graph grew from one node to two. The fixture supplied no verification evidence and was also accepted.

This is a protocol enforcement gap, not a claim that an ordinary SDK tool call can freely add permissions. A buggy or custom worker can exercise authority that the SDK hides. Delegation can select another registered worker with its own broader manifest.

**Documentation:** [RFC-045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md) and [agent definitions](../../specifications/agent-definition/v1/001-schema.md) make capabilities meaningful runtime grants; [RFC-050](../../rfc/RFC-050-Attempt-Scoped-Tool-Broker.md) says calls cannot expand authority. The broker checks grants, but the final-response path is a separate channel.

**Repair:** authorize every mutation in Go before committing anything. Distinguish implicit result storage from an explicit model-requested memory mutation. Check delegation capability, target policy and execution scope; reject the whole unauthorized response. Add adversarial worker-protocol tests that bypass the SDK.

### A02 — P1: Explicitly empty capabilities grant broad compatibility permissions

**Reproduced.** `runtime/agent/registry.go:190` treats `len(capabilities)==0` as the legacy default, conflating an omitted field with `capabilities: []`. The probe received nine grants, including workspace writes, network, container/native process access, memory and delegation. Native commands still require the user's separate native setting.

There is a second boundary to fix: `Task.Capabilities` uses JSON `omitempty`, and `worker_sdk.run` selects its compatibility profile when the task field is absent. Fixing only registry parsing would therefore be insufficient for a deliberately empty task grant.

**Documentation:** the [agent specification](../../specifications/agent-definition/v1/001-schema.md) and [September repair ledger](../2026-09-18/02-repair-status.md) deliberately preserve compatibility for *omitted* capabilities. They do not establish an explicit empty list as a request for broad authority.

**Repair:** preserve omitted versus empty values through manifest parsing and task serialization. Keep legacy behavior behind a visible compatibility mode; new manifests and restrictive profiles should fail closed.

### A03 — P1: RAG silently indexes zero files in the normal session layout

**Reproduced.** `cmd/forge/compiler/lib/rag_tools.py:43` excludes a file if *any absolute path component* is `.reticle`. Forge creates sessions under `.reticle/sessions/<execution>` (`cmd/forge/waitlist.go:645`). Every normal session file therefore matches the exclusion.

The same one-document fixture produced `Indexed 1 files` in an ordinary directory and `Indexed 0 files` under `.reticle/sessions/fixture`. Both returned a success-like response. No embedding service was required to reproduce this; the collection was an in-memory fixture.

**Documentation:** [core agents](../../core_agents.md), [specialized skills](../../specialized_skills.md), and the RAG worker contract describe useful indexing/query behavior. The production path defeats that behavior.

**Repair:** evaluate excluded components relative to the selected source root. Warn or fail when a nonempty source unexpectedly yields no eligible files. Exercise the actual session layout in tests, including replacement, deletion and source attribution.

### A04 — P1: Retry safety mistakes effectful commands for inspection

**Reproduced classification.** `worker_sdk.py:696` accepts commands largely by executable basename and first argument. It classifies all of these as inspection:

- `git branch audit-probe` and `git branch -D audit-probe`, which mutate repository state;
- `rg --pre ./helper needle .`, which invokes a preprocessing program;
- `tsc --noEmit --incremental`, which can write build metadata.

These commands were classified, not executed, by the probe. The implementation can consequently advertise `NO_EFFECTS` and permit replay where it should retain an effect record. The issue matters in native mode or a configured image that contains these executables; the default image does not contain all of them.

**Documentation:** [runtime orchestration](../../specifications/runtime-orchestration.md) permits retry based on no-effect/file-only proofs. Its inspection-command exception is stronger than this parser can establish.

**Repair:** use narrow structured command adapters for genuinely side-effect-free operations. Treat arbitrary shell commands as uncertain unless the runtime can establish a stronger guarantee. Do not infer safety from the program name or `--noEmit` alone.

### A05 — P1: Browser containment overclaims network safety; Docker lacks a network policy

**Policy/argv probes reproduced; no network exploit attempted.** `browser_tools.request_allowed` at line 101 allows arbitrary public GET/HEAD/OPTIONS requests, including a URL carrying data in its query. It also permits an unresolved hostname when `_addresses` returns an empty list. Browser DNS resolution occurs separately from the Python check; the cache/check is not an enforced network destination boundary.

Blocking POST does not stop data leaving via URLs, image requests or GET requests with remote effects. This directly undermines the explanation for classifying browser operations as file-only/replay-safe. The same policy is reused by public browsing. Separately, `forge_utils.py:579` starts Docker without a network restriction or an egress proxy; `process.container` does not mechanically narrow command network access to `network.public`.

**Documentation:** [RFC-053's containment and replay explanation](../../rfc/RFC-053-Run-Page-Browser-Observation.md) says nothing can be sent out. [RFC-055](../../rfc/RFC-055-Specialist-Agents.md) inherits the browser policy. The architect accurately mentions container network access, but this is a weaker authority model than a reader may infer from capability names.

**Repair:** enforce destination policy at a network proxy or sandbox boundary, fail closed on unresolved destinations and define which origins/resources are permitted. Separate public retrieval from general network execution. Revisit browser effect classification and test GET-based outbound data, redirects and DNS changes. On Linux, also review the `--no-sandbox` root-user path at `browser_tools.py:413`; do not claim Chromium process isolation there without an outer sandbox.

### A06 — P1: Completion can be certified by a version command

**Reproduced through the SDK loop.** `_meaningful_terminal_verification` at `worker_sdk.py:327` accepts `pytest --version`, `tsc --version`, and `eslint --help`. A mocked model wrote syntactically invalid Python, received a successful `pytest --version` result, and was allowed to complete with that command as its verification evidence.

The broader quality boundary is weak as well: re-reading files satisfies the generic gate; Go validates only the shape of evidence supplied by the worker; `run_page` results are recorded as output files without making browser failures a mandatory completion blocker (`worker_sdk.py:1551`). Static checks are useful but do not establish successful application behavior.

**Documentation:** [worker protocol v1](../../specifications/worker-protocol/v1/001-protocol.md) expressly says availability/version probes are not validation. [September P2-05](../2026-09-18/02-repair-status.md) was repaired, but the current command recognizer leaves this class open. [RFC-054](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) already recommends structured diagnostics.

**Repair:** runtime-owned verification receipts bound to the changed artifact hashes and actual check scope. Reject help/version/list-only commands. Require appropriate task-level acceptance checks, distinguish inspection from validation, and invalidate evidence after edits. Completion should be verified, failed, or explicitly unverified. Feed verified outcomes into routing, not just exit-zero outcomes.

### A07 — P2: Broker JSON Schema validation is only shallow

**Reproduced.** `runtime/toolbroker/broker.go:648` checks top-level required fields, additional properties and primitive types. It ignores enum restrictions, numeric bounds and nested object constraints. A fixture violating `enum`, `maximum`, nested `required`, and nested `additionalProperties` passed validation.

**Documentation:** [RFC-050](../../rfc/RFC-050-Attempt-Scoped-Tool-Broker.md) describes schema and argument admission; the [plugin contract](../../specifications/plugin-manifest-v1.md) accepts an object JSON schema. A tool author can reasonably assume advertised constraints are enforced.

**Repair:** support a documented JSON Schema dialect with a maintained validator, or reject unsupported schema keywords on registration. Domain authorization must still be enforced by adapters; schema validation is not authorization.

### A08 — P2: Plugin skill instructions are not resolved through the registry

**Source-confirmed with a compiler-side fixture; full plugin enablement was not run.** Plugin loading registers YAML dependency metadata (`runtime/plugin/loader.go:65`). `SkillDefinition` carries no canonical instruction path. The compiler at `coder.py:27` always looks under the checkout's `skills/<id>/SKILL.md`.

A plugin-only instruction file outside that directory is therefore not discoverable by the generated worker path. The compiler fixture emitted a missing-instructions warning and succeeded without the skill text. Plugin workers may implement their own instruction loading; this finding concerns assignment of a plugin-contributed skill to a generated agent.

**Documentation:** [extensions](../../extensions.md), [the agent discovery contract](../../specifications/agent-definition/v1/001-schema.md), and [specialized skills](../../specialized_skills.md) connect plugin registration, architect discovery and worker instruction loading. Those paths are not fully joined.

**Repair:** resolve skills by registered ID to a contained package root, version and content digest. Pass the resolved resources to the compiler and worker. Distinguish an intentional dependencies-only skill from a missing required instruction file. Test install → enable → catalogue → assignment → actual prompt.

### A09 — P2: An interrupted file edit leaves a permanent lock

**Reproduced.** `_overwrite_file` and `_rewrite_locked` create `.reticle-lock` files exclusively and remove them in `finally` (`forge_utils.py:189`, `:321`). A process killed or terminated inside the edit cannot execute `finally`. There is no stale-lock recovery path in the checked source.

A disposable child terminated after acquiring the lock. The next normal edit failed with `File exists`. This can obstruct the file-resume mechanism precisely after an interrupted attempt.

**Documentation:** [RFC-045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md), [runtime orchestration](../../specifications/runtime-orchestration.md) and [RFC-054's edit tools](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) aim to preserve useful file work across attempts.

**Repair:** use process-owned OS locks, or runtime leases with attempt ownership and safe crash reconciliation. Never blindly remove another live writer's lock. Cover process termination, cancellation and concurrent editors.

### A10 — P2: External tool effects are not integrated with the durable effect ledger

**Source-confirmed.** An `EffectManager` is created and listens for begin/finish events. Production MCP/plugin/broker call paths do not publish those events or call the manager. The repository-wide call-site search found the effect manager exercised by its tests, but no corresponding adapter integration.

Broker call IDs and cached responses are attempt-local, in-memory records. MCP conservatively returns `uncertain`, which prevents generic replay; that is a useful protection. It does not provide a durable record from which an operator can reconcile a tool that acted just before a crash.

**Documentation:** [RFC-045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md) describes the ledger and also acknowledges adapter work remains; [RFC-050](../../rfc/RFC-050-Attempt-Scoped-Tool-Broker.md) distinguishes uncertain effects from safe retries. This is an integration gap, not evidence that Reticle blindly replays every MCP call.

**Repair:** persist effect intent and completion around effectful broker adapters, including target/request hashes, external IDs, idempotency policy and reconciliation ownership. Test crashes before transmission, after transmission and before result persistence.

### A11 — P2: CI misses compiler tests and important production paths

**Source-confirmed; compiler suite executed.** `.github/workflows/verify.yml:34` discovers only `tests/`. The separate `cmd/forge/compiler/tests` suite contains 28 passing tests but is not discovered by that job. Studio runs only on Linux in CI despite Windows-specific process, path, shell and Electron behavior. There is no checked-in clean-machine installed-application gate or end-to-end task-quality gate in this workflow.

The Go job specifies 1.21 while the README advertises 1.22+ and the local environment is 1.27.1. That is support-policy drift, not proof of a compilation bug. Local race testing was unavailable because CGO is disabled.

**Documentation:** [CONTRIBUTING](../../../CONTRIBUTING.md), the [September validation ledger](../2026-09-18/02-repair-status.md), and [the old coverage matrix](../../diagrams/runtime/test_coverage_matrix.md). The latter still lists nonexistent/pending test paths and is explicitly excluded from one documentation check.

**Repair:** include both Python suites, choose explicit supported toolchain versions, run Windows Studio checks, add provisioned browser/container integration jobs and an installed-app smoke test. Keep external-service tests separately budgeted and credential-controlled.

### A12 — P2: Release packaging and license are incomplete

**Source-confirmed, previously acknowledged.** `LICENSE` is zero bytes while the README displays an MIT badge. `studio/package.json` packages the UI and Electron code; it does not bundle the Go runtime, compiler resources and complete tool environments. `studio/electron/paths.ts` expects a Reticle source checkout.

**Documentation:** [Studio README](../../../studio/README.md) correctly calls the packaged app a checkout companion. The [September ledger](../2026-09-08/03-repair-status.md) already leaves license text and signed/installed acceptance open. These are release limitations, not newly discovered regressions.

**Repair:** the owner should supply the intended license and make claims consistent. Define a supported installation model, runtime/resource discovery, upgrade/rollback, provenance and platform acceptance. A passing Vite build is not an installed product test.

## 3. Architectural limits beyond the individual defects

### Isolation remains an explicit trust decision

Workers are host subprocesses (`worker.go:227`). The current coder generates a small maintained SDK wrapper, rather than arbitrary LLM-written Python worker logic; that substantially narrows the ordinary generated-worker surface. Nevertheless, a process boundary and a virtual environment are not an OS permission boundary. Plugins and MCP servers are trusted local code, as [extensions](../../extensions.md) explicitly states. The selected inference credential is still available to the worker process. A production claim for untrusted/custom workers requires a stronger boundary or an explicit trusted-code product scope.

Follow [RFC-048's existing gate](../../rfc/RFC-048-Meta-Scaffolder-Revised.md): do not add autonomous worker self-modification before isolation, rollback and review exist.

### Persistence is useful but incomplete

[RFC-045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md) correctly acknowledges separate memory/result and execution snapshots, a single-writer assumption and a crash window requiring reconciliation. In-flight work recovers as interrupted, not as a preserved model conversation. There is no transactional outbox or persisted pending-inference continuation in the reviewed path. [RFC-052](../../rfc/RFC-052-Request-Level-Inference-Recovery.md) remains a proposal for fuller recovery; interim SDK retries are not equivalent to it.

For a single-user desktop product, SQLite transactions plus an outbox are a reasonable next design to evaluate. Distributed execution is not a prerequisite. File effects still need checkpoints and adapter-specific reconciliation even with a transactional database.

### Default environments constrain coding quality

The default shell image is Python-oriented. The architect itself warns that it lacks Node, npm and Git (`architect.py:81`). Each command uses a fresh container and only workspace files persist. Python dependencies installed for the host worker do not automatically become tools inside that container.

This prevents ordinary JavaScript application development and testing from being dependable out of the box. Provide prepared, reproducible project environments with the needed runtimes, dependency caches and managed dev servers. Granting more capabilities or adding a React skill cannot supply a missing executable.

### Context, user control and evaluation need a common design

Reticle has selected shared memory and reactive compaction. It does not yet have the full user-facing session continuity, steering, checkpoint/rewind and accepted cross-run project memory described in [RFC-054](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md). `_drop_old_turns` can replace earlier work with a note listing files, losing decisions and unresolved constraints. [RFC-049](../../rfc/RFC-049-Memory-Query-Management.md) concerns a proposed memory UI; it should not be mistaken for a complete memory-learning product.

Skills are mostly appended in full at compile/startup time. No general `AGENTS.md`/`RETICLE.md` instruction-resolution path was found in the inspected runtime/compiler. Tools need on-demand discovery, and skills need resource resolution, applicability tests and versioned provenance.

Model metadata and routing already include price/capability estimates, but no execution-wide spend reservation/stop mechanism or normal SDK token-usage ledger was found. The worker loop does not turn actual usage into task cost receipts. `tools/eval_memory_quality.py` is useful but narrowly tests selected-memory answering; its offline mode validates prompt construction, not real task success. The `eval-harness` skill is explicitly unassignable reference material, not an operating benchmark system.

## 4. Documentation reconciliation

The documentation contains the right strategic direction, but several current statements need repair before adding more rules:

| Document | Current discrepancy | Treatment |
| --- | --- | --- |
| Runtime orchestration, lines 40 versus 68–69 | One passage permits file-effects resume after stalls/budget exhaustion; another says those require no-effects proof. Current dispatcher permits both proofs for these cases. | Consolidate into an executable retry decision table. |
| Runtime orchestration, line 77 onward | Says the dispatcher cannot replay once files change, despite the earlier file-effects resume path. | Distinguish request repair, file continuation and unsafe-effect recovery. |
| Runtime/SDK comments about writes | Several passages still say `write_file` never overwrites. Explicit overwrite-after-read now exists. | State the exact digest/read condition and avoid a universal idempotency claim. |
| Core agents | Still says Browser/OSINT/Quant/Hermes entrypoints share the generator. | Update to the real specialists accepted in RFC-055. |
| RFC-053 and browser tool descriptions | Claim nothing can be sent out because remote non-GET requests are blocked. | Correct the containment model and its replay consequences; see A05. |
| Test coverage matrix | Lists old nonexistent paths and pending work already implemented. | Replace with generated suite inventory, or clearly archive it. |
| README versus LICENSE | MIT badge, empty license file. | Resolve owner intent and release metadata. |
| RFC-054 | Correctly distinguishes draft gaps from built tools, but some older problem paragraphs still describe already-fixed gaps. | Keep a concise implementation-status table tied to tests. |

Do not erase dated audit evidence or change draft RFCs to accepted merely because a related helper exists. Use the [RFC process](../../governance/RFC_PROCESS.md) for frozen-contract changes.

## 5. Verification and confidence

The machine's execution sandbox denies loopback connections and initially supplied an inaccessible temporary directory. Tests were rerun with temporary files inside the writable checkout; network restrictions were left intact. This matters: an `EACCES` socket failure is not evidence of a Reticle socket bug.

| Check | Result | Interpretation |
| --- | --- | --- |
| Runtime `go test -timeout 120s ./...` | Six packages passed; MCP, routing and toolbroker could not complete successfully; orchestrator has no tests | Failed integration cases were blocked by loopback access. |
| Forge `go test ./...` | Passed | Existing compiler CLI/package tests. |
| Runtime and Forge `go vet ./...` | Passed | Static checks. |
| Root Python discovery | 353 tests: 282 passed, 16 failures, 7 errors, 48 skipped | Most failures were browser/socket restrictions; RAG failure reproduced independently; one Docker test has a mock/platform interaction. Optional dependency coverage remained skipped. |
| Compiler Python discovery | 28 passed | Separate suite omitted by current CI. |
| Studio tests | 84 tests: 75 passed, 9 failed | All nine failures reported loopback `EACCES`. |
| Studio typecheck / lint / build | Passed | Lint advisories and large bundle warnings remain. |
| Targeted audit probes | Completed | Confirm the behaviors above; they are characterization probes, not passing security acceptance tests. |

No live model calls, container execution, installed-app acceptance, race-detector run, remote CI run or dependency vulnerability scan was completed. Do not report the full suite as green. The failed-pull mock issue is retained as a test limitation, not promoted to a confirmed Docker product defect.

Evidence: [verification record](evidence/verification.json), [Python observations](evidence/python-probes.json), [Go observations](evidence/go-probes.txt), and [reproduction instructions](evidence/README.md).

The recommended delivery sequence and current-harness comparison are in [the implementation plan](02-implementation-plan.md).
