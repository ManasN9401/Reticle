# Recoverable patches and attempt checkpoints

These additions implement the editing/checkpoint portions of the [production plan](audit/2026-10-21/02-implementation-plan.md) and [RFC-054 tool proposal](rfc/RFC-054-Agent-Tool-Surface-Roadmap.md). They retain existing capability names, worker framing and event types. They do not supply durable conversation recovery, automatic rerun/rewind, branch/worktree isolation or a Studio rewind screen.

## Applying a patch

`apply_patch(patch)` requires `workspace.write`. It accepts UTF-8 unified diffs with `---` / `+++` headers, `@@ -old,count +new,count @@` hunks, context lines, additions and removals. Use `/dev/null` for an added or deleted file. Renames, binary patches, file-mode changes and empty-file-only patches are unsupported.

```diff
--- a/main.py
+++ b/main.py
@@ -1 +1 @@
-print("old")
+print("new")
--- /dev/null
+++ b/tests/test_main.py
@@ -0,0 +1,5 @@
+import subprocess, sys, unittest
+class MainOutput(unittest.TestCase):
+    def test_output(self):
+        result = subprocess.run([sys.executable, "main.py"], capture_output=True, text=True, check=True)
+        self.assertEqual(result.stdout, "new\n")
```

A patch is limited to 32 files, 256 hunks, 2 MiB of patch text, 1 MiB per source/result file and 8 MiB of combined before/after content. Context scanning has a separate work limit. Exact context at the declared location takes precedence; displaced context requires a unique exact or whitespace-only match. Missing/ambiguous context produces a bounded hint rather than choosing an arbitrary similar block. Existing CRLF style and missing-final-newline markers are preserved. HTML duplicate-document checks remain active.

Paths use the existing [worker v1 path contract](specifications/worker-protocol/v1/001-protocol.md): one leading `src/` aliases the source root. Canonical paths inside receipts are resolved literally, so the alias is not stripped twice. Traversal, symlinks/junctions, repository/runtime metadata, duplicate/case-aliased targets, parent/child file-target collisions and Windows device/trailing-dot/space aliases are refused.

The SDK resolves guidance for every patch target before making changes. New repository scope must be reviewed and the tool retried. A successful patch invalidates prior diagnostics and requires fresh verification of updated outputs. A delete-only patch can record its verified deletion receipt. Git status/diff are observations and do not satisfy application verification.

## Commit and recovery semantics

The complete patch is validated before replacing any application file. Stable OS-owned locks cover the transaction and all target files. Before/after bytes, SHA-256 digests, task/attempt identity and file modes are journaled under the session's `patches/` directory before commit begins. Each file replacement uses a synced temporary file and checks the expected digest again before replacement. Existing basic permission bits are retained; extended ACLs and all filesystem metadata are not preserved by this text-edit interface.

**Multi-file visibility is not simultaneous.** Each individual replacement is atomic, while another reader can observe a partially advanced file set. The transaction supplies complete prevalidation and recoverable rollback, rather than a filesystem-wide atomic snapshot or serializable database transaction. Recovery is tested for process death; physical power-loss consistency is not established. Locks coordinate cooperating tools; arbitrary shell commands and external editors can ignore them.

On an ordinary write failure, rollback restores before-images if the complete recovery set still matches before/after hashes. A killed worker leaves a durable pending journal. Before a later SDK task runs, a writer recovers that journal; read-only workers refuse the mixed state. Mutating file tools and completion also check for pending recovery. A conflicting later edit is preserved, the journal stays pending, and controlled edits/completion stop for operator repair. No automatic conflict choice or forced overwrite is made.

Completed/rolled-back journals move to `patches/completed/`; normal recovery scans pending records only. Journal contents include original source text, so they can contain sensitive material from permitted source files. They remain local session data and need the same access controls and retention as the workspace. Automatic journal pruning and storage quotas remain open. Removing the last conflicting journal without inspecting its files is not a recovery procedure.

## Runtime-owned Git history

Set `RETICLE_WORKSPACE_CHECKPOINTS=true` in the environment that starts Forge to enable checkpoints. Git must be installed on the runtime host. The feature is opt-in: supported project sizes and source selections need validation before enabling it across workloads.

