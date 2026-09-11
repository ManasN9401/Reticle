# Audit repair status — 8 September 2026

The original [findings](01-findings.md) and [recommendations](02-recommendations.md) are preserved beside this ledger. Their source line numbers describe the audited baseline, not the edited files. The user authorized these repairs. No cloud resources were deployed and no real model, training or image-generation jobs were submitted during verification.

## Result and limits

Follow-up: [shared-memory effectiveness review](04-shared-memory-review.md) records additional fixes and measured lookup improvements, plus unresolved lifecycle limits. [ML environment selection](../../../ml-environments.md) distinguishes proposed hardware profiles from current behavior; [environment configuration](../../../environment.md) now documents AWS identity options and the missing deployment adapter.

The repair implements authenticated local control, contained file tools and attachments, a common worker protocol/SDK, specialist registration, ordered result handling, cancellation across compilation/execution, scoped generated workers, lazy dependency provisioning, Studio lifecycle recovery, schemas, documentation and offline regression gates.

This is not a claim that every grouped finding is fully closed. Rows marked **Partial** identify remaining engineering work; **External** requires a real environment, credential source or owner decision. In particular, protected cloud deployment and active pentesting are intentionally unavailable through the generic specialist command loop until an action-specific authorized adapter exists. Native execution remains trusted code running with the user's OS permissions.

## Per-finding disposition

