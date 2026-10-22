# Diagnostics and repository guidance

These SDK features implement parts of the [tool roadmap](rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) and [production implementation plan](audit/2026-10-21/02-implementation-plan.md). They do not change the v1 worker framing or permission model.

## Structured diagnostics

`run_diagnostics(kind?, path?)` detects a supported project configuration and runs fixed argument vectors. Kinds are `auto`, `test`, `typecheck`, `lint`, and `build`; `path` defaults to the workspace source root.

| Project | Supported checks |
| --- | --- |
| JavaScript `package.json` | Existing `test`, `typecheck`/`check`, `lint`, and `build` scripts with npm, pnpm, Yarn or Bun |
| Go `go.mod` | `go test ./...`, `go vet ./...`, `go build ./...` |
| Python tests/configuration | unittest discovery or pytest; configured Ruff, mypy/Pyright and non-isolated Python package builds |

An unknown project reports nothing to run. `packageManager` selects the JavaScript adapter; otherwise a single manager's lockfile is used, with npm as the default. Conflicting lockfiles require an explicit declaration. Commands retain the selected project's working directory, including subprojects with spaces in their paths. Missing executables or dependencies produce failed checks; Reticle does not silently install another toolchain. Prepared Node/Go project images are still required for those container workloads. Python checks are enabled by their configuration files or pyproject sections; package builds use the installed environment with `--no-isolation`.

Checks use the existing terminal containment, timeout, network grant and resource limits. Detected npm scripts and test code can execute project code; the tool is classified unsafe for automatic replay. Native mode remains explicit trusted host execution. DevOps and pentest specialists do not receive this generic execution adapter.

Results include exact argument vectors, exit codes, observed test counts, parsed file/line/column errors, excerpts and a receipt reference. Tests must show executed tests, rather than merely exiting zero. Common unittest/pytest/Jest/Vitest summaries and Go package test summaries are recognized. An unrecognized test summary fails verification conservatively.

`read_diagnostics(receipt, check?, offset?, limit?)` pages captured output for a receipt belonging to the current task, including earlier attempts of that task. Other tasks' receipts and arbitrary paths are refused. Check indexes start at zero; output line offsets start at one. Diagnostic summaries remain valid JSON within the route's tool-result budget; large details stay in the receipt. Terminal capture is still bounded to 10,000 bytes per stream and marks truncation explicitly; it is not full-output streaming.

Receipts are stored outside source files under the session's `diagnostics/` directory. They bind task/attempt identity to SHA-256 hashes of the SDK's currently pending modified artifacts, captured before and after checks. The SDK rechecks those hashes at completion and rejects a stale receipt. Each hash operation is bounded to 500 files, 16 MiB per file and 64 MiB total. Binary files can be hashed without decoding.

This is SDK-managed evidence, not an independent Go acceptance oracle. Hash coverage currently includes tracked pending edits, not every repository dependency or files silently modified by arbitrary shell commands. Existing v1 reread and terminal verification paths remain compatible. Build-script semantics, malicious project scripts and unrelated environmental changes still require independent acceptance tests. Managed development servers, full-output streaming and semantic diagnostic adapters remain future work.

## Scoped repository guidance

Workers with `workspace.read` load `AGENTS.md` and `RETICLE.md` from the session source root and ancestors of files in their declared or discovered task scope. Parent directories outside the source root and unrelated sibling directories are excluded. Files and scopes use the same traversal/symlink checks as workspace tools.

User intent and executable runtime policy take precedence. Deeper-directory guidance overrides ancestor guidance only for files under that directory. In one directory, `RETICLE.md` follows and overrides `AGENTS.md`. Source path, scope, raw-content SHA-256 and byte size accompany the content in task context. Retrieved documents are not automatically treated as repository guidance.

Guidance is refreshed at inference boundaries. Before a guided file tool enters a new scope, newly discovered guidance is loaded and the action is returned to the model for review and retry; the first action does not run under unseen rules. Literal terminal commands are not parsed into file scopes: declare the intended files or inspect them with file tools first when commands touch new directories.

Each instruction file is limited to 12,000 UTF-8 bytes; selected guidance totals at most 32,000 bytes and the SDK lowers that ceiling using its route context budget. Exceeding the budget produces an explicit error instead of silently dropping constraints. Source metadata, without instruction bodies, is saved under the session's `guidance/` directory. Guidance does not expand capabilities, authorize deployments, override approvals or make untrusted host code contained.

Contract checks: `tests/test_diagnostics_tools.py`, `tests/test_worker_diagnostics.py`, `tests/test_repository_rules.py`, and the compiler contract suite.
