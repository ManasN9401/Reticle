---
status: draft
owner: Reticle Project
updated: 2026-09-23
---

# RFC-048: Meta-Scaffolder, Revised

## Motivation

`docs/rfc/RFC-035-Agent-Evolution-Meta-Scaffolder.md` proposes a background "Evolution" orchestrator lifecycle: a Meta-Scaffolder agent that monitors other agents' success/failure ratios, rewrites a failing agent's `worker.py` or `agent.yaml` in response, and hot-reloads the result. Its own `Consequences` section already names the risk plainly: "High risk of 'bad mutations' where an agent accidentally corrupts its own fundamental execution loop, requiring rollback mechanisms or strict sandboxing." That RFC's status remains `Proposed (Future Roadmap)` — it was never accepted, and nothing in this repository implements it.

This document is a deliberately narrow, documentation-only deliverable. It does not propose implementing the Meta-Scaffolder. It restates the concept for continuity, carries RFC-035's own risk assessment forward without softening it, and states explicitly what would have to be true before an implementation RFC could responsibly be written. **Accepting this RFC authorizes nothing beyond the gates below being on record — it is not itself a green light to build the Meta-Scaffolder.**

## Design

**The concept, restated.** A telemetry feedback loop watches per-agent success/failure ratios. An agent flagged as consistently failing is handed, along with its `agent.yaml`, its `worker.py`, and its recent `stderr`, to a specialized high-tier model that rewrites the failing script or refines its system prompt. The revised worker is deployed and hot-reloaded via `registry.LoadAgents()`. Over many iterations across many projects, the intended outcome is agents that specialize themselves to a codebase rather than staying generic.

**Required gates before an implementation RFC is written.** These carry forward RFC-035's own stated concern, made concrete rather than left as a one-line risk note:

1. **Sandboxing.** RFC-047 (Plugin System) explicitly flags that plugin-bundled code runs unsandboxed, at the same trust level as a built-in agent. A system that writes its own worker code is a strict superset of that risk — it isn't just running untrusted code, it's generating it. This RFC treats RFC-047's sandboxing gap being closed as a hard precondition, not a parallel concern.
2. **Rollback.** A mutation that corrupts an agent's execution loop must be reversible without operator archaeology — a previous-version pointer per agent, at minimum, with a defined trigger for reverting (repeated post-mutation failures, an explicit operator command, or both).
3. **Mutation review surface.** Before a mutation goes live, Studio (or an equivalent surface) needs somewhere to show what changed and why, even if the initial version requires a human to approve it rather than auto-deploying. Silent self-modification of the framework's own execution code is a materially different risk posture than silent self-modification of, say, a generated workflow's parameters.
4. **Bounded blast radius.** The node-budget and graph-mutation-validation machinery `runtime/agent/workflow_engine.go` already applies to *dynamic delegation* (a supervisor spawning new nodes) is the right shape of precedent — mutations should be resource-bounded and validated before commit, not applied optimistically.

## Drawbacks

Everything RFC-035 already says: unbounded self-modification of execution-critical code is the highest-risk category of change this framework could make to itself, and getting the gates above wrong is worse than not building the feature. This document does not resolve that risk — it records what would need to be resolved, and by whom, before a future RFC could propose resolving it.