| Finding | Status | Repair and remaining boundary |
|---|---|---|
| F01 | Repaired | Loopback bind/Host checks, same-origin policy, persistent random control token, authenticated HTTP/WS and browser access-code login. |
| F02 | Repaired | Attachments resolve only issued staging IDs; caller paths are ignored. Traversal, aliases and overwrites are rejected; copy/sync/close precede deletion. |
| F03 | Repaired | Generated artifacts use attachment/sandbox responses; the legacy UI is not served. Current UI dynamic names are escaped and directory listing is denied. |
| F04 | Repaired | Shared tools reject absolute/traversal/symlink paths, preserve literal text and use exclusive creates or exact optimistic replacements. Public document fetching pins the checked address and revalidates redirects. These are tool boundaries, not a native-code sandbox. |
| F05 | Partial | Prompt/Markdown approval bypass removed. The runtime checks a separate decision hash against the exact request. Studio writes decisions through the trusted main process. A capability broker for authorizing an exact cloud apply/scan is still absent; those specialist commands are refused. |
| F06 | Repaired | HitL is a runtime checkpoint with normal worker start/completion events and the expected UI markers. The obsolete Python bypass fails explicitly when run directly. |
| F07 | Repaired | Both YAML extensions load; specialist manifests use the actual schema, IDs, entrypoints and skill lists. Registry fixture tests load the advertised specialists. |
| F08 | Repaired | Examples/scaffolding use the runtime workflow format and retain modality and node parameters. Strict field decoding detects unsupported manifest fields. |
| F09 | Repaired | Broken generators share a tested generator and SDK; generated Python handles quotes/newlines and parses successfully. |
| F10 | Repaired | DevOps exposes actual shared tools with checked argument schemas. Unsupported phantom tools and duplicate loops were removed. |
| F11 | Partial / External | No silent Docker-to-native fallback; prepared image/native CLI requirements are explicit. Read-only/validation commands can fail honestly. A real cloud toolchain, scoped identity, plan/apply adapter and deployment acceptance run are still required. |
| F12 | Partial / External | Configurable image, CPU/memory/process/time limits and explicit GPU devices replace assumed hardware. ML dependencies are lazy; native commands use the worker interpreter. A hardware-specific locked training image, GPU admission scheduler and full training/resume validation remain. |
| F13 | Repaired guidance | ML instructions require called seed initialization, dataset/split/version records, deterministic-setting caveats, metrics and resume checks. Cross-platform bitwise guarantees were removed. The example is a workflow specification, not evidence of a completed training experiment. |
| F14 | Repaired | Iteration/time exhaustion raises failure. Completion requires recorded verification; file mutations invalidate previous verification. A successful command is evidence of its exit status, not proof of scientific quality. |
| F15 | Repaired | RAG persistence is workspace-scoped, with checked paths and cached client/embedding objects. |
| F16 | Repaired | Full indexing creates an atomic new snapshot; removed files/trailing chunks disappear from the active index. Deletion uses exact source equality. Mocked adapter tests verify reconciliation. Old snapshots remain until a retention policy is added. |
| F17 | Partial / External | ComfyUI uses explicit local host/checkpoint settings, dimensions/output bounds, request timeouts and an overall deadline. Image capability is not relaxed by routing. A running ComfyUI job can outlive worker cancellation; exclusive server-job/GPU ownership is not implemented. |
| F18 | Repaired | RAG/frontend use the shared protocol; Graphify reads current memory context, has a deadline and reports subprocess/report failure. Actual optional service/tool availability still needs integration testing. |
| F19 | Partial | Specialist terminal commands use validation/read-only restrictions; active scans are refused. This is not a general OS/network sandbox or a scoped scanning adapter. |
| F20 | Repaired | Domain handlers are serialized and graph mutations/submission share a mutex. Linux race execution is configured in CI; it has not run locally. |
| F21 | Repaired | Each execution owns a deep-copied definition; each dispatch owns its parameters and memory map. |
| F22 | Repaired | Worker results publish memory, graph changes, artifacts and then completion in order. Dependents dispatch from completion. The integration test observes both the committed memory and parent artifact. This is an in-memory ordering guarantee, not a durable transaction. |
| F23 | Repaired | Completed/failed/cancelled states are authoritative. Terminal queue entries cannot be resumed or overwritten by late completion. |
| F24 | Repaired | Missing workers and submission failures emit failure and release queue work; no silent running node. |
| F25 | Repaired | One EOF-terminated JSON request, matching response identity, optional artifact and one final response; malformed metadata fails. |
| F26 | Repaired | Concurrent process output draining uses bounded buffers, process context and WaitDelay. Large-output/EOF/identity tests exercise real child processes. |
| F27 | Repaired boundary | Removed fictitious stdin lock grants. The supported SDK uses exclusive filesystem lock/create operations and exact replacement. Legacy interactive lock protocol is unsupported. Native commands can bypass file tools. |
| F28 | Repaired | Unsubscribe callbacks deactivate and remove handlers; replacing an automation disposes its old registration. |
| F29 | Repaired for JSON data | Stores clone supported JSON-shaped mutable values and artifacts; latest-version secondary indexes are maintained. Arbitrary custom Go pointer types are outside the worker JSON contract. |
| F30 | Repaired queue/lifecycle | One owned dispatcher, physical unsubscribe, orderly drain/close, bounded worker slots and a 4,096-event hard queue limit. Diagnostic logs shed earlier. Core overflow explicitly fails active work, rejects new work and requires restart; regression tests cover reentrant overflow. Completed-state archival remains a future retention feature. |
| F31 | Repaired | Task contexts exist before model waiting/retries; kill includes compiler tasks and late-submission tombstones. Pause/resume propagate to compilation. Environment waits and subprocesses honor cancellation. |
| F32 | Partial | Unix process groups, Windows process trees and labelled per-command container cleanup replace immediate-child-only cancellation. Detached external services and already-running ComfyUI work need adapter-specific reconciliation. |
| F33 | Repaired retry boundary | CLI/Studio default to three total attempts, bounded to 1–15. SDK internal retries are disabled. Whole-worker retry requires both a classified transient provider failure and the shared SDK's explicit no-effects marker. Once a file, command or adapter effect starts, an API failure cannot restart the worker automatically. Unknown/custom worker failures default to no retry. A durable effect journal for explicitly resuming side effects remains a separate feature. |
| F34 | Repaired description | Routing is documented as an EMA preference score, not calibrated Bayesian confidence. The wire setting name remains for compatibility; confidence-relaxing fallback is a preference policy. |
| F35 | Repaired | Leases release on success/failure/cancellation even with learning disabled. Capacity applies independently of learning; cooldown timestamps do not re-enable user-disabled models. Regression test covers disabled-learning admission/release. |
| F36 | Repaired / External | Removed fabricated fallback availability. OpenRouter discovery requests tool support; known different Groq system/audio families are excluded. Unknown prices are labelled unknown; available OpenRouter input/output prices use consistent per-million units. Name-based capability remains an explicitly labelled estimate, not a benchmark or verified tool probe. |
| F37 | Repaired | Ollama endpoint is passed as api_base independently of API-key selection. SDK test checks the endpoint contract. |
| F38 | Repaired | Queue execution IDs use random 128-bit values; persisted history no longer resets the identity source. |
| F39 | Repaired | Ungrouped parallel tasks do not inherit one another. Sequential mode gets a real group, and paused work retains capacity. |
| F40 | Repaired with limits | Pending work pumps after definitions load; interrupted work recovers failed; running work cannot be removed. Malformed queue input is preserved separately. General multi-process queue ownership and durable resource recovery remain architecture work. |
| F41 | Repaired | Compiler/runtime context includes attachments, IDE context, history, effort, complexity, native mode, retry and timeout settings. Catalog generation follows registry loading and excludes compiler-only generators. |
| F42 | Repaired | Generated definitions/workers are scoped by execution and resolved before global builtins. Environment caches use semantic identity plus dependency hash. |
| F43 | Repaired | Startup no longer wipes other sessions or changes cwd behind callers; exit unwinds shutdown. Studio documents and validates the checkout working-directory requirement. |
| F44 | Repaired admission/output path | Checked copy/sync/close and symlink refusal determine inheritance/output success. Queue snapshots use unique temporary files, sync and atomic replacement; failure is logged, enqueue rolls back, and pending jobs do not launch if admission cannot be persisted. Broader multi-process transactions remain outside this file-backed queue. |
| F45 | Partial | Lazy, serialized, cancellation-aware provisioning; pinned direct base dependencies; hashed specialist caches; host interpreter PATH consistency. Specialist transitive locks, immutable image builds and clean-machine install acceptance remain. |
| F46 | Partial / External | Workers receive an environment allowlist and the selected provider credential; builtin ML optional tokens are scoped separately. Logs redact known secrets and sensitive event context; a prior log segment rotates on startup. Credential values remain user-supplied; continuous log retention/ACL policy needs deployment-specific work. |
| F47 | Repaired | Studio type checking and production build pass. |
| F48 | Repaired | Projection models pause/cancel and respects terminal states. Pure reducer tests cover lifecycle immutability. |
| F49 | Partial | Session-spanning batches discard old state; reconnect snapshots restore authoritative node/run states, and replay tolerates out-of-order retained events. History is explicitly bounded/in-memory; durable event replay is not implemented. |
| F50 | Repaired scope | Root selection updates workspace/credentials and rejects invalid checkouts. Packaged Studio is documented as a checkout companion, not a bundled standalone runtime. |
| F51 | Repaired hardening | IPC validates main-frame sender; filesystem reads resolve canonical paths and exclude secrets; external navigation is HTTP(S)-only. Checkpoint paths are constrained. Settings validate host, numeric bounds and booleans. |
| F52 | Repaired | Socket callbacks check ownership; connecting sockets are not duplicated; spawn errors and stale process callbacks are handled; stop targets the process tree. Native execution is explicit and defaults off. |
| F53 | Repaired | Key mutations are serialized in the main process and preserve existing content. The real .env was not overwritten by this repair. |
| F54 | Repaired containment | Uploads have total-size/type checks and bounded multipart cleanup. Output previews skip binary/oversized files and bound total content. Large ML files stay on disk; this is not a resumable object-storage artifact service. |
| F55 | Repaired | Per-client bounded outbound queues and write deadlines disconnect slow clients without blocking the event bus. |
| F56 | Repaired / External | Runtime/Forge tests, four example builds, Python worker/adapter tests and Studio reducer tests are added; CI includes Linux race and build gates. Remote CI, signed installers and live services have not been run here. |
| F57 | Repaired | Agent/task/workflow schemas are populated. Plugin schema is explicitly reserved; strict YAML parsing enforces supported manifest fields. |
| F58 | Repaired | README flags, worker protocol and compiler-writer claims match supported behavior. |
| F59 | Repaired | Core-role docs distinguish generator/worker/writer roles and correct manifest-relative worker paths. |
| F60 | Repaired historical boundary | Supersession notes point old RFCs/troubleshooting to current v1 contracts; historical content is preserved with its status. |
| F61 | Partial / External | Core standards/templates/indexes/current specs and contribution guidance are populated. The owner must select and supply LICENSE text; no license was invented. |
| F62 | Repaired scope | Builtin specialists and generated workers load declared SKILL.md instructions. Docs distinguish instruction content, dependency metadata and manual/reference integrations. This is not a universal runtime hook loader. |
| F63 | Repaired scope | Stable session-key compaction counter and explicit external-host/manual integration labels replace automatic-learning/compaction claims. No unverified learned rules are installed. |
| F64 | Repaired with follow-up | Fixed environment badge, null UI payload, escaping, stale file/replay displays and inaccurate comments. Statistical guidance now states estimand/assumption caveats. Archive index identifies unsupported duplicates/scripts. Eight lint warnings and Vite bundle/config warnings remain; see verification. |

