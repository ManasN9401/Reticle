# Shared-memory effectiveness review

Follow-up to the 8 September audit, completed 9 September 2026. Benchmarks and runtime tests were run before the interruption; Python tests and runtime static checks were completed again after continuation. Unrelated workspace changes have not received a separate comprehensive review here.

## Assessment

Shared memory had useful scope isolation and ordered delivery, but was not effective end to end: the common worker SDK discarded arbitrary injected facts when constructing the model request. Reading an entire scope also became slower as unrelated executions accumulated. Both defects are repaired. This remains an ephemeral, explicitly selected context store, not a durable or semantically ranked knowledge system.

## Repairs and evidence

1. **Model visibility:** `worker_sdk.py` now includes dispatched shared facts in the model's initial user-message JSON. Runtime controls are excluded from that field. The Python contract test inspects the actual model request and verifies a prior agent's dataset fact is present. This is a mocked model test, not evidence that a real model will reason correctly from the fact.
2. **Scope lookup scaling:** `RuntimeState` now stores entries in scope buckets rather than scanning the entire store. Reads still return owned copies of supported JSON values. Tests exercise nested mutation isolation and 16 concurrent execution scopes with 100 updates each.
3. **JSON read requests:** the manager now accepts a plain string scope, as produced by JSON decoding, as well as Go's named scope type. The regression test failed before the fix with `found:false`. Responses echo the optional `request_id` for correlation.
4. **Atomic artifact updates:** selecting the latest artifact and appending its replacement now occur under one exclusive lock. Previously a deletion or metadata update could intervene between selection and append. Tests verify 320 concurrent updates retain all 321 contiguous versions, returned history is isolated, latest indexes remain consistent, and an update after deletion fails. The test does not deterministically recreate every possible concurrent delete interleaving; lock coverage addresses that code-level defect.
5. **Explicit context budget:** dispatched shared facts exceeding 64 KiB of serialized UTF-8 JSON fail with an actionable error rather than being silently truncated. This bounds that prompt field only; it does not bound the store, all task inputs, total prompt tokens or artifact size. Use selected keys and summaries/references for large data. A regression test checks the limit.

## Lookup benchmark

Windows/amd64; Go reported an AMD Ryzen 9 9900X with 24 logical processors. Each scope held ten short string facts. Each case was measured three times, with a 200 ms benchmark target per repetition. Setup was outside the timed region. Values below are medians, not tail-latency or concurrent-load measurements.

| Total stored facts | Before, ns/read | After, ns/read |
|---|---:|---:|
| 1,000 | 8,394 | 401.8 |
| 10,000 | 75,459 | 451.9 |
| 100,000 | 1,159,180 | 395.7 |

Per-read allocation fell from approximately 1,001 bytes / 8 allocations to 712 bytes / 7 allocations. The nested maps have their own resident-memory overhead, which was not measured. These results show elimination of unrelated-store scanning for this fixture; they do not imply an equivalent improvement in whole-agent runtime or large-value cloning.

Raw evidence: [before](evidence/memory-before.txt), [after](evidence/memory-after.txt). Reproduce from `runtime` with `go test ./memory -run '^$' -bench BenchmarkScopeRead -benchtime=200ms -count=3`.

## Remaining weaknesses and priorities

| Priority | Observed limit | Recommended change |
|---|---|---|
| High | No runtime-state expiry, scope deletion policy, byte quota or automatic artifact-version retention. New executions and versions accumulate until process termination. | Archive completed execution outputs, then release eligible scratch scopes after dependants/readers finish; introduce configurable retention and byte quotas with visible rejection/eviction reasons. Preserve active and pinned data. |
| High | A worker receives a snapshot. Arbitrary facts require `required_memory` declarations; running workers do not receive live refreshes. | Validate producer/consumer memory contracts when compiling a graph; expose missing required facts as explicit errors and record read provenance/version. Use DAG dependencies for freshness. |
| High | Shared-key writes are last-write-wins, with no compare-and-swap or merge semantics. | Add expected revisions for concurrent updates, or use immutable per-task facts and an explicit reducer. Atomic artifact append does not solve shared-key lost updates. |
| High | No durable reconstruction of this store across process restarts. | Persist versioned events/snapshots and test crash recovery before advertising resumable cross-session memory. |
| Medium | Artifact execution queries still scan all artifact series; large values are cloned while holding locks. | Add an execution index, benchmark realistic artifact sizes and mixed read/write contention, then consider immutable payload references and shorter lock holds. |
| Medium | `cloneValue` supports the JSON-shaped protocol values and selected Go containers, not arbitrary custom Go pointers/typed collections. | Constrain internal memory values to a validated JSON contract or implement a comprehensive ownership boundary. The current comment claiming physical prevention of all external mutation is too broad. |
| Medium | Agent-scope fallback precedes execution/workflow/global lookup for declared facts. Agent-scoped facts can therefore survive across executions for the same agent identity. | Make intended lifetime explicit in declarations; require deliberate cross-run sharing and test conflicting scope values. |
| Medium | No semantic relevance selection, stale-fact evaluation, or measured real-model task quality. | Build an evaluation set with relevant, irrelevant, conflicting and stale facts; measure task accuracy and prompt cost against no-memory and full-context baselines. |

