---
status: accepted
owner: Reticle Project
updated: 2026-09-08
---
# Worker protocol v1

One UTF-8 JSON task is sent to stdin, followed by EOF. Workers send progress to stderr and one final JSON response to stdout, then exit zero. Output is bounded; multiple final responses and mismatched task IDs fail. The final artifact is optional; when present it requires id, name and type. Task fields are defined in schemas/task.schema.json.

SDK workers include structured `verification` evidence in the final response. Each entry names the tool, exact target and successful outcome. Availability, inventory and shell probes such as `terraform version`, `bandit --version`, `docker images`, `ls` and `echo` are not evidence that produced work is valid. File-producing workers must re-read every file changed since the last recognized test, build, lint or validation command, or run one of those commands successfully after the changes. RAG workers query the built index. Accepted evidence is emitted as `WorkerVerificationRecorded` for inspection.

The runtime validates results before publishing their memory changes, graph changes and artifacts in order, followed by WorkerCompleted. Graph completion follows that event. Worker memory writes are execution-scoped; higher-scope changes require a trusted runtime path. Legacy stdin lock request/grant messages are unsupported and must not be used.

The built-in HitL node runs in the trusted Go runtime and stores a separate hash-bound decision. Its Markdown preview is not an authorization parser. Older RFC-026/027 prose is historical where it conflicts with this document.

Contract tests: runtime/agent/worker_test.go, runtime/memory/runtime_state_test.go, tests/test_workers.py and tests/test_memory_quality.py. A schema contract test checks that every serialized Go task field is declared in `schemas/task.schema.json`.
