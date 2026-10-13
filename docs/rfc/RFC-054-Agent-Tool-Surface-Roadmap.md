---
status: draft
owner: Reticle Project
updated: 2026-10-13
---

# RFC-054: Agent Tool Surface Roadmap

## 1. Purpose

Record the tools and surrounding capabilities that Reticle's workers lack compared with agentic coding and co-working applications, why each matters (with evidence from real runs), and a concrete specification for each, so they can be scheduled and built without re-deriving the reasoning. This document proposes; nothing here is implemented unless it says so. [RFC-053](RFC-053-Run-Page-Browser-Observation.md) implements the first item (`run_page`).

## 2. Motivation

A comparison with Claude Code, Cowork-style desktop agents and OpenClaw-style personal agents found that the largest gaps are structural (an act, observe, fix loop; one coherent context; a strong tool-trained model) rather than a long list of missing tools. Several tools are nonetheless missing, and the evidence for them is in Reticle's own runs:

| Observation | Source | Tool gap |
| --- | --- | --- |
| A site of 457 KB did not parse; its CDN script URL was dead | gallery run | run the page (done, RFC-053), then see it, then test it |
| A QA node briefed to run browser tests spent 30 turns on shell commands | `exec-856b…` qa-testing | run and observe; honest briefs |
| `replace_file_content` "Expected exactly one match" loops; `write_file` refuses to overwrite | `exec-e1a8…` attempts 7 and 8 | edit tools |
| Generated images the model could never look at | image nodes | vision |
| `browser-agent`, `quant-agent`, `osint-agent`, `hermes-coder-agent` workers are compatibility shells that run the code generator; Playwright and yfinance appear only in their YAML descriptions | repository | catalogue honesty |
| Only `read_url`; no search | tool list | web search |

## 3. Principles

1. **Observation before assertion.** Prefer tools that let a worker see what happened over instructions that ask it to be careful.
2. **Capabilities, not prompts.** Every new tool is gated by an explicit capability the architect must grant (RFC-045). Nothing is added to the default profile.
3. **Structured results.** A tool returns something a model can act on in one read: file, line, message; not 300 lines of log.
4. **Effects are classified.** Each tool declares whether it is read-only, replay-safe (new files) or unsafe, so recovery stays correct.
5. **Untrusted input stays untrusted.** Web pages, search results and documents are data, never instructions, and are size-bounded.

## 4. Vision: `view_image` and model vision metadata

**Problem.** `run_page` and image generation produce pictures nothing can look at. The blank-screen verdict catches only "nothing drew".

**Interface.** `view_image(path, max_side?)` returns the image as an image content part in the next model turn (downscaled, default longest side 1280, PNG/JPEG/WebP only). `run_page` gains `view: true` to attach its screenshot directly.

**Capability and containment.** `workspace.read`. Size and dimension caps; no remote URLs.

**Implementation.** The router needs to know which models accept images: add `Vision` to `routing.Model` from provider metadata (litellm `supports_vision` and the discovery responses) and exclude non-vision routes for a task whose worker has attached an image (`RoutePolicy`). The worker offers `view_image` only when the routed model is vision-capable. Messages carry `{"type":"image_url","image_url":{"url":"data:..."}}` parts; compaction (`_compact_messages`) must drop or replace image parts first.

**Acceptance.** A scripted vision-capable fixture receives the image part; a non-vision route never sees the tool; compaction removes images before text; a 4 MB screenshot is downscaled below the request limit.

**Depends on.** RFC-053. Unlocks QA nodes that judge a screenshot, not only count its pixels.

## 5. Web search

**Problem.** Only `read_url` exists, so a worker cannot find documentation, a library's current CDN path or an API reference. `osint-agent` advertises search but has no implementation.

**Interface.** `web_search(query, limit?<=8)` returns `[{title, url, snippet}]`; `read_url` is the follow-up. A configured backend is required: a self-hosted SearXNG URL, or a key-based provider (Brave, Tavily, others) stored through the existing credential store. No scraping of a consumer search page.

**Capability and containment.** `network.public`. Results are wrapped as untrusted data; snippets are length-capped; the tool never follows links itself. Per-execution call and rate budgets; responses cached for the execution.

**Acceptance.** No backend configured returns a clear error naming the setting; a fixture backend returns results; a snippet containing instructions is returned inside the untrusted wrapper unchanged; the call budget is enforced.

## 6. Diagnostics: tests, type checks and linters with structured results

**Problem.** `execute_terminal_command` returns raw output and, once it runs anything that is not an inspection verb, makes the node unreplayable. Workers re-read log text instead of acting on errors.

**Interface.** `run_diagnostics(kind?, path?)` detects what the project has (`package.json` scripts, `tsc`, `eslint`, `pytest`, `go vet`/`go test`, `node --check`) and runs only allowlisted invocations. It returns `{tool, ok, summary, errors:[{file, line, column, message}], truncated}`.

