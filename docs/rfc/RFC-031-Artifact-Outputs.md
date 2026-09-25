---
status: historical
owner: Reticle Project
updated: 2026-09-23
---

> Historical design record, relocated from the repository root where it had been sitting unlinked. Current versioned specifications under `docs/specifications/` take precedence where they conflict.

# RFC-031: Artifact Outputs Standard

## 1. Abstract
When the Reticle Forge orchestrates a complex workflow DAG (e.g. generating a snake game via multiple agents), the end result is a collection of generated code files, documentation, and logic chunks called `Artifacts`. This RFC establishes a standard for extracting these final in-memory artifacts and physically rendering them to the disk.

## 2. Motivation
Users often run Forge to produce a final, runnable application. The `MemoryManager` securely holds these artifacts in a transient graph, but to execute the code, the user needs tangible `.py`, `.js`, `.go`, or `.txt` files in their workspace.

## 3. Specification

### 3.1 Directory Structure
When a workflow run (Execution) successfully reaches a `WorkflowCompleted` state, the Waitlist Manager MUST automatically scrape the `ArtifactStore` for all items bound to that Execution ID.

The artifacts MUST be dumped in the active workspace under the following structure:
```
{workspace_dir}/
  outputs/
    {exec_id}/
      README.md
      {artifact_id_1}.ext
      {artifact_id_2}.ext
```

### 3.2 README Autogeneration
An auto-generated `README.md` MUST be placed alongside the dumped artifacts. This README acts as an index, mapping each artifact file to its generating Producer Agent and its logical Type.

Example:
```markdown
# Execution exec-001 Outputs
- `code-1_output.py` (Producer: coder-agent, Type: artifact_type_code)
- `scaff-1_output.txt` (Producer: scaffolder-agent, Type: artifact_type_text)
```

### 3.3 Execution
Execution triggers via an event-driven hook on `WorkflowCompleted`. Artifacts with empty payload data (`art.Data == nil`) will be skipped.

This guarantees artifacts are safely logged to disk without pausing or interrupting the CLI REPL environment.
