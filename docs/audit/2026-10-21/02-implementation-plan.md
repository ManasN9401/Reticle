---
status: recommendation
owner: Reticle Project
updated: 2026-10-21
---

# Implementation priorities and a credible route to competitive performance

This plan accompanies [the production-readiness audit](01-production-readiness.md). It recommends future work; it does not accept a draft RFC or authorize a release. Priorities are based on the checked implementation and current official product documentation, retrieved 21 October 2026.

## 1. What is missing: tools, skills or rules?

All three matter, but the largest gaps sit in the runtime and the feedback loop that decides whether work is correct.

| Layer | What Reticle has | What should change | Priority |
| --- | --- | --- | --- |
| Runtime enforcement | Capabilities, broker grants, attempt IDs, deadlines | Enforce grants on final responses, preserve empty grants, isolate untrusted execution and enforce network policy | First |
| Verification | File rereads, shell checks, static page checks, browser reports | Bind checks to artifact hashes; require real acceptance outcomes and repair failures | First |
| Execution environment | Host Python workers and disposable command containers | Reproducible Python/Node/Go project profiles, persistent project dependencies, managed dev servers | First |
| Continuity and recovery | Durable graph state, interrupted recovery, file continuation | Persist conversation/task state, pending inference and effect records; checkpoints and reliable cleanup | First/next |
| Core tools | Basic files/search/edit/commands, browser, role-specific research | Vision, structured diagnostics, patch transactions, semantic navigation, process lifecycle and shared research | Next |
| Skills | YAML metadata plus whole SKILL.md injection | Registered resource roots, lazy loading, version/hash provenance, applicability/effectiveness tests | Next |
| Rules | Manifests, runtime checks, prompts and skills | Separate executable policy from scoped repo instructions; deterministic precedence and visible provenance | First/next |
| User collaboration | Approval, pause, kill and run views | Structured questions, live steering, editable plans, resume/retry and per-attempt diffs | Next |
| Evaluation and cost | Unit/contracts; narrow memory eval; route scores and prices | End-to-end task evals, token/cost receipts, budgets, comparative baselines and outcome-driven routing | Start immediately |
| Product delivery | Source-checkout companion and build scripts | Tested installation/upgrade, clear support matrix, license, release provenance and support diagnostics | Before release |

