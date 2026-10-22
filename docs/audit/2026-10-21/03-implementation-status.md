---
status: draft
owner: Reticle Project
updated: 2026-10-21
---

# Implementation status

For subsequent changes and current validation, see the [22 October continuation](04-implementation-continuation.md). This file records the first batch.

This records implemented changes against [the implementation plan](02-implementation-plan.md), rather than treating the entire production roadmap as complete. The original [audit](01-production-readiness.md) and its evidence remain historical observations. No release or comparative performance claim is made.

## Hardening changes

| Finding | Implementation | Remaining limits |
| --- | --- | --- |
| A01: final-response authorization | Explicit memory mutations require `memory.execution`; graph mutations require `graph.delegate`, checked before any result commit. Dynamic targets must fit the source worker's capabilities, MCP servers and broker policies. Return-to-supervisor uses the actual graph source. | Trusted host workers and plugins are still trusted executable code. Graph rejection is logged; a richer acknowledged delegation response remains future work. |
| A02: empty capabilities | Only omitted manifest capabilities receive compatibility defaults. Task serialization always emits the grants array, including `[]`; SDK null/empty grants deny all. Registry and dispatch copies preserve explicit empty arrays. | Omitted legacy manifests still intentionally receive the v1 compatibility profile. |
| A03: RAG paths | Exclusions inspect paths relative to the indexing root. Regression fixture lives under `.reticle/sessions/fixture`. | Index scale limits remain unchanged. |
| A04: replay classification | Reject explicit executable paths and ripgrep pre-processors. Git, Node syntax checks and TypeScript commands are conservatively unsafe because configuration/preload hooks can execute or write. | Shell command classification is a heuristic, not executable provenance or OS enforcement. |
| A05: network | Docker commands without `network.public` use `--network none`; unresolved browser hosts fail closed. Public browser calls are unsafe for replay. Incorrect no-exfiltration claims corrected. | **Partially addressed.** Network-enabled Docker commands have unrestricted networking. Browser URL filtering cannot prevent DNS rebinding or public-request exfiltration. Native processes remain trusted. |
| A06: false verification | Reject help/version/list/config/dry-run/no-tests shortcuts and shell control syntax. Interpreter evaluation flags cannot masquerade as direct execution of a modified script. | **Partially addressed.** Runtime-owned artifact hashes, collected-test receipts, independent acceptance predicates and structured browser outcomes remain outstanding. |
| A07: broker schema validation | Registration rejects unsupported schema keywords; supported constraints validate recursively before invocation. | Deliberate subset, not full JSON Schema. Existing third-party schemas using unsupported keywords now fail admission explicitly. |
| A08: skill resources | Both repository and plugin manifests register instruction path, size and SHA-256. Catalogue-backed generated workers load that resource, reject missing/changed instructions, and retain provenance including version. | **Partially addressed.** Legacy standalone loading retains its fallback; dependency-only intent is inferred for legacy manifests. Lazy loading and skill applicability evaluations remain outstanding. |
| A09: crashed edit locks | OS-owned nonblocking locks release on process death. Stable lock files remain to prevent inode replacement races. Tests cover a child dying inside an edit and simultaneous lock rejection. | Lock files are retained; multi-file transactions and orphan temporary-file reconciliation remain outstanding. Mixed old/new worker versions must not edit the same workspace. |
| A10: external effects ledger | No production integration added in this batch. | Durable effect intent, uncertainty reconciliation and broker integration remain open. |
| A11: CI | Compiler Python suite added on Linux/Windows; Studio matrix expanded to Windows. | Clean installation, real browser/container acceptance and toolchain policy still need delivery gates. |
| A12: packaging/license | Removed unsupported MIT badge and stated the empty-license condition. | License selection belongs to the owner. No license text invented. Standalone distribution, upgrade/rollback and provenance remain open. |

## Contract and documentation references

The [worker protocol](../../specifications/worker-protocol/v1/001-protocol.md), [runtime orchestration specification](../../specifications/runtime-orchestration.md), [RFC process](../../governance/RFC_PROCESS.md), [RFC-045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md), [RFC-052](../../rfc/RFC-052-Request-Level-Inference-Recovery.md), [RFC-053](../../rfc/RFC-053-Run-Page-Browser-Observation.md), [RFC-054](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) and [RFC-055](../../rfc/RFC-055-Specialist-Agents.md) informed the changes. No draft RFC was silently marked accepted.

Current guides now distinguish specialist workers from generator shells, registered skill resource resolution from checkout-only lookup, and file-only continuation from unsafe replay. RFC-053 contains an explicit correction to its original network/effect premise.

The broker's supported schema subset includes primitive types/type unions, nested properties/required/additionalProperties, array items and uniqueness, enum/const, numeric bounds/multipleOf, string lengths/patterns, and object/array size bounds. Metadata title/description/default/examples/comment is accepted. Nesting is limited to 32. References, composition, formats, custom keywords and boolean schemas are not supported. Patterns use Go regular expressions. Unsupported assertions fail registration instead of weakening validation.

## Validation

Executed locally with temporary files redirected into the workspace because the managed default temporary directory is inaccessible to these subprocesses:

- Go agent and plugin package tests pass, including child-process authorization and delegation regressions.
- Go broker schema tests pass; Forge package tests pass. Runtime and Forge `go vet ./...` pass.
- Python compiler suite: 28 passing tests.
- Targeted Python tests pass: hardening (5), skill loading (7), file overwrite (15), file tools (34), worker awareness (12), specialists (15), documentation contracts (4). Browser sandbox tests pass with deterministic DNS; real Playwright cases in that module require its unavailable dependency.
- Full Python run before the final deterministic DNS-fixture correction: 359 tests, 17 failures, 7 errors, 48 skips. Three failures were unresolved fixture-domain assumptions and were corrected and rerun successfully. The remaining failures/errors are browser startup or restricted loopback connections already observed in the audit. They are not converted to skips or represented as a green integration suite.
- A broker integration test also fails with WinError 10013 on loopback in this executor. It requires rerunning in an environment that permits local sockets.
- Studio code was not changed or locally revalidated in this batch; Windows coverage is configured in CI, not claimed executed here. Docker networking argv is regression-tested without claiming a live-container acceptance test.

Regression sources: `runtime/agent/hardening_test.go`, `runtime/toolbroker/schema_test.go`, `tests/test_hardening.py`, the updated production-layout RAG case, plugin loader tests, skill tests, replay tests and edit tests. Historical audit probes are preserved rather than rewritten to produce success.

## Outstanding plan stages

Stage 0 and stage 1 have substantial changes, but their complete exit criteria are not met. Stages 2–5 remain open: prepared project environments and managed servers; vision and typed diagnostics; atomic multi-file edits and workspace checkpoints; durable conversation/inference/effect recovery; user questions, steering and targeted reruns; scoped repository rules; self-contained releases and support diagnostics; cost reservations/settlement; and frozen comparative task evaluations.

The next integrated acceptance slice remains the plan's Node/Python application loop. It needs real container/browser execution, durable owned-process cleanup, and runtime-owned verification receipts before it can establish production readiness. Network containment is a prerequisite for expanding untrusted browser execution. Frozen protocol/event changes must follow the RFC process; none are claimed implemented by this hardening batch.
