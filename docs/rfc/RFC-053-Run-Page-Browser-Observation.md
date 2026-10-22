---
status: accepted
owner: Reticle Project
updated: 2026-10-13
---

# RFC-053: Run-Page Browser Observation

## 1. Purpose

Give a worker a way to load the website it is building in a real browser and read back what the browser saw, so that "the page works" is something it can observe instead of something it asserts.

## 2. Motivation

Static checks (`check_web_page`) parse scripts and follow imports. They cannot see anything that only happens when the page runs. Two observed runs show the gap:

- A generated three.js gallery had 45 files and 457 KB of JavaScript. Three modules in its entry graph did not parse, and the integration node still completed.
- After those were repaired, `run_page` on the same site reported `ReferenceError: THREE is not defined` and a blank screen. The cause was a CDN URL that does not exist (`cdnjs.../three.js/r158/three.min.js` answers 404; the browser reports `ERR_BLOCKED_BY_ORB`). No static check can know that a remote URL is dead. A QA node briefed to "run browser tests and report console errors" had no browser and spent its turns on shell commands.

The tool is the smallest thing that closes that loop: run the page, return console errors, exceptions, failed requests and whether anything is on screen.

## 3. Scope

In scope: one worker tool, `run_page`, behind a new capability `browser.run`; a stdlib DevTools client for an installed Chrome or Edge; an optional Playwright backend; a loopback static server; a bounded text report and a screenshot file.

Out of scope here (see [RFC-054](RFC-054-Agent-Tool-Surface-Roadmap.md)): letting the model *see* the screenshot, a dev-server tool for framework projects, test and lint runners, cross-origin or authenticated flows, recording video or traces.

## 4. Design

### Tool

`run_page(path?, wait_ms?, viewport?, screenshot?, actions?)`

| Argument | Meaning |
| --- | --- |
| `path` | The `.html` file relative to the workspace; default the first page found. |
| `wait_ms` | How long the page runs before it is observed. Default 3000, at most 15000. |
| `viewport` | `{width, height}`, default 1280 x 720. |
| `screenshot` | Save a PNG, default true. |
| `actions` | At most 20 of `{click: selector}`, `{click_at: [x, y]}`, `{press: key, hold_ms}`, `{scroll: dy}`, `{wait_ms}`, `{eval: expression}`. |

The result is a text report of at most 6000 characters: load outcome and timing; uncaught exceptions and console errors with `file:line`; failed requests (with a hint for the common `ERR_BLOCKED_BY_ORB` and `ERR_NAME_NOT_RESOLVED`) and HTTP 4xx/5xx responses; the page title; each canvas and whether any code ever requested a context from it; whether `requestAnimationFrame` callbacks ran during the wait; a blank-screen verdict computed from the screenshot; any `alert`, `confirm` or `prompt` the page opened (accepted automatically, because nobody is there to answer and an unanswered dialog would otherwise look like an endless loop); `eval` results; the screenshot path; a one-line result. `favicon.ico` noise is ignored.

The screenshot is written to `run_page/<page>-<n>.png` in the workspace and never overwritten, so it is a file the run produced: it is listed with the node's files and opens in Studio's image viewer.

### Browser backends

1. **Playwright (preferred when importable).** The package is the more complete and better maintained driver: dialogs, frames, popups and actionability waits are handled for it. It launches the system Chrome or Edge through `executable_path` when one is found (so nothing is downloaded), and otherwise its own browser if that is installed. It feeds the same observation record and report, and applies the same containment through route interception. Playwright's calls carry no timeout, so a page stuck in a loop would otherwise hang the caller; the adapter therefore runs in a child process that the parent kills (browser and driver included) at the deadline, keeping the progress the child had checkpointed and reporting the page as unresponsive. If it cannot start and a system browser exists, the call falls back to the built-in client and says so in the report; a forced backend never falls back silently.
2. **System browser over the DevTools protocol (zero-install fallback).** `find_chromium()` looks at `RETICLE_BROWSER`, then the usual Edge and Chrome install locations, then `PATH`. The browser is launched headless with a throwaway profile and `--remote-debugging-port=0` (with software GL, so WebGL works without a GPU), and driven by a small RFC 6455 client written against the standard library. No package is installed and no browser is downloaded, so there is no setup step that can fail.

`RETICLE_BROWSER_BACKEND=cdp|playwright` forces a backend. When neither is available the tool returns an error that says what to install or set. The driver is attached to the capability, not to a skill: an agent granted `browser.run` receives `playwright` (pinned) as an *optional* dependency, so it is available whichever skills the agent has. Optional means best-effort: it is installed on first use into a shared folder, a failed install is logged and not retried for an hour, and the worker runs with the built-in client instead of failing at environment setup.