## Verification

Offline checks completed in this Windows checkout:

The saved [initial repair verification](evidence/repair-verification.json) and [final verification](evidence/repair-final-verification.json) record command results separately from historical baseline probes. The final record includes the later overload/no-effects retry tests and static Go checks.

- Runtime and Forge Go package tests pass, including real child-process output contracts, result-before-dependent dispatch, cancellation-before-submission, approval tampering, path/control authorization, routing leases and attachment ownership.
- All four standalone Go example modules build through go test. They have no package-level tests and were not executed against providers.
- Thirteen Python unittest cases pass, covering the worker contracts above plus shared-memory prompt visibility, conditional mutations, credential-literal scanning and the paired memory-quality harness. No LLM or service request is needed for this default suite.
- Five Studio projection tests pass: reconnect snapshots, mixed-session batches, immutable lifecycle transitions, out-of-order replay and fatal-overload handling.
- Studio typecheck and production bundle build pass. Lint exits successfully with eight warnings. Vite reports future config-loader compatibility and large editor/application chunks; a successful bundle is not a signed/installed Electron acceptance test.
- Whitespace validation uses git diff --check with line-ending conversion disabled for this mixed-ending checkout.

Local Go is 1.27.1 and Node is 26.8.1. Linux race checks are specified in CI. A local race attempt reported that CGO must be enabled, so the race detector was not completed on this Windows machine. The test suite does not validate actual cloud identities, paid provider access, GPU training quality, checkpoint resumption, ComfyUI service cancellation, or clean dependency installation.

