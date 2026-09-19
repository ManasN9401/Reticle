---
status: accepted
owner: Reticle Project
updated: 2026-09-19
---

# Repair status for the 18 September 2026 review

This ledger records the disposition of the findings in `01-logic-and-documentation-review.md`. The original review remains unchanged as evidence of the reviewed baseline.

## Code findings

| Finding | Status | Resolution |
|---|---|---|
| P1-01 | Fixed | `RuntimePersistenceFailed` now identifies affected executions. The waitlist terminalizes matching running items, releases their capacity, persists the queue, and stops further admission until restart. |
| P1-02 | Fixed | Execution-store failures now pass through one fatal reporting boundary. Active work and attempts are interrupted, success is not published after an uncommitted transition, and the phase, execution, and node are reported. |
| P1-03 | Fixed | The task schema now declares `attempt_id`, `memory_metadata`, and `capabilities`. A contract test compares serialized task fields with the schema. |
| P2-01 | Resolved with compatibility retained | Agent definitions now require the schema's `name` and `version`, and current examples declare capabilities. Omitted capabilities still select the documented v1 compatibility profile, and unknown skills are still logged and skipped. Both behaviors are intentional compatibility contracts in the agent-definition specification and the registry compatibility repair; changing them would break existing definitions. New definitions should use explicit least-privilege capabilities. |
| P2-02 | Fixed | Admission context is written through one acknowledged, rollback-capable memory batch before workflow submission. |
| P2-03 | Fixed | Explorer scans are serialized, scoped to the selected execution, and guarded by request generations so stale responses cannot replace current state. |
| P2-04 | Fixed | Provisioning events carry operation, execution, task, and attempt identity when work is task-triggered. Studio scopes them to the selected execution and keys concurrent activity by operation. |
| P2-05 | Fixed | Worker responses carry structured verification evidence. Every changed file must be reread unless a recognized test, build, lint, or validation command succeeds after the changes. Availability, inventory, and generic shell probes do not satisfy completion. |
| P2-06 | Fixed | Only stderr records whose trimmed line starts with `[LLM_STREAM]` are excluded from the bounded failure excerpt. Ordinary diagnostics that mention the marker remain visible. |
| P2-07 | Fixed | A panicking event subscriber is quarantined and a redacted `SubscriberPanicked` event is sent to surviving subscribers; the dispatch loop continues. |
| P3-01 | Fixed | Routing preferences use temporary-file, flush, close, and replacement steps. Read, decode, and write failures are logged. |
| P3-02 | Fixed with one owner | The router no longer subscribes independently to terminal worker events. The dispatcher owns scoring because each attempt must update routing before a retry selects its next model. |
| P3-03 | Fixed | Workspace creation failure now fails the queue item immediately, saves the queue, and releases its capacity. |
| P3-04 | Fixed | If staged-source cleanup fails after a copy, the destination is removed so a retry is not poisoned by an existing file. |

## Documentation findings

| Finding | Status | Resolution |
|---|---|---|
| D1 | Fixed | The root README now describes bounded concurrency, score-and-capacity routing, selected context, and measured behavior. |
| D2 | Fixed | The troubleshooting guide now follows Forge-owned retry, current local-model support, and current failure signals. |
| D3 | Fixed | The Studio README and IPC specification now match snapshots, node states, approval documents, Electron IPC, and current runtime events. |
| D4 | Fixed | The three misleading runtime diagrams were redrawn from the current lifecycle, isolation, and event contracts. |
| D5 | Fixed for the reviewed contradictions | Superseded retry/logging design records are marked historical or superseded, and the documentation index defines authority order. |
| D6 | Fixed for RFCs and specifications | RFCs and specifications now carry the enforced `status`, `owner`, and `updated` metadata. A documentation test enforces the minimum and rejects obsolete runtime-diagram terms. Broader metadata remains governed by the draft standard. |
| D7 | Fixed | Skill documentation now distinguishes strict specialist instruction loading from the v1 registry's intentional log-and-skip compatibility behavior. |
| D8 | Fixed | The Studio process comment now describes the current conditional native-mode behavior and fail-fast Docker path. |

## Validation

The repaired tree passed:

- all runtime Go tests and `go vet`;
- all Forge Go tests and `go vet`;
- 17 Python worker, memory, and documentation tests;
- Studio type checking, 11 tests, lint, and production build;
- diff whitespace validation.

Studio lint still reports seven pre-existing React advisory warnings. The production build still reports the existing Vite native-config warning and large bundle chunks. These warnings are outside the reviewed findings and do not fail the build.

No live cloud account, provider API, container daemon, GPU workload, or external side effect was used in this repair pass. The code and contract fixes for those paths are covered by local tests; provider-specific integration still requires configured infrastructure.

## Provider-routing follow-up

On 19 September, real OpenRouter timeout and Gemini 403 traces exposed an ordering defect in the retry path: the narrow retry gate rejected those failures before the existing provider-penalty code could run. Provider failures are now classified once before routing action. With explicit `NO_EFFECTS` proof, upstream timeouts retry, authentication/permission/quota/rate-limit failures cool down the affected key, bad-request/context failures lower the selected model's score, and unsupported tool calling disables that model. Generic process failures remain terminal.

The same follow-up separated an OpenRouter key's free-tier status from exhaustion. Free-tier keys remain routable to free models; only an exhausted reported limit marks the key unavailable in the waitlist/Studio projection.
