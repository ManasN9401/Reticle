# RFC-034: Complex Agentic Workflows via ReAct

## Status
Proposed (Future Roadmap)

## Context
Currently, our Python workers (`coder.py`) execute as static "one-shot" scripts: they take an input context, execute a single `litellm` completion, output JSON patches, and exit. To handle high-complexity tasks (debugging obscure errors, scraping documentation, verifying test suites), agents need to be stateful and capable of executing tools iteratively.

## Proposal
Upgrade the Worker Protocol to support an asynchronous ReAct (Reasoning and Acting) loop integrated natively with the Go orchestrator Event Bus.

1. **Stateful Execution:** The `worker.py` enters a loop.
2. **Intermediate Artifacts:** Instead of only emitting `ArtifactType: final`, the agent can emit `ArtifactType: tool_call`.
3. **Orchestrator Execution:** The Go orchestrator intercepts the `tool_call` JSON over `stdout`, parses it, executes the requested action (e.g., terminal command, web request, AST extraction), and injects the result back into the agent's `stdin`.
4. **Memory Persistence:** Allow agents to serialize persistent contextual memories via a dedicated `MemoryStore` API endpoint, ensuring context survives between execution nodes in the DAG.

## Consequences
- **Pros:** Massive capability jump. Agents can self-verify their code by running the compiler, reading the error, and rewriting the patch autonomously before concluding their node in the DAG.
- **Cons:** Significantly increases execution duration and token consumption. Requires complex infinite-loop detection and token budget management in the Go orchestrator.
