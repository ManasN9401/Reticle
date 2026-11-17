---
status: draft
owner: Reticle Project
updated: 2026-10-24
---

# Recoverable patch and checkpoint implementation

This follows the [application-loop batch](01-application-loop-status.md) and
[production plan](../2026-10-21/02-implementation-plan.md). The operational
contract is in [workspace history](../../workspace-history.md). Documentation
was checked against the [authority order](../../README.md),
[worker v1 contract](../../specifications/worker-protocol/v1/001-protocol.md),
[capability/effect RFC](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md)
and [RFC process](../../governance/RFC_PROCESS.md). Worker framing, capability
names, frozen agent/workflow schemas and event names are unchanged. RFC-054
remains draft for its unimplemented proposals.

## Delivered

- Bounded unified patches with complete prevalidation, unique context matching,
  portable path guards, guidance review, per-file OS locks and synced rollback
  journals. CRLF, missing final newlines, HTML guards and the one-prefix path
  contract are retained. Canonical recovery paths are resolved literally.
- Ordinary failure rollback and subprocess-crash recovery; conflicts preserve
  later edits and block controlled changes/completion. Updates invalidate
  verification; delete-only patches can prove their actual deletions.
- Opt-in runtime-owned Git snapshots before/after attempts, raw binary/text
  capture, bounded source selection, excluded dependency/secret-name paths and
  explicit overlap tracking. Project hooks, filters and user Git metadata are
  not used for checkpoint writes.
- Retry/progress marker parsing ignores quoted marker text and cannot downgrade unsafe effects. Legacy leading-marker diagnostics remain compatible. This closes a shared replay/rewind eligibility weakness found during final review.
- Read-only task-bound `git_status`/`git_diff` SDK tools, packaged with generated
  workers. Authenticated operator list/diff/rewind APIs restore only eligible
  failed file-only diffs with live-state conflict checks and a durable rewind
  journal compatible with SDK recovery.

## Verification

- Focused Python suite: 107 tests passed in 172.761 seconds. The current patch/history tests also pass independently: 15 patch cases and 4 history-inspection cases. They include actual subprocess death, rollback conflicts, mid-write user edits, CRLF/EOF preservation, one-prefix path recovery, bounded context scanning and real Git inspection without an index write.
- Compiler Python suite: 28 tests passed, including generated-worker helper packaging.
- Runtime and Forge Go tests/vet pass. New tests exercise real Git snapshots, Unicode/empty/binary files, ignored secret/dependency paths, disabled project filters, automatic before/after recording, unsafe/overlap/later-edit refusals, authenticated control handler validation and shared-journal recovery of an interrupted Go rewind through Python.
- Retry-proof regressions reject marker text inside structured logs and unsafe-to-safe downgrades while retaining legacy leading-marker exception formatting.
- Documentation links/metadata and whitespace checks pass. No project commit or push is claimed.
- Live Docker/browser acceptance remains blocked as recorded in the prior batch. This focused run does not erase the earlier broad suite's 10 browser failures/errors. Local Go race execution remains unavailable in this environment; CI retains race checks.

## Commit request

The user requested a commit before continuation. The session's explicit
filesystem permission profile still marks the project `.git` read-only. No
project commit was created. This batch's private Git checkpoint test repositories
are temporary product-data fixtures; they do not stage or publish the Reticle
source changes and are not a workaround for that restriction.

## Remaining work and limits

Patches provide recoverable rollback and individually atomic file replacement,
not simultaneous multi-file visibility. Locks coordinate cooperating tools;
uncooperative commands/editors can still race. Completed journals and Git history
need retention/storage management. Original source bytes may contain sensitive
material despite conventional path exclusions.

Checkpoints are opt-in, size-bounded observations of a shared source tree. They
do not establish per-worker attribution during overlap, preserve every file
metadata/ACL, include excluded files or rewind external effects. Only explicit
operator rewind of eligible failed attempts is implemented. Studio rewind UI,
automatic retry rewind, isolated branches/worktrees and incomplete-attempt
reconciliation after Forge death remain open.

Durable task/conversation and pending-inference recovery, questions/steering,
targeted reruns, stronger egress containment, release packaging and independent
comparative evaluation remain on the roadmap. Docker/browser acceptance from the
previous batch remains an environment gate, not a passing result.
