---
Status: Stable
Author: HyperParallel Core
Date: 2026-07-31
---

# Worker Protocol (v1)

The Worker Protocol defines the strict JSON-based contract for data exchange between the HyperParallel Dispatcher and individual Worker agents. Agents operate as isolated child processes and communicate exclusively via `stdin`, `stdout`, and `stderr`.

## 1. Task Invocation (`stdin`)

When a worker is launched, the Dispatcher injects exactly one JSON object into standard input.

**Schema:**
```json
{
  "id": "string",
  "agent_id": "string",
  "workflow": "string",
  "execution": "string",
  "inputs": [
    {
      "artifact_id": "string",
      "version": 1,
      "name": "string",
      "data": "any"
    }
  ]
}
```

## 2. Successful Task Completion (`stdout`)

Upon successful execution, the worker must emit exactly one JSON object to standard output and exit with code `0`. The framework will automatically hydrate missing metadata (like `producer`, `execution`, `parents`, and timestamps).

### 2.1 Artifact Production
If the worker produces an artifact, the response must look like this:
```json
{
  "artifact": {
    "id": "string",
    "data": "any"
  }
}
```

### 2.2 Memory Scalar Update
If the worker just produces a scalar result instead of a stored artifact, it uses `result`:
```json
{
  "result": "string"
}
```

## 3. Worker Failure (`stderr` and Exit Codes)

If a worker fails to execute, it must exit with a non-zero exit code.
The Dispatcher will continuously stream standard error (`stderr`) to capture the failure context.

- **`exit_non_zero`**: Process exited normally but with a code `!= 0`.
- **`protocol_error`**: Process exited `0`, but the stdout JSON could not be parsed or was missing.
- **`timeout`**: Process exceeded the max runtime (future implementation).

The resulting emitted failure event struct will appear as:
```json
{
  "task_id": "string",
  "worker_id": "string",
  "reason": "enum(exit_non_zero, protocol_error, timeout)",
  "exit_code": 1,
  "stderr": "string"
}
```