**Capability and containment.** Same execution path as `execute_terminal_command` (container unless native is enabled), same resource limits. Output parsers are pure functions with fixtures. Allowlisted runs are classified replay-safe only when they write nothing outside declared cache folders; otherwise unsafe, as today.

**Acceptance.** Parser fixtures for each tool; an unknown project returns "nothing to run" rather than guessing; a failing test produces file and line; large output is truncated with a count.

## 7. Dev server: `start_server`, `server_logs`, `stop_server`

**Problem.** `run_page` serves static files. Framework projects (Vite, Next) need a dev server; today a worker cannot start one and look at it.

**Interface.** `start_server(command, port?, ready_pattern?)` -> `{id, url}`; `server_logs(id, tail?)`; `stop_server(id)`; `run_page` accepts `url` for a started server (still loopback only).

**Capability and containment.** `process.container` or `process.native` per the user's setting, plus `browser.run` to look at it. Lifetime is bound to the attempt: all servers are stopped when it ends, however it ends. One server per node by default; loopback binding is enforced by port mapping or `--host 127.0.0.1`. A started server is an external effect and therefore unsafe for recovery.

**Acceptance.** Readiness by pattern or port; logs ring buffer; cleanup on timeout and kill (no orphan process); a second start on a used port is an error.

## 8. Edit tools: `apply_patch`, overwrite after read, multi-edit

**Problem.** `write_file` refuses an existing file and `replace_file_content` needs one exact match. With weaker models these produce loops (observed: repeated "Expected exactly one match"), and a resumed node cannot overwrite what an earlier attempt wrote.

**Interface.**
- `apply_patch(patch)`: unified-diff or `*** Begin Patch` style, atomic across files, with fuzzy context matching and a precise failure message (the nearest matching lines).
- `write_file(path, content, overwrite?: true)`: allowed only if `read_file` returned that file earlier in this attempt; the previous content hash is recorded.
- `replace_file_content` accepts a list of `{target, replacement}` pairs and, on no match, returns the closest candidate lines.

**Capability and containment.** `workspace.write`; the same path guards; all three are replay-safe file effects.

**Acceptance.** A multi-file patch is all-or-nothing; a stale-context patch reports the nearest lines; overwrite without a prior read is refused with instructions; a fixture of the observed failure (target text differing by whitespace) succeeds through fuzzy matching.

## 9. File operations: `glob`, `move_file`, `delete_file`, `make_dir`

**Problem.** Workers list one directory at a time and cannot rename or remove a file they wrote wrongly (a mislocated file stays in the output).

**Interface.** `glob(pattern, limit?)`, `move_file(from, to)`, `delete_file(path)`, `make_dir(path)`.

**Capability and containment.** `workspace.read` / `workspace.write`; workspace only; `delete_file` requires that the path was written or read in this execution and records the deletion in the node's file list so Studio shows it. `delete_file` is not replay-safe in general (the file may be gone on a replay), so it is classified unsafe unless the target was created by the same attempt.

**Acceptance.** Traversal and symlink-escape refusals; deletion is visible in the projection; glob output is bounded and sorted.

## 10. Git: status, diff, checkpoint, rewind

**Problem.** There is no per-node record of what changed and no way to undo a failed attempt that left partial files. A resumed attempt starts from unknown state and has to rediscover it.

**Interface.** `git_status`, `git_diff(path?)` read-only. Runtime-owned (not a model tool): each session workspace is a git repository; the dispatcher commits a checkpoint when a node completes and before each attempt, and can `rewind` an attempt that failed with file-only effects before resuming.

**Capability and containment.** The repository lives inside the session folder; the model has read access only; checkpoint and rewind are runtime operations with their own audit events. Studio can show a node's diff.

**Acceptance.** A failed attempt can be rewound to the pre-attempt checkpoint; the node's diff matches its reported files; binary assets are handled (size cap or LFS-style skip).

## 11. Plan and ask: `update_plan`, `ask_user`

**Problem.** Weak models lose track of what is left (the turn-budget nudges exist because of this), and a node that needs a decision can only guess or fail.

**Interface.** `update_plan(steps:[{text, status}])` persists the plan in node memory and shows it in Studio's node view. `ask_user(question, options?)` blocks the node on the human-in-the-loop channel and resumes with the answer.

**Capability and containment.** `ask_user` reuses the approval request mechanism (never expires, bound to a hash of the request) and therefore runs under the cancel-only context. `update_plan` has no external effect.

**Acceptance.** A question appears in Studio with its options; the answer reaches the model verbatim; a killed run ends the wait; plan updates are bounded in size.

## 12. Office documents

**Problem.** Co-working agents spend much of their time on `.docx`, `.xlsx`, `.pptx` and PDF files. Reticle has LaTeX and Markdown only.

**Interface.** `read_document(path)` -> text, tables, slide notes; `write_document(path, spec)` for docx/xlsx/pptx from a structured spec; PDF read and simple PDF write.

**Capability and containment.** `workspace.read`/`workspace.write`; libraries installed through a skill's dependencies into the shared libs folder; no macros, no external links fetched; size caps. Studio previews through the existing file viewer plus a table view.

