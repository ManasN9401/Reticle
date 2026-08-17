# RFC-035: Agent Evolution (The Meta-Scaffolder)

## Status
Proposed (Future Roadmap)

## Context
Writing robust `.yaml` definitions and Python worker scripts for every possible edge case is unscalable. As the framework encounters novel codebases, existing agents may frequently fail syntax checks or timeout. We need a system where agents actively improve themselves over time through iterative feedback, effectively writing and maintaining the framework code autonomously.

## Proposal
Introduce a background "Evolution" orchestrator lifecycle.
1. **Telemetry Feedback Loop:** `forge.exe` actively monitors the success/failure ratios of specific agent IDs (e.g., `react-coder`). If an agent fails compilation or testing consistently, it is flagged for evolution.
2. **The Meta-Scaffolder:** A specialized, high-tier agent (e.g., using a massive parameter model) intercepts the flagged agent's `agent.yaml`, its `worker.py` script, and the raw `stderr` logs of its recent failures.
3. **Autonomous Patching:** The Meta-Scaffolder rewrites the failing `worker.py` script (or refines the `system_prompt` in the YAML) to handle the edge case.
4. **Hot-Reloading:** The new worker script is deployed to the `compiler/workers/` directory, and `forge.exe` hot-reloads it into the active registry via `registry.LoadAgents()`.
5. **Natural Selection:** Over thousands of iterations across numerous projects, brittle agents are pruned and rewritten into highly resilient, codebase-specific specialist agents.

## Consequences
- **Pros:** Unprecedented framework resilience. The system practically writes itself, optimizing prompt structures and Python extraction logic far better than a human engineer could manually maintain.
- **Cons:** High risk of "bad mutations" where an agent accidentally corrupts its own fundamental execution loop, requiring rollback mechanisms or strict sandboxing.