## Required follow-up

1. Identify the missing .env key names and their credential source. Existing catalogued provider slots were populated when inspected; only names/presence were inspected, never values in reports. The new .env.example and environment guide include optional integrations. No secrets can be reconstructed from source code.
2. Choose the cloud target/identity, prepared immutable job image and ML hardware/budget, then run adapter-specific acceptance checks. Protected mutations remain refused until their exact authorization path exists.
3. Finish the engineering items still marked Partial: reproducible specialist locks, external-effect reconciliation and remote resource reconciliation. RFC-044 now provides bounded local memory retention and restart snapshots; a replicated or high-throughput journal/database backend remains future scaling work.
4. Supply the project's intended license text.

## External references used for repair decisions

- [Go subprocess contract](https://pkg.go.dev/os/exec): process cancellation/WaitDelay and pipe handling.
- [Docker resource constraints](https://docs.docker.com/engine/containers/resource_constraints/): resource limits need explicit configuration.
- [PyTorch reproducibility](https://docs.pytorch.org/docs/2.9/notes/randomness.html): seeded execution does not guarantee identical results across platforms/releases.
- [LiteLLM Ollama configuration](https://docs.litellm.ai/docs/providers/ollama): endpoint configuration is independent of hosted credentials.
- [OpenRouter model discovery](https://openrouter.ai/docs/guides/overview/models) and [pricing documentation](https://openrouter.ai/docs/faq): tool filtering and distinct input/output prices.
- [Groq tool-use overview](https://console.groq.com/docs/tool-use/overview) and [Compound systems](https://console.groq.com/docs/compound/systems): custom application tools differ from provider-owned compound systems.
- [GitHub setup-go](https://github.com/actions/setup-go) and [setup-node](https://github.com/actions/setup-node): CI toolchain/cache configuration.