Shared memory must contain task data rather than credentials. The historical RFC-027 credential example was replaced with a dataset revision. Historical RFC-007 claims about deterministic purity, undeletable artifacts and seamless scalability should be read with its existing supersession notice; they are not verified current guarantees.

## Verification boundaries

- Runtime `go test ./...` passed after the Go changes, including the new memory and artifact tests and existing dispatcher integration tests.
- Nine Python worker tests passed after the prompt changes and passed again after continuation with the current shared worker file.
- Benchmark output is recorded above. No race-detector, heap-growth soak, p95/p99 contention measurement, live-model quality experiment or GPU training run was performed.
- Runtime `go vet ./...` passed after continuation. Its earlier attempt was blocked by automatic approval review reporting an account usage limit; a subsequent retry through the normal approval process succeeded.

The repaired implementation is materially more useful and faster for scoped scratch facts. Long-running, concurrent and restartable agent workloads still need the lifecycle, conflict and evaluation work listed above.

## Implementation follow-up — 9 September 2026

The four high-level gaps requested after this report were implemented under [RFC-044](../../../rfc/RFC-044-Memory-Lifecycle-Recovery-Evaluation.md): bounded execution expiry and terminal cleanup, entry/byte and artifact-version retention, compare-and-set revisions with worker acknowledgement, local restart snapshots with rollback on persistence failure, and a paired memory-quality evaluation harness. The original table remains as the record of what was observed before that follow-up.

The implementation does not turn local snapshots into multi-process or replicated storage. The quality harness supports real configured models, while the committed default is an offline contract evaluation so tests do not spend provider quota. Current verification results are recorded in the repair status and test output for the follow-up.

Follow-up verification passed the full runtime suite, `go vet ./...`, and 13 Python tests. The four-case [offline quality result](evidence/memory-quality-offline.json) passed 4/4 and checks the prompt/selection contract only. No local Ollama service was available. With the user's request for real-model evaluation, the synthetic [Groq Allam 2 7B result](evidence/memory-quality-groq-allam-2-7b.json) scored 0/4 without memory, 1/4 with all selected and distracting facts, and 4/4 with only the required facts. This is one attempt over four small exact-answer cases, so it demonstrates the harness and a promising relevance effect rather than statistical reliability.

An initial current-catalogue Qwen 3.6 27B trial reached the model but spent its 32-token answer budget on reasoning text and scored 0/4 in all modes under the exact-output grader. The result was not used as evidence of failed memory retrieval because the selected answers appeared inside truncated reasoning. This revealed model/output-budget compatibility that the routing and evaluation configuration must handle explicitly. The paired live path records exact-answer accuracy, pass@k, pass^k, and selected-context uplift against no-memory and full-context conditions.

After adding retention accounting, a benchmark exposed full-store scans during insertion; the counter and periodic expiry sweep replaced them before completion. The corrected [lifecycle benchmark](evidence/memory-lifecycle-benchmark.txt) populated all requested entries and measured ten-fact scope reads at 408.5 ns (1,000 total), 448.0 ns (10,000), and 410.3 ns (100,000), with 712 bytes and seven allocations per operation. This remains a microbenchmark; synchronous full-snapshot persistence is intentionally documented as the next scaling boundary for write-heavy workloads.
