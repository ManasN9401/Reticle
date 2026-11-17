---
status: accepted
owner: Reticle Project
updated: 2026-11-16
---
# Worker protocol v1

One UTF-8 JSON task is sent to stdin, followed by EOF. Workers send progress to stderr and one final JSON response to stdout, then exit zero. Output is bounded; multiple final responses and mismatched task IDs fail. The final artifact is optional; when present it requires id, name and type. Task fields are defined in schemas/task.schema.json.

SDK workers include structured `verification` evidence in the final response. Each entry names the tool, exact target and successful outcome. Availability, inventory and shell probes such as `terraform version`, `bandit --version`, `docker images`, `ls` and `echo` are not evidence that produced work is valid. File-producing workers must re-read every file changed since the last recognized test, build, lint or validation command, or run one of those commands successfully after the changes. RAG workers query the built index. Accepted evidence is emitted as `WorkerVerificationRecorded` for inspection.

A failed worker prints a retry-safety marker on stderr: `[RETICLE_RETRY_SAFE: NO_EFFECTS]` when it changed nothing, `[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]` when it changed only workspace files through replay-safe tools, and nothing once it ran a terminal command, a RAG index change or a brokered tool. The dispatcher uses it as described in the runtime orchestration specification.

Web output has its own check. Any role may call `check_web_page`, a read-only static check of a page and the scripts it loads: a classic script that uses `import`/`export`, a bare module import with no import map, a referenced file that does not exist, a renderer canvas that is not a `<canvas>`, and (when `node` is installed) a JavaScript syntax error. Roles whose purpose is to build (currently `frontend`) cannot complete after writing web files until a check returns no blocking problems, and a later edit requires checking again. A builder role with no declared outputs must also have changed something before it can complete.

The final response may also carry an optional `files` array: the workspace files the worker created or changed, as paths relative to the session's `src` directory, in the canonical form the file tools resolve. The session's `src` directory is the worker's workspace, so a leading `src/` names that directory itself: `src/main.js` and `main.js` are the same file, and a bare `src` is the directory. Before this rule `write_file("src/main.js")` landed at `src/src/main.js` beside files written without the prefix, which broke the pages built on them and left re-read verification unmatched. The runtime keeps at most 500 unique, well-formed relative paths and silently drops the rest (absolute paths, `..`, empty or `.` segments, backslashes, drive colons, NUL, paths over 512 characters); a malformed list never fails a task. A non-empty list is emitted as `WorkerFilesWritten`. It covers the final attempt only; files written by an earlier failed attempt remain on disk but are not listed.

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

Model requests may use a second loopback side channel, the inference proxy of
RFC-052. When the runtime proxies an attempt it injects `RETICLE_INFERENCE_URL`
(an OpenAI-compatible base URL ending in `/v1`) and `RETICLE_INFERENCE_TOKEN`,
and removes the route credential from the task, so the child environment holds
no provider secret. A worker that sees both variables sends every chat request
to `POST {RETICLE_INFERENCE_URL}/chat/completions` with
`Authorization: Bearer {RETICLE_INFERENCE_TOKEN}`, in the OpenAI chat-completions
format, streamed or not. The proxy replaces the request's `model` with the
current route's upstream name, so the worker's value only labels the request; the
worker cannot choose the endpoint, the model or the credential. The token is bound
to the attempt, is redacted from worker output, is never serialized into the task
or model context, and is revoked when the attempt ends; a request with an unknown
or revoked token is refused with 401.

A proxied request either streams the provider's response unchanged or returns the
provider's error status and body once the dispatcher has no other route for it.
A provider that cannot be reached is reported as `502` with the error code
`provider_unreachable`. A worker must execute tool calls only from a complete,
validated response, so the proxy's repeating a pending request on another route
never repeats a tool. Workers that call their provider themselves keep the
direct path: the runtime proxies only workers whose entrypoint runs
`worker_sdk.run`, and only on routes with an OpenAI-compatible endpoint.

Contract tests: runtime/agent/worker_test.go,
runtime/toolbroker/broker_test.go, runtime/inference/proxy_test.go,
runtime/agent/inference_routing_test.go, runtime/memory/runtime_state_test.go,
tests/test_workers.py, tests/test_inference_proxy_worker.py and
tests/test_memory_quality.py. A schema contract test
checks that every serialized Go task field is declared in
`schemas/task.schema.json`.