### Containment

The page is LLM-written code. The tool therefore:

- serves the workspace's `src` folder read-only from `127.0.0.1` on an ephemeral port (GET/HEAD, no dotfiles, traversal refused, `Host` checked);
- uses a profile directory that is deleted afterwards and no extensions;
- intercepts every request. Allowed: the local origin; `data:`, `blob:`, `about:`; GET/HEAD/OPTIONS to public `http(s)` hosts, because CDN scripts, fonts and textures are normal. Refused and reported: any other method to a remote host (nothing is sent out), private, link-local and reserved addresses, any other local port, other schemes, and WebSockets to remote hosts;
- enforces an overall deadline (default 45 s, at most 120 s) on every protocol call, so a page stuck in an endless loop is reported as `PAGE UNRESPONSIVE` instead of hanging the node;
- kills the browser's whole process tree on every exit path.

Correction (2026-10-21): public GET requests can send data through URLs and headers. The implementation now classifies `run_page` and `browse_web` as unsafe for automatic replay. The URL filter does not enforce an OS network boundary; the original no-exfiltration premise was incorrect. See [current environment limits](../environment.md).

### Capability

`browser.run` is a new, explicit capability. It is in the runtime's known set, in `schemas/agent.schema.json` and `schemas/task.schema.json`, and in the architect catalogue together with the tool and its constraint. It is not part of the v1 compatibility default profile: a manifest that does not declare it does not get a browser. The architect is told to grant it to the integration and QA nodes that verify a page, not to every node.

### Guidance

The `run-and-observe` skill is a small instruction file: static check first, how to read the report in order, common causes (a dead CDN URL, a library that loads after its user, a render loop that never starts), and when to stop. `verification-loop` mentions `run_page` conditionally. The architect's capability statement depends on whether the catalogue offers the tool.

## 5. Principles

- Observation, not assertion: the node reports what the browser reported.
- Honest limits: software rendering is slower than a GPU, and the tool says so instead of flagging a low frame rate as a defect.
- Containment by default: untrusted code gets read access to the public web and write access to nothing.
- Advisory, not mandatory: a clean `run_page` is not a completion gate, because a machine without a browser must still be able to complete. `check_web_page` remains the gate.

## 6. Trade-offs

- Playwright is preferred for its completeness and upkeep, at the cost of a package (about 110 MB, one shared copy) that may fail to install, which is why it is an optional dependency with a fallback. Keeping a small DevTools client as the fallback costs about 350 lines and a test suite, in exchange for a path that needs nothing installed and cannot fail at environment setup.
- A real browser per call costs about 3 to 6 seconds; the tool is meant to be called a handful of times per node.
- The blank-screen check is a pixel statistic, not understanding. It catches the common failure (nothing drew) and nothing subtler; seeing the screenshot is [RFC-054](RFC-054-Agent-Tool-Surface-Roadmap.md) section 4.

## 7. Known gaps

- The model cannot look at the screenshot (no vision input yet).
- No video, trace or network HAR output; no multi-page flows with state beyond `actions`.
- Pages that need a real GPU for acceptable frame rates are only checked for liveness.
- Framework projects that need a dev server or a build step are not served by the static server ([RFC-054](RFC-054-Agent-Tool-Surface-Roadmap.md) section 7).

## 8. Verification

The Playwright adapter is also exercised against the real package when it is importable (`RealPlaywright` in `tests/test_browser_tools.py`). That run found two adapter bugs the stand-in could not: a failed CDN script was filtered out as "blocked", and the sandbox's own aborted requests were counted as page errors. Both are fixed. Running the whole scenario matrix (healthy, broken, blank, dialogs, WebGL, actions, exfiltration, hung page, a real broken site) under the interpreter the runtime provisions, once per backend setting, found a third: a hung page was not bounded under Playwright. That is why the adapter is supervised.


`tests/test_browser_tools.py` covers the WebSocket client, containment decisions, PNG statistics, the site server, report formatting, backend selection and the Playwright adapter against a stand-in, and (when Chrome or Edge is installed) drives a real browser through a healthy animated page, exceptions and missing files, a blank page, an unused canvas, WebGL, blocked exfiltration, actions and a hung page. `tests/test_worker_run_page.py` covers registration, argument validation, effect classification and a real run through the worker. Go tests cover the capability and the catalogue entry.

## 9. Related

[RFC-045](RFC-045-Durable-Execution-Capabilities-Effects.md) (capabilities and effects), [RFC-050](RFC-050-Attempt-Scoped-Tool-Broker.md) (tool broker), [RFC-054](RFC-054-Agent-Tool-Surface-Roadmap.md) (what is still missing).
