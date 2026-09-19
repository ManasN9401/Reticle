# Reticle Orchestrator

Reticle is a local-first, polyglot agent orchestration framework. Its Go runtime executes bounded directed acyclic graphs, and its Forge compiler can generate execution-scoped workflows and Python workers from a prompt.

## Core Features

- **Autonomous Compilation Pipeline (`forge`)**:
  - Simply pass a prompt like `"Build a 2D Platformer"`.
  - The runtime dynamically provisions an **Architect** to design a Directed Acyclic Graph (DAG), a **Scaffolder** to generate physical structure, and a **Coder** to synthesize bespoke, specialized Python agents on the fly.
- **Polyglot Execution**: A blazing-fast Go-based orchestrator that communicates with isolated Python workers via versioned JSON over STDIO.
- **Shared Worker Tools**: Compiled workers use a common SDK with contained file operations, bounded execution and explicit verification. The compiler writer aggregates file maps; it is not automatically injected into task workflows.
- **Bounded routing and retry**: The router selects an enabled model using capability, capacity, cooldown, and observed outcome scores. Forge owns a bounded retry policy for failures classified as transient and safe to retry.
- **Selected workspace context**: Workers receive bounded IDE context, declared shared-memory facts, upstream artifacts, and contained workspace tools. They inspect additional files as needed instead of injecting the entire source tree into every prompt.
- **Telemetry and Studio**: The loopback control server exposes authenticated events and artifacts. Reticle Studio projects workflow snapshots, logs, files, dependency activity, approvals, and artifacts into a desktop UI.

## Architecture

Reticle consists of two primary systems:

### 1. The Runtime Engine
Located in `runtime/`, the Go orchestrator manages the lifecycle, event bus, and execution of DAGs.
- **GraphEngine**: Parses `workflow.yaml` files and manages cross-layer dependencies.
- **Memory System**: Handles multi-scope (Global, Workflow, Execution) variable injection.
- **Workers**: Isolated processes (e.g., Python scripts) that subscribe to the event bus and process node executions.

### 2. The Forge Compiler
Located in `cmd/forge/`, the compiler acts as a meta-workflow. It transforms a natural language objective into a fully functional workspace containing a custom DAG and dynamically generated Python agents.

## Getting Started

1. **Build the compiler:**
   ```bash
   cd cmd/forge
   go build -o forge.exe
   ```

2. **Setup your API Keys:**
   Create a `.env` file in the root directory:
   ```env
   GROQ_API_KEY=your_groq_key
   OPENROUTER_API_KEY=your_or_key
   ```

3. **Run a Prompt:**
   ```bash
   .\forge.exe -port 8080 "Build a pixel art tetris clone"
   ```

4. **Follow-On Edits (Iterative Development):**
   ```bash
   .\forge.exe -workspace ./workspaces/forge_workspace_XXX "Make the blocks fall 50% slower"
   ```

## Telemetry GUI

When running `forge`, the telemetry server boots automatically. Navigate to `http://localhost:<PORT>` (default 8080) to monitor the execution in a high-tech visualizer. Waitlist queues, task durations, and physical output artifacts are instantly mapped and interactive.


See [current documentation](docs/README.md), [environment configuration](docs/environment.md) and [audit repair status](docs/audit/2026-09-08/03-repair-status.md). The control server binds loopback and requires an access code; Studio reads it automatically. Docker is the default command environment. Choose native execution explicitly for trusted host commands.
