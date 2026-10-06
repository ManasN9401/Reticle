---
status: accepted
owner: Reticle Project
updated: 2026-10-06
---
# Worker protocol v1

One UTF-8 JSON task is sent to stdin, followed by EOF. Workers send progress to stderr and one final JSON response to stdout, then exit zero. Output is bounded; multiple final responses and mismatched task IDs fail. The final artifact is optional; when present it requires id, name and type. Task fields are defined in schemas/task.schema.json.

SDK workers include structured `verification` evidence in the final response. Each entry names the tool, exact target and successful outcome. Availability, inventory and shell probes such as `terraform version`, `bandit --version`, `docker images`, `ls` and `echo` are not evidence that produced work is valid. File-producing workers must re-read every file changed since the last recognized test, build, lint or validation command, or run one of those commands successfully after the changes. RAG workers query the built index. Accepted evidence is emitted as `WorkerVerificationRecorded` for inspection.

The final response may also carry an optional `files` array: the workspace files the worker created or changed, as paths relative to the session's `src` directory, exactly as the worker wrote them (a leading `src/` is kept, because it names a real subdirectory). The runtime keeps at most 500 unique, well-formed relative paths and silently drops the rest (absolute paths, `..`, empty or `.` segments, backslashes, drive colons, NUL, paths over 512 characters); a malformed list never fails a task. A non-empty list is emitted as `WorkerFilesWritten`. It covers the final attempt only; files written by an earlier failed attempt remain on disk but are not listed.

The runtime validates results before publishing their memory changes, graph changes and artifacts in order, followed by WorkerVerificationRecorded and WorkerFilesWritten when present, and then WorkerCompleted. Graph completion follows that event. Worker memory writes are execution-scoped; higher-scope changes require a trusted runtime path. Legacy stdin lock request/grant messages are unsupported and must not be used.

The built-in HitL node runs in the trusted Go runtime and stores a separate hash-bound decision. Its Markdown preview is not an authorization parser. Older RFC-026/027 prose is historical where it conflicts with this document.

Runtime-owned tools use a separate loopback side channel and do not alter stdin,
stdout or stderr framing. For one active attempt, the runtime may inject
`RETICLE_TOOL_BROKER_URL`, `RETICLE_TOOL_BROKER_TOKEN` and
`RETICLE_ATTEMPT_ID` into the child environment. The token is bound to that
attempt, is never serialized into the task or model context, and is revoked when
the attempt ends. Workers send it only in the broker Authorization header and
send the attempt ID in `X-Reticle-Attempt-ID`.

The SDK discovers admitted descriptors with `GET /v1/tools` and invokes them
with `POST /v1/calls`. Broker calls cannot expand the task's capability grant.
Cancellation, bounded output and effect certainty belong to the broker; the
worker still emits exactly one final TaskResponse on stdout.

Contract tests: runtime/agent/worker_test.go,
runtime/toolbroker/broker_test.go, runtime/memory/runtime_state_test.go,
tests/test_workers.py and tests/test_memory_quality.py. A schema contract test
checks that every serialized Go task field is declared in
`schemas/task.schema.json`.