For attempts with an ID and a source workspace, Forge records a before snapshot, then an after snapshot on success/failure/cancellation while Forge remains alive. A private bare repository and bounded per-attempt records live under the session's `history/`. This repository is runtime data: it is not the user's project repository. Checkpointing never stages, commits, resets, checks out or pushes the user's repository metadata.

Snapshots capture raw file bytes through [Git fast-import](https://git-scm.com/docs/git-fast-import), with inherited Git overrides/global configuration removed and project hooks/filters avoided. Files are bounded to 16 MiB each, 64 MiB total and 1,000 files. Binary files are retained within those limits. Two reads detect ordinary changes during snapshot preparation; this does not provide a filesystem snapshot under arbitrary concurrent writes. Exceeding a limit or failing checkpoint persistence fails the attempt rather than silently recording a partial source set. Result-memory persistence and history are separate stores, not one transactional outbox.

Conventional dependency/runtime directories, `.git`, `.aws`, `.ssh`, `.env*`, venvs, caches and edit locks are excluded. Exclusions are name-based, not secret detection; another file can still contain credentials, and legitimate files can match excluded names. Excluded changes are not undoable through these checkpoints. Existing `.gitignore` and `.gitattributes` do not silently decide the snapshot set. There is no LFS/large-asset snapshot support.

One Forge process owns a session history lease. Concurrent attempts inside that process are recorded as overlapping; their whole-workspace differences are not reliable per-worker attribution and cannot be rewound through this API. After a Forge crash, completed records remain readable, while abandoned running records display as interrupted and lack a trustworthy after snapshot. Automatic startup reconciliation of those attempts remains open.

With a checkpoint, workers granted `workspace.read` receive:

- `git_status(offset=1, limit=100)`: paged additions, changes and deletions relative to the current attempt's before snapshot.
- `git_diff(path?, offset=1, limit=200)`: paged text differences; binary/large files are summarized. At most 20 changed files are expanded per call; narrow `path` for another file.

Both are read-only views of runtime history and source files. Source bounds and path guards apply. The checkpoint must belong to the current task. These are observed workspace changes, which may include another actor's edits; they are not evidence of actor identity or successful tests.

## Operator inspection and rewind

Use the existing authenticated loopback control connection. New endpoints are additive to the [external-effect controls](external-effect-recovery.md):

| Request | Purpose |
| --- | --- |
| `GET /api/checkpoints?execution=<id>` | List up to 1,000 checkpoint records, newest first, with a truncation flag. |
| `GET /api/checkpoints/diff?execution=<id>&id=<checkpoint>` | Inspect the saved before/after difference, bounded to a 512 KiB text response. |
| `POST /api/checkpoints/rewind` | Restore an eligible failed attempt's captured difference after review. |

Rewind request body:

```json
{"execution":"run-id","id":"32-character-checkpoint-id","expected_after":"40-character-after-commit-id"}
```

Retry/progress proofs must be leading stderr tokens, not quoted model/tool content. Unsafe progress cannot be downgraded by a later proof. Rewind requires an idle workspace, a completed failed attempt, explicit no-effects/file-only progress proof, no overlapping attempts, no previous rewind, and the expected after identity. It compares affected live bytes and executability with the recorded after state, refusing later edits or busy files. Unrelated current files are retained. Attempts with commands, managed servers, browsers or brokered uncertain effects are ineligible even if their files are restorable. Inspect and reconcile external effects separately; restoring files does not undo them.

A rewind is limited to 32 changed files, 1 MiB per file and 8 MiB of before/after content. Existing non-executable permission bits are preserved where supported; restored deleted files use owner-only permissions with recorded executability. Mode-changing attempts and extended ACL restoration are not supported. Rewind uses the same durable patch-journal format. If interrupted, normal SDK patch recovery rolls back the incomplete rewind before later editing. Conflicting external edits stop recovery.

The control is an explicit operator action. It does not resume the task, reset model conversation state, reuse approvals or silently rewind on provider fallback. There is no Studio button for it yet. Inspect the exact diff and exclusions before supplying `expected_after`.
