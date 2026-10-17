---
status: accepted
owner: Reticle Project
updated: 2026-10-16
---

# RFC-055: Specialist Agents (Browser, OSINT, Quant, Hermes)

## 1. Purpose

Make four registered agents do what their descriptions say. `browser-agent`, `quant-agent`, `osint-agent` and `hermes-coder-agent` were shells: their workers ran the code generator (`coder.py`), so the architect, choosing them for Playwright, yfinance, Google search or a read-eval-print loop, got a generic coder that was told to install those things in a terminal. In a container, where nothing persists between commands, that never worked.

## 2. What each agent is now

| Agent | Kind | Tools it adds | Capabilities | Skill |
| --- | --- | --- | --- | --- |
| `browser-agent` | `browser` | `browse_web` | `workspace.*`, `network.public`, `browser.run` | `browser-automation` |
| `osint-agent` | `osint` | `web_search`, `fetch_pages` | `workspace.*`, `network.public` | `osint-investigation` (rewritten) |
| `quant-agent` | `quant` | `market_quote`, `price_history`, `financial_statements`, `technical_indicators`, `risk_metrics` | `workspace.*`, `network.public`, `process.container` | `quant-finance` (pinned `yfinance`, `pandas`, `numpy`) |
| `hermes-coder-agent` | `hermes` | none; a completion rule and a failure loop over `execute_terminal_command` | `workspace.*`, `network.public`, `process.container`, `process.native` | `hermes-repl` |

Each is a registered worker that calls the shared worker loop with its own `kind`; the tools exist only for that kind and only when the capabilities are granted. A generic generated worker never gets them.

## 3. Decisions

**Browser (`browse_web`).** A supervised child process drives Playwright, which the agent receives as the optional `browser.run` dependency. It reuses the `run_page` containment (public destinations only, every request checked including redirects, a throwaway profile, a deadline enforced by a parent that kills the browser) with different defaults: it is **read-only** (only GET/HEAD/OPTIONS leave the browser) and `allow_submit=true` additionally lets forms post to the site being browsed, never to a third party. Downloads are refused, popups closed, dialogs dismissed. It reports an `ACCESS WALL` and stops at a CAPTCHA, consent wall or HTTP 401/403/429; it does not try to get around one. Text comes from the page's main content when it has one. A browse that may submit is not replay-safe; a read-only one is.

**OSINT (`web_search`, `fetch_pages`).** Search uses an **official API**: Google results through Serper, Brave, or a self-hosted SearXNG server. A consumer search page is never scraped (RFC-054 section 5 rules it out, and the attempt was rate-limited within minutes). With nothing configured the tool says what to set; Wikipedia's API works without a key but only when asked for by name, so the agent knows it is searching a narrower source. `fetch_pages` reads up to 20 pages in parallel, respects `robots.txt`, sends nothing to a site, refuses private addresses (connecting to the address it checked, re-checking every redirect), and saves every page under `research/sources/` with its URL, retrieval time and a SHA-256, indexed as `S1, S2, ...` in `research/sources.json`; `research/search-log.jsonl` records every query. The skill is rewritten: passive and public sources only, no credentials or breached data, and a private individual only with a stated legitimate purpose. The old skill told the agent to install username-enumeration and footprinting tools inside the container, where installs do not persist, and to file "Credentials" among its findings.

**Quant.** The arithmetic is done in pandas, not by the model: indicators (SMA, EMA, RSI, MACD, Bollinger, ATR), annualised return and volatility, Sharpe, drawdown, beta and correlation, with statements about the latest bar. Every table is saved as CSV under `data/`. Output says the source is Yahoo Finance via yfinance, may be delayed or adjusted, and is not investment advice. The dependencies are pinned (`locked` policy) in a shared environment.

**Hermes.** `mark_task_complete` is refused until `execute_terminal_command` has returned exit code 0, running something other than an inspection, **after the last change** to the workspace. A failed run gets the next step of the loop appended (read from the bottom, name the cause, change one thing); the same error three times is called out and the agent is told to change approach. If the terminal itself cannot run (no Docker), the gate lifts, the model is told plainly to say the code was not run, and the node is not trapped. Hermes loops through the terminal rather than a persistent in-process interpreter: running model-written code inside the worker, unsandboxed, is what the container exists to prevent.

**Environment.** The worker environment is an allowlist. `RETICLE_BROWSER` and `RETICLE_BROWSER_BACKEND`, documented since RFC-053, never reached a worker and now do. `SERPER_API_KEY`, `BRAVE_API_KEY`, `SEARXNG_URL` and `RETICLE_SEARCH_PROVIDER` reach `osint-agent` only, as `ml-agent` alone receives its tokens.

## 4. Guards

- `tests/test_catalogue_agents.py` fails when a catalogue agent's entrypoint is the code-generator shim (RFC-054 section 17), when a specialist does not run the worker loop for its kind, and when a description promises a tool the worker does not define.
- `runtime/agent/specialists_test.go` loads the real definitions and checks each agent's skill, granted capabilities and the ones it must not have (OSINT has no terminal and no browser; the browser agent has no terminal).
- `tests/test_skill_tool_references.py` covers the new skills: a skill cannot name a tool the agent is not offered.

## 5. Verification

Each agent's real entrypoint was run through the real worker loop with a scripted model and live tools: OSINT searched Wikipedia and fetched two pages into cited sources; the browser agent opened example.com and followed its link to IANA; the quant agent quoted AAPL and MSFT and computed risk and indicators from Yahoo data; Hermes failed a test, fixed the code and passed, and was only then allowed to finish. A test over a local site covers browsing (extraction, actions, popups, dialogs, an access wall, a page that never yields, and that nothing is sent out by default).

## 6. Not done

- No agent has yet completed a full Forge run: model availability, not these agents, has been the limit.
- `browse_web` needs Playwright and a Chromium-based browser; there is no DevTools fallback for it (it exists for `run_page`).
- `fetch_pages` does not run JavaScript; a page that needs it reads as nearly empty and the skill says to use `browse_web`.
- No market data other than Yahoo; no order, account or brokerage access, by design.
- Hermes's container has Python and pip but no node, npm or git; for other toolchains it needs native execution or a different `RETICLE_WORKER_IMAGE`.
