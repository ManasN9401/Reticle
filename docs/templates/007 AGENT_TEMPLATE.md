# Agent Template (Python / Go / Binary)

> [!IMPORTANT]
> **MANDATORY READING:** Before creating or modifying any Agent, you MUST read and fully understand:
> - **[Agent Definition v1](../specifications/agent-definition/v1/001-schema.md)**
> - **[Worker Protocol v1](../specifications/worker-protocol/v1/001-protocol.md)**

## 1. Directory Structure
Each agent must be self-contained in a directory matching its ID, e.g. `agents/my-worker/`:
- `my-worker.yaml` (The declarative Agent Definition Schema)
- `workers/` (The actual executable worker script/binary)
- `requirements.txt` (If Python, for isolated dependencies)

## 2. YAML Schema (`my-worker.yaml`)
```yaml
id: my-worker
name: "My Worker Name"
description: "Brief description"
version: 1.0.0
runtime: python # or go, binary
entrypoint: workers/my_worker.py
inputs:
  - "expected_artifact_dependency"
memory:
  - "expected_shared_memory_key"
skills:
  - "some-global-skill"
```

## 3. Worker Protocol Implementation
The executable defined in `entrypoint` MUST adhere to
**[Worker Protocol v1](../specifications/worker-protocol/v1/001-protocol.md)**.

- **Input:** It will receive a single `Task` JSON object on `stdin`.
- **Output:** It must print a single `TaskResponse` JSON object to `stdout` upon completion.
- **Failures:** It must report a structured failure or exit non-zero. It must not
  fabricate a successful artifact or deterministic mock result for failed work.
