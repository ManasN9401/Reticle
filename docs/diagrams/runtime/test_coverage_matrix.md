# Test Coverage Matrix

This matrix tracks the required unit and integration tests across the bifurcated Go and Python components.

| Component | Language | Test File | Coverage Status | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **Event Bus** | Go | `runtime/bus_test.go` | Pending | Must mock concurrent pub/sub events |
| **Worker Executor** | Go | `cmd/forge/executor_test.go`| Pending | Test `stdin` injection and `stdout` JSON parsing |
| **Memory Store** | Go | `runtime/memory_test.go` | Pending | Test atomic reads/writes across namespaces |
| **LLM Fallback** | Python | `tests/test_architect.py` | Pending | Mock `litellm.completion` throwing `RateLimitError` 3 times before succeeding |
| **Scaffolder** | Python | `tests/test_scaffolder.py` | Pending | Verify `workflow.yaml` generation matches schema |
| **Telemetry UI** | TS/JS | `runtime/telemetry/ui/test.ts`| Pending | E2E test for WebSocket rendering logic |