This agrees with [RFC-054](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md): structural quality matters more than the length of the tool list. It also respects [the roadmap's hardening phase](../../ROADMAP.md), [RFC-045's recovery limits](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md), and [RFC-048's isolation preconditions](../../rfc/RFC-048-Meta-Scaffolder-Revised.md).

## 2. Comparison with current harnesses

This is a documentation-based capability comparison, not a measured ranking. Product options, operating systems and deployment profiles differ; no universal security or performance superiority is implied.

| Capability | Current reference point | Reticle implication |
| --- | --- | --- |
| Enforced execution policy | OpenAI's [security cookbook](https://learn.chatgpt.com/docs/enterprise/security-cookbook) describes managed permission profiles, command rules, independent approvals and correlation of actions with network activity. | Keep the broker, but enforce the policy on every action path and record runtime evidence. |
| OS-backed command isolation | Claude Code [sandboxing](https://code.claude.com/docs/en/sandboxing) distinguishes filesystem/network enforcement from permission prompting. Its documented scope and platform limitations matter. | Specify exactly which Reticle processes are contained; an SDK allowlist is a different boundary. |
| Rewind and conversation continuity | Claude Code [checkpointing](https://code.claude.com/docs/en/checkpointing) tracks edits and supports restoring prior code/conversation states, with documented limits. | Implement workspace checkpoints and durable session state; do not equate them with undoing arbitrary external effects. |
| Skills as usable resources | Claude Code [skills](https://code.claude.com/docs/en/skills) supports metadata-based discovery and detailed resources loaded when needed. | Register actual resource roots and add on-demand loading rather than expanding every prompt. |
| Integrated agent loop | [Deep Agents](https://docs.langchain.com/oss/python/deepagents/overview) combines filesystem context, subagents, memory, summarization/offloading and approval features. | The baseline is an integrated experience, not individual tool names. |
| Persistent state | [LangGraph persistence](https://docs.langchain.com/oss/python/langgraph/persistence) separates thread checkpoints from cross-thread stores. | Separate conversational progress, shared execution facts and accepted project knowledge in the product model. |
| Outcome evaluation | Anthropic's [agent evaluation guidance](https://www.anthropic.com/research/demystifying-evals-for-ai-agents) distinguishes the final environment outcome from what an agent says happened. | Score delivered work and actual effects; final prose and process success are insufficient. |

Reticle's opportunity is a well-integrated local workflow system that can route across models, show why decisions were made, recover interrupted work and expose the real cost and quality of results. This is a plausible product direction. The audit supplies no measured evidence that it currently wins that comparison.

## 3. The execution model to build toward

Keep the existing Go control plane and broker. Add one persistent task controller that maintains the user's objective, constraints, accepted plan, outstanding questions, changed artifacts and verification state. It should choose between direct work, a specialist call and a DAG according to task needs. Every task should not have to pay for the same architect/scaffolder/compiler sequence.

The controller should continue an observation-and-repair loop until acceptance criteria pass, it needs a real user decision, a budget is exhausted, or a concrete blocker remains. Delegate bounded work when the expected quality or latency benefit is measurable. Give parallel workers separate worktrees or explicit write ownership; merge their changes under one integration owner. A DAG is useful for genuine dependencies, but does not automatically provide shared understanding or integration correctness.

Persist the state that matters at turn/tool boundaries: instructions and their provenance, plan, decisions, tool call identities, artifacts/digests, verification receipts, effect uncertainty, pending inference and user steering. A model/provider failure should not require forgetting the task or replaying a tool. This develops [RFC-052](../../rfc/RFC-052-Request-Level-Inference-Recovery.md) while preserving [RFC-045](../../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md).

Keep worker implementations small. The current generated SDK wrappers are a useful simplification; adding more generated executable framework logic would increase the validation and recovery burden.

## 4. Tool work in useful order

### 4.1 Verified execution and observation

1. **Project environment profiles:** prepared Python and Node environments first, followed by Go and other demonstrated demand. Pin the full dependency/image identity, cache installs safely, and expose detected versions to the worker. Commands and the worker's Python packages must not be confused as the same environment.
2. **`run_diagnostics`:** typed test/build/lint/typecheck results with command identity, exit status, collected-test count, relevant files, artifact hashes and bounded full-output references. A successful help/version invocation must not satisfy completion.
3. **Managed processes:** `start_server`, `server_logs`, `stop_server`, readiness checks and cleanup tied to attempt ownership. Prefer a runtime service manager to models constructing detached shell commands.
4. **Visual observation:** `view_image` and screenshot content parts, routed only to vision-capable models. Add image size budgets and explicit compaction rules. Reticle already produces screenshots, so this unlocks a current blind spot.
5. **Browser acceptance:** retain static checks and `run_page`, but represent outcomes structurally and test user journeys. Require relevant observations after the final edit when web behavior is part of acceptance.

These correspond to [RFC-054 sections 4, 6 and 7](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) and extend [RFC-053](../../rfc/RFC-053-Run-Page-Browser-Observation.md). Correct A05's network model before widening browser access.

### 4.2 Reliable editing and code understanding

- Add `apply_patch` with multi-file validation and transaction semantics. Preserve existing paged reads, digest-checked overwrite and `apply_edits`; avoid building another inconsistent edit path.
- Make multi-file updates atomic or recoverable, and give edits runtime-owned receipts. Reconcile stale locks and partial creation after interruption.
- Add symbol/reference/diagnostic lookup through language services where useful. Literal search is useful but not sufficient for large refactors.
- Add runtime-owned Git checkpoints, per-attempt diffs, branch/worktree isolation and explicit rewind. Never silently reset the user's repository or overwrite unrelated changes.
- Revisit the session `src/` alias contract when supporting arbitrary existing repositories: conventional project `src/` directories must remain unambiguous. Any change needs a versioned migration because current specs intentionally normalize one prefix.

See [RFC-054 sections 8–10](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md) and [worker protocol paths](../../specifications/worker-protocol/v1/001-protocol.md).

### 4.3 User control and shared research

- Add structured `ask_user`, `update_plan` and live steering, with persisted responses and clear pause/cancel behavior. An approval question and a missing-information question are different interactions.
- Allow targeted rerun/resume with an edited brief, preserving upstream outputs and giving the new attempt a fresh identity and authorization check.
- Expose the existing research capabilities to appropriate generic workers through shared descriptors and budgets, rather than requiring every documentation lookup to become an OSINT node.
- Add broader document tools, remote MCP/OAuth and curated service connectors after effect authorization and reconciliation are dependable. Mail/calendar/deployment writes need concrete approval semantics.

These follow [RFC-054 sections 5, 11–16](../../rfc/RFC-054-Agent-Tool-Surface-Roadmap.md). Scheduling and broad connector catalogues are later product choices, not prerequisites for making the coding loop work.

## 5. Skills and rules need operational contracts

### Skills

A production skill should have an ID/version, package root, content digest, applicability description, resource list, declared prerequisites, expected outputs and an executable validation recipe. Resolve it through the registry; do not guess its location from its ID. Expose short metadata first and load instructions/resources when relevant. A token budget should be based on actual content, not merely a fixed count of three skills.

For each important skill, keep should-trigger and should-not-trigger cases and a few outcome tests. Record which version actually reached the model. Fail explicitly when a required instruction/resource is missing; permit dependency-only skills only when declared as such. Cached summaries must retain essential constraints and identify their source version.

Start with tested workflows for repository orientation, bug reproduction and repair, integration verification, browser/UI acceptance, and failure recovery. Reticle already has many relevant skills; improve their loading and effectiveness before adding a large catalogue. [Specialized skills](../../specialized_skills.md) accurately explains the present limitations and distinguishes reference-only examples from active functionality.

### Rules

Separate two systems:

- **Executable policy:** permission grants, filesystem/network boundaries, effect classes, approval requirements, budgets and runtime lifecycle. Models and skill text cannot expand it.
- **Task guidance:** user intent, repository/subdirectory conventions, architecture decisions and skill procedures. Give these a documented precedence order, provenance and a visible context budget. Retrieved pages and documents remain task data and must not silently become higher-priority instructions.

Add a scoped repository instruction resolver, with explicit handling of `RETICLE.md` or compatible `AGENTS.md` files. Show what was loaded and why. When a rule says a worker must verify a build, give the runtime an acceptance predicate that can enforce it. Longer prose is not a substitute for an executable check.

Because capabilities and protocols are versioned contracts, apply these changes through [the RFC process](../../governance/RFC_PROCESS.md), preserving a deliberate compatibility path.

## 6. Delivery sequence and exit criteria

No calendar estimates are given: team size, available engineering time and release scope were not supplied. Each stage has observable acceptance criteria.

| Stage | Deliverable | Exit criteria |
| --- | --- | --- |
| 0: Establish truth | Add both Python suites to CI; repair contradictory documentation; freeze representative task fixtures | Every advertised feature is labelled implemented, limited or proposed and linked to an exercised check. |
| 1: Close correctness boundaries | A01–A07, RAG production-path test, explicit empty grants and accurate effect classification | Protocol bypass, empty-grant, GET/data, invalid-schema and false-verification regression cases are rejected as intended. No product failure is hidden by an environment skip. |
| 2: Make useful work repeatable | Runtime-owned verification, project environments, managed servers, vision, robust edits, A08–A09 | A real app can be built, run, observed, repaired and verified in container mode; interruption leaves no unrecoverable lock or orphan server. |
| 3: Preserve progress | Durable task transcript, pending-inference recovery, effect ledger integration, checkpoints and steering | Kill/restart and provider failures retain the user's constraints and completed work; uncertain external effects reconcile before retry; targeted reruns show exact changes. |
| 4: Productise | Install/update/rollback, supported platform matrix, license and provenance, support export, retention | A clean supported machine can install, complete a representative task, restart/recover and upgrade without a source checkout requirement unless that remains an explicit product choice. |
| 5: Demonstrate differentiation | Frozen comparative evaluation and routing/cost optimisation | Published results show repeatable gains on the declared target workload under comparable models and budgets. |

Evaluation instrumentation should start in stage 0 and continue through every stage. Broad marketplaces, distributed schedulers and self-modifying agents should wait for demonstrated demand and the applicable safety/recovery gates.

## 7. How to establish that Reticle beats a modern harness

Build a small 30-task development set first; then create a separate frozen benchmark of at least 100 representative tasks. Suggested categories are existing-repository bug fixes, cross-file changes/refactors, frontend integration/visual checks, and multi-step data/research/tool workflows. Include crash recovery, provider failure, deceptive success, instruction injection and repeated-effect fixtures alongside quality tasks.

Run two comparisons:

1. **Harness comparison:** same model/version, starting repository, task prompt, resources and spend/time cap, where supported. This estimates what the harness contributes.
2. **Product comparison:** each product's recommended configuration, with cost and hardware reported. This estimates the experience a user buys. Do not attribute a stronger model's gain entirely to the harness.

Use several trials per task, independent outcome checks and held-out acceptance tests. Record:

- verified task completion and retained regression tests;
- false-success rate, destructive/unapproved effects and repeated external effects;
- human interventions and prompts required;
- end-to-end latency, time to first useful change and failure-recovery time;
- input/output/cache tokens, actual monetary cost and cost per verified completion;
- context loss, abandoned work and reproducibility across fresh environments.

For a claim of superiority, predeclare a meaningful target. Illustrative targets are a 10-percentage-point completion gain at equal spend, or 25% lower cost at matched completion quality, with uncertainty intervals and no observed authorization regressions in the safety fixtures. These are proposed decision thresholds, not results or guarantees. A small or noisy experiment is not evidence of a win; use paired analysis and report per-task failures as well as averages.

Route on verified outcomes per task category. Preserve the distinction already present in `runtime/routing/router.go` between an exponential moving score and a calibrated probability. Add cost reservation before a call, settlement afterward, hard execution budgets and a sensible treatment of unknown prices. Count local-model latency and resource use even when API cost is zero.

The current [memory-quality harness](../../../tools/eval_memory_quality.py) can remain one component of this evaluation. It should not stand in for end-to-end product evidence.

## 8. Recommended first implementation batch

Start with a focused hardening change set: final-response authorization, empty-capability preservation, RAG path filtering, safe retry classification, version/help rejection, strict broker schema admission, crash-safe edit locks, and compiler-suite CI inclusion. Design the browser/container egress fix alongside that work because it affects security claims and replay semantics.

The next product slice should deliver a complete Node/Python application loop: prepared environment → edit → run diagnostics → start app → observe browser/image → repair → verified finish → show diff. Add durable continuation and steering immediately around that slice. This concentrates effort on behavior users can test and gives the benchmark a meaningful target.
