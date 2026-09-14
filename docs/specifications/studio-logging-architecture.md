# Studio Logging Architecture & Recent Fixes

This document outlines the logging pipeline for Reticle's React-based Studio Frontend, how logs are parsed and routed to specific UI tabs, and documents recent architectural fixes implemented to stabilize Agent orchestration.

## Studio/Frontend Logging Architecture

The Studio UI isolates logs into specific diagnostic tabs on a per-node basis using a combination of the global Zustand store (`store.ts`) and the `Inspector.tsx` view component. 

### 1. Log Ingestion & State Management (`store.ts`)
When a node executes, its stdout/stderr streams and IPC structured events (`WorkerLog`) are intercepted by the Go runtime and shipped to the Electron main process via websockets. The main process pushes these to the React frontend through a bridge (e.g. `bridge.logs.onBatch`). 

Inside `store.ts`:
- Each log chunk is stored with metadata: `timestamp`, `level` (warn for stderr, info for stdout), `message`, and crucially, an `agentId` (e.g. `exec-123__frontend-agent`).
- **LLM Log Detection**: A helper function (`isLlmLog`) evaluates the text of incoming chunks. If the line contains `[LLM]` or `[LLM_STREAM]`, the chunk is strictly flagged with the boolean `isLlm: true`.

### 2. Log Rendering (`Inspector.tsx`)
When a user clicks on a node in the graph, the `Inspector.tsx` component pulls logs filtered specifically for that node's `agentId`. It distributes the output into three primary tabs:

- **Overview Tab**: Displays high-level status, inputs/outputs, and critical failures. If the node crashes (exit code > 0), the Go backend attaches the entire raw stderr buffer to `node.failure.stderr`. To prevent the UI from being cluttered with raw JSON LLM token streams, `Inspector.tsx` actively filters out lines starting with `[LLM_STREAM]` before rendering the red failure box.
- **Log Tab**: Displays standard application logs, filtering `records.filter(r => !r.isLlm)`.
- **LLM Tab**: Displays the raw conversational output and tool-calling JSON of the agent, filtering `records.filter(r => r.isLlm)`.

---

## Documentation of Recent Changes

The following critical fixes were recently implemented to resolve stability issues involving hallucinated skills, massive dependency downloads, and UI log clutter:

### 1. Stripped LLM Streams from the Error Overview UI
- **File Modifed**: `studio/src/features/graph/Inspector.tsx`
- **Issue**: When an agent exhausted its iteration budget and crashed, its entire `stderr` buffer was dumped into the frontend's failure box. Because `[LLM_STREAM]` chunks were emitted to stderr, the UI error box was filled with unreadable JSON token streams.
- **Resolution**: Implemented a string manipulation filter in `Inspector.tsx` to actively strip out any lines starting with `[LLM_STREAM]` when rendering `node.failure.stderr`.

### 2. Fixed Hallucinated/Ghost Skill Assignment
- **File Modified**: `cmd/forge/compiler/agents/architect-agent/workers/architect.py`
- **Issue**: The Architect Python script dynamically scanned the `skills/` folder to build a list of available skills to assign to nodes. However, it blindly indexed *any directory* (e.g. `skills/graphify/`), even if the actual `.yaml` definition file was missing or deleted. This caused the LLM to assign phantom skills, which instantly crashed the `graph_engine` (e.g., `Agent requested unknown skill`).
- **Resolution**: Updated the directory indexing logic to strictly only append skills if a corresponding `.yaml` file exists in the directory.

### 3. Prevented Over-assignment of Heavy Dependencies
- **File Modified**: `cmd/forge/compiler/agents/architect-agent/workers/architect.py`
- **Issue**: When the Architect designed simple UI agents (e.g. `hero-image-agent`, `dark-mode-agent`), it would randomly assign highly specialized skills (like `ml-engineering` or `devops-infrastructure`) simply because they were available in the list. This resulted in the environment wastefully downloading massive dependencies (PyTorch, Scikit-Learn, Kubernetes, Terraform) for simple web tasks.
- **Resolution**: Aggressively updated the Architect's system prompt with `CRITICAL INSTRUCTION` markers, explicitly banning the assignment of heavy ML/DevOps skills to simple frontend/backend agents, and instructing it to default to an empty list `[]` if no skill is strictly required.

### 4. Suppressed Hugging Face Hub Warning
- **File Modified**: `cmd/forge/compiler/lib/rag_tools.py`
- **Issue**: The `sentence-transformers` library threw a continuous warning to stderr: `Warning: You are sending unauthenticated requests to the HF Hub`, cluttering the logs.
- **Resolution**: Injected `os.environ["HF_HUB_DISABLE_SYMLINKS_WARNING"] = "1"` and standard Python `warnings.filterwarnings` to permanently suppress this specific message.