**Acceptance.** Round-trip fixtures for each format; a document with embedded objects is read without executing anything; large spreadsheets are summarised with row limits.

## 13. Connectors

**Problem.** MCP integration (RFC-046) is local stdio only, and there is no catalogue of ready connectors (drive, mail, calendar, chat, issue trackers).

**Interface.** A curated catalogue of MCP servers with per-server tool allowlists, configured in Studio; streamable-HTTP transport with token storage; consent screen listing exactly which tools a server exposes.

**Capability and containment.** `mcp.call` plus per-server policy (RFC-050). Tokens never reach workers; calls go through the broker. Write-capable tools are unsafe effects and require approval by default.

**Acceptance.** A fixture remote server with a fake token; a tool outside the allowlist is invisible to the architect and the worker; revoking a server removes it from the next catalogue.

## 14. Cross-run memory

**Problem.** `remember` is execution-scoped. Nothing carries what was learned (a working CDN URL, the user's preferences, a project's conventions) into the next run.

**Interface.** Project memory in a reviewable file the user can edit (a `RETICLE.md`-style document) plus structured facts with provenance (RFC-049's query and management surface). Read at node start under a size budget; writes are proposals the user accepts.

**Capability and containment.** Memory never contains secrets; proposals are listed in Studio; nothing is written without review.

**Acceptance.** A fact proposed in one run appears, once accepted, in the next run's context; size budget enforced; secrets rejected.

## 15. Scheduling and always-on use

**Problem.** Personal-agent products act on triggers: a schedule, a message, a file change. Reticle runs only when a person starts a workflow.

**Interface.** A scheduler in Forge: saved prompt + mode + cron expression; notification channels (Studio, OS, webhook); a headless run mode.

**Capability and containment.** Scheduled runs are limited to the capabilities of the saved definition and never auto-approve a checkpoint; a failed unattended run notifies instead of retrying indefinitely.

**Acceptance.** A schedule fires once per window; missed runs are reported, not replayed in a burst; an approval checkpoint holds until a person answers.

## 16. Steering and re-run

**Problem.** A person can only approve or kill. They cannot tell a running node "use the other CDN", re-run one failed node with an edited prompt, or fix a plan before it runs.

**Interface.** Message-to-node (delivered at the next turn), pause/resume, re-run from a node with an optional prompt edit, and a plan editor before approval (RFC-036's interactive canvas).

**Capability and containment.** Steering messages are marked as user instructions in the transcript and are logged; re-run creates a new attempt identity and never reuses an approval.

**Acceptance.** A steering message appears in the node's next request; re-run from a node leaves upstream outputs untouched; an edited prompt is recorded.

## 17. Catalogue honesty

**Problem.** The architect picks agents from the catalogue, and four of them do nothing: their workers run the generator, and the capabilities in their descriptions (Playwright, yfinance, search) do not exist.

**Interface.** Either implement them on the new tools (a browser agent on RFC-053 and `web_search`; a finance connector through section 13) or mark them internal so the catalogue stops listing them. A test fails when a catalogue agent's entrypoint is the compatibility shim.

**Acceptance.** `TestCatalogueAgentsHaveWorkers` passes; the architect catalogue contains only agents whose worker is not the shim.

## 18. Recommended order

| Step | Item | Why now | Size |
| --- | --- | --- | --- |
| 1 | Catalogue honesty (17) | Stops the architect choosing agents that do nothing; an afternoon | S |
| 2 | Edit tools (8) | Removes an observed loop class; small and local | M |
| 3 | Vision (4) | Turns RFC-053 screenshots into judgement; needs router metadata | M |
| 4 | Diagnostics (6) | Gives verification a structured signal beyond pages | M |
| 5 | Web search (5) | Prevents dead-URL class of bug at the source | S-M |
| 6 | File operations (9) | Small; removes mislocated-file dead ends | S |
| 7 | Plan and ask (11) | Reuses the approval channel | M |
| 8 | Git checkpoints (10) | Makes resume and per-node diff trustworthy | L |
| 9 | Dev server (7) | Only for framework projects; after 4 and 3 | M |
| 10 | Steering and re-run (16) | Largest UX change; depends on 10 for clean rollback | L |
| 11 | Office documents (12), connectors (13), memory (14), scheduling (15) | Product surface; independent of the above | L each |

Adjacent work that is not a tool but dominates quality: a coding tier of models with proven tool calling and prompt caching, and fewer, larger nodes with a verify-and-fix loop that runs the integrated page.

## 19. Related

[RFC-036](RFC-036-Human-in-the-Loop-Workflows.md), [RFC-045](RFC-045-Durable-Execution-Capabilities-Effects.md), [RFC-046](RFC-046-MCP-Integration.md), [RFC-049](RFC-049-Memory-Query-Management.md), [RFC-050](RFC-050-Attempt-Scoped-Tool-Broker.md), [RFC-053](RFC-053-Run-Page-Browser-Observation.md).
