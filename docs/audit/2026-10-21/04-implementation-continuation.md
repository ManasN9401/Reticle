---
status: draft
owner: Reticle Project
updated: 2026-10-22
---

# Implementation continuation: effects, diagnostics and repository guidance

Subsequent changes and publication blockers are recorded in [the readiness follow-up](05-readiness-and-publication-status.md).

This continues [the implementation plan](02-implementation-plan.md) and [first hardening batch](03-implementation-status.md). It implements additional parts of stages 1-3; the full production roadmap remains open.

## Delivered

| Area | Result | Evidence |
| --- | --- | --- |
| Final-response authorization | A worker's dynamic target is checked before any result commit, as well as at graph admission. An unauthorized target cannot first commit other response memory. | Child-process regression with memory plus denied delegation; full agent package tests |
| A10: effect ledger integration | Forge attaches its persistent effect manager to the broker. Canonical request hashes and full operation identities are saved before adapters run; completion is saved before response. Unresolved equivalent intents block fresh attempt IDs. | In-process HTTP handler regressions, on-disk restart/reconciliation tests, actual MCP fixture through the journal |
| Effect failure containment | Missing durable admission blocks effectful adapters. Completion-persistence failure stops broker admission and publishes the existing fatal persistence event. Adapter panics remain uncertain. Running/prepared snapshots recover as interrupted; missing primary snapshots can recover their previous file. Invalid/null snapshots fail startup. | Broker journal, effect manager, and full runtime tests |
| Operator reconciliation | Authenticated loopback controls expose bounded effect inspection and explicit observed-state reconciliation. Reconciliation never executes a tool. | Protected-handler tests: missing credentials, missing observation, invalid state, unknown fields, valid reconciliation, conflicting terminal observation |
| Structured diagnostics | `run_diagnostics(kind?, path?)` detects npm, Python and Go checks, invokes fixed argument vectors, parses common error/test summaries and saves receipts with task/attempt identity and artifact hashes. Empty suites and changing artifacts cannot verify work. Hashes are rechecked at completion. | Real successful/failing unittest subprocesses, empty-suite/mutation/parser fixtures, worker tool-loop integration |
| Scoped rules | SDK workers load root and scoped `AGENTS.md`/`RETICLE.md`, preserve deterministic precedence, reject aliases/escaping scopes, enforce visible byte budgets and save source metadata. Newly discovered guidance requires review before the first file action. | Scoped precedence, sibling exclusion, oversize/escape and worker review-before-edit tests |
| Network replay classification | Public retrieval and market-data calls, including `read_url`, no longer receive generic file-only replay proof. Corrected the remaining fetch description that claimed requests send nothing. | Specialist worker effect regression |
| Browser support diagnostics | Browser startup errors now include a bounded stderr tail and exit status. Security flags were not weakened to bypass a launch failure. | Bounded-diagnostic regression and one real local launch attempt |

The implementations follow the existing [worker protocol](../../specifications/worker-protocol/v1/001-protocol.md), [RFC-045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md), [RFC-050](../../rfc/RFC-050-Attempt-Scoped-Tool-Broker.md), [RFC-054](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) and [RFC process](../../governance/RFC_PROCESS.md). No frozen schema, protocol framing or event name was replaced and no draft RFC was silently accepted.

Current behavior and limitations are documented in [diagnostics and guidance](../../diagnostics-and-guidance.md) and [external effect recovery](../../external-effect-recovery.md), linked from the documentation index. Historical audit evidence remains unchanged.

## Validation and remaining blockers

- Full `go test ./...` passes in `runtime`, including MCP, routing, broker, agent, plugin and telemetry packages. Forge `go test ./...` passes. Runtime and Forge `go vet ./...` pass. These are ordinary tests, not a claim of a local race-detector run.
- Compiler Python suite: 28 passing tests. Targeted suites pass: diagnostics 6, repository rules 4, worker diagnostics 1, specialists 16, hardening 6, documentation 4. Worker awareness also passes its 12 tests.
- A broad Python run during this continuation exercised 369 tests: 311 passed, 48 skipped, 9 failed and 1 errored. All failures/errors were real Chromium startup cases; no new product failure was converted to a skip. Later additions and refinements were checked with the targeted suites rather than represented as a fresh full-suite pass.
- Loopback-based Go/MCP tests that failed in the earlier execution environment now run successfully. The current browser blocker is different: Chromium exits with code `4294930433`; its startup diagnostics report a failed Crashpad crash-server launch (`TransactNamedPipe`, pipe ended). Real browser/UI acceptance therefore remains unverified here.
- No live paid provider, production MCP service or deployment was invoked. The MCP integration uses the repository's child-process fixture. Docker argv containment is covered by tests, but no clean Node/Python container application acceptance slice is claimed.
- Source checks retain the original unrelated `test_list_models.py` and historical audit evidence. Changes remain local and uncommitted.

## Still open

Diagnostics are SDK-managed evidence, not an independent runtime acceptance oracle. Coverage currently binds tracked pending artifacts; arbitrary shell edits and untracked dependencies need broader workspace receipts. Python lint/typecheck/build adapters, pnpm/yarn/bun, full-output streaming and prepared project toolchains remain outstanding.

The effect ledger is still a single-process snapshot with linear scans and whole-snapshot writes. It has no distributed transaction/outbox, provider-specific automatic reconciliation, retention policy or durable result replay. Local SDK tools do not yet use this broker journal. Conservative uncertainty can require manual reconciliation even for an MCP read unless a safe adapter contract is available. The control API exists; a Studio reconciliation panel does not.

Repository rules are task guidance, not executable permission grants. Literal shell commands do not infer new file scopes. File guidance is bounded, and oversized instruction sets fail explicitly. Validated scopes are retained between turns while small instruction files are re-read and aliases checked; this avoids repeated full ancestor resolution without claiming a measured end-to-end speed gain.

Prepared environments, managed servers, vision routing, multi-file patch transactions, Git checkpoints/rewind, durable conversation and pending-inference recovery, questions/steering/reruns, self-contained packaging, license selection, cost budgets and frozen comparative evaluations remain to be delivered. Network-enabled containers and browser public-request egress still lack the OS enforcement needed to close A05. Reticle is not yet established as production-ready or superior to competing harnesses.
