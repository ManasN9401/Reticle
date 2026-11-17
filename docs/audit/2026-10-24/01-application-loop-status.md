---
status: draft
owner: Reticle Project
updated: 2026-10-24
---

# Application-loop implementation status

This continues the [production plan](../2026-10-21/02-implementation-plan.md) and
[22 October status](../2026-10-21/05-readiness-and-publication-status.md). The
preceding hardening batch is committed locally as `1ed45a4`. Its historical
publication blocker is not treated as an unfinished product feature.

## Delivered in this batch

| Area | Implemented |
| --- | --- |
| Project environments | Explicit locked npm/uv/Go preparation using operator-selected images resolved to immutable image IDs. Version output and dependency hashes are reported. An OS-owned advisory lease excludes concurrent cooperating preparation attempts for the same project until cleanup or process death. Project dependencies/caches persist between command containers; changed lockfiles invalidate the attempt's prepared selection. Failed reinstallations discard the previous selection. |
| Command consistency | Commands, structured diagnostics and servers use the prepared image for the same explicit project path. Python selects its project venv; Go/npm/uv use project-local caches. Diagnostic receipts include the environment identity. |
| Server lifecycle | Start/log/stop with bounded argv, one active service, HTTP 2xx readiness, occupied-port refusal, bounded log tails and timeout cleanup. Host port publication is loopback-only. Runtime-owned random labels support cleanup after worker cancellation or death while Forge remains alive. Cleanup failures fail the worker. |
| Browser observation | `run_page(server_id=..., path=...)` resolves only handles owned by this attempt; arbitrary target URLs are not accepted. Existing browser origin filters remain active. |
| Vision | Explicit model image-input metadata, routing requirements that survive fallbacks, local image decoding/downsampling/encoding budgets, real image content parts and screenshot attachment. Earlier images are discarded before text compaction. |
| Packaging and Studio | Generated SDK workers copy both new helper modules. Studio can mark static models with image input and retains their existing metadata when saving. |

The maintained [project execution guide](../../project-execution.md) gives exact
interfaces and operational requirements. The [documentation authority order](../../README.md),
[worker v1 contract](../../specifications/worker-protocol/v1/001-protocol.md),
[capability/effect semantics](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md)
and [RFC process](../../governance/RFC_PROCESS.md) remain in force. RFC-054 stays
draft; this implementation does not accept its unimplemented proposals or change
worker framing, agent schemas, workflow schemas or event names.

## Verification

- Full runtime Go tests and runtime/Forge vet checks pass; Forge tests pass.
- Studio: 83 passing tests and 1 skipped. Type checking, lint and production build pass; existing lint warnings and bundle-size advisories remain. No live visual QA is claimed.
- Full Python run: 397 tests in 437.440 seconds; 353 passed, 34 skipped, 9 failed and 1 errored. All failures/errors are the existing real-browser cases: Chromium exits with Crashpad's crash-server startup failure. The cases remain enabled in CI. Pillow was available from the existing worker package cache for this run.
- The final environment/server tests include real HTTP child-process readiness and cleanup under a substituted launcher, lock exclusion/release, failed install invalidation, runtime container-state checks, and cleanup failure reporting. Vision tests cover bounded real-image encoding, route gating, image content parts, managed-route screenshot attachment and compaction. These are not a live Docker or hosted vision-provider acceptance result.
- Final focused Python run: 88 tests passed in 145.955 seconds, including all 23 environment/server/vision tests. Compiler suite: 28 passed. Four additional regression cases were added after broad-suite discovery, so they are verified in the focused run rather than included in its 397-test count.
- Local Go race execution is unavailable because this environment has CGO disabled and no GCC on PATH. Standard tests/vet pass; CI retains its race checks on the supported OS matrix.
- Whitespace checks pass. A bounded credential-pattern scan of changed/new deliverables found no matches. The unrelated untracked `test_list_models.py` is untouched.

## Remaining limits and next work

- Docker's Linux daemon is unavailable in this execution environment, so no live
  prepared-container/framework acceptance pass is claimed. Substituted-launcher
  tests exercise real local HTTP children, not a Docker daemon.
- Prepared images are configured by the operator. There is no shipped all-language
  image, automatic image build, global dependency cache or durable environment
  lease. npm is the only supported Node preparation manager; pnpm/Yarn/Bun can
  still be used through the existing diagnostics with an appropriate image.
- Managed native servers are deliberately unavailable until forced-cancellation
  process ownership is implemented. Direct native commands remain trusted host
  execution under the existing grant and user setting.
- Force-killing Forge itself still needs startup resource sweeping. Network-enabled
  containers retain unrestricted egress, and public browser requests have no
  OS-enforced public-only egress boundary. Container servers currently require
  `network.public` because Docker network-none mode cannot publish a usable port.
- Vision support is metadata-based, not measured provider compatibility. Unknown
  providers default to text-only; other discovery adapters still need image
  metadata. Pillow is a skill dependency, with an explicit error if absent.
- Multi-file patch transactions, Git checkpoints/rewind, durable conversation and
  pending inference, the task controller, questions/steering/reruns, release
  packaging and comparative evaluation remain open. Image input and readiness
  alone are not evidence that a complete application meets its requirements.
