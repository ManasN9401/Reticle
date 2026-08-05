# RFC-008 — Agent Architecture

Status: Stable
Version: 1.0.0
Author: HyperParallel Core
Last Updated: 2026-08-03

---

## 1. Purpose
This document defines the architecture of HyperParallel Agents. It covers the declarative Agent Definition Schema (YAML) and the JSON-based Worker Protocol (v1) that dictates how agents communicate with the Orchestrator runtime.

## 2. Motivation
To achieve massive parallelism and language-agnostic execution, agents must be completely isolated from the orchestration logic. Defining agents via a declarative YAML schema and communicating over a strict `stdin`/`stdout` JSON protocol ensures that agents are highly portable, ephemeral, and safe to execute in sandboxed environments (processes, containers, or web assembly).

## 3. Scope
This RFC covers:
- The YAML Schema for defining agents.
- Agent Identity, Inputs, Outputs, and Subscriptions.
- Worker Protocol (v1) schema for `stdin` (Task Invocation).
- Worker Protocol (v1) schema for `stdout` (Task Completion).
- Error handling via `stderr` and Exit Codes.

## 4. Philosophy
Agents are modeled as pure, deterministic functions. They do not retain persistent dynamic memory across invocations and they do not reach out to the orchestrator for state. All required memory and instructions must be resolved by the Workflow Engine before dispatch.

## 5. Principles
- **Language Agnosticism**: Agents can be written in any language (Python, Go, Node.js, bash) as long as they adhere to the JSON `stdin`/`stdout` contract.
- **Declarative Subscriptions**: Agents declare their automation triggers natively in their YAML schema, offloading the event-matching logic to the orchestrator.
- **Strict Boundaries**: The runtime orchestrates; the agent executes. Agents do not orchestrate.

## 6. Architectural Laws
1. The Dispatcher injects exactly one JSON object into standard input when a worker is launched.
2. Upon successful execution, the worker must emit exactly one JSON object to standard output and exit with code `0`.
3. If a worker fails to execute, it must exit with a non-zero exit code and dump context to standard error.

## 7. Rationale
By enforcing strict request/response protocols over standard streams, we decouple the agent's logic from the framework's internal event bus. This makes it trivial to test agents locally (e.g., `echo '{...}' | python agent.py`) without needing to boot the full HyperParallel runtime.

## 8. Trade-offs
- Parsing JSON over standard streams introduces slight serialization overhead.
- Workers cannot dynamically query memory mid-execution in v1. They only receive what was pre-fetched. (This will be addressed in a future streaming protocol, v2).

## 9. Future Considerations
- Introducing Worker Protocol v2 (Streaming), which would allow agents to open bidirectional communication with the orchestrator over a socket to dynamically query memory or stream long-running outputs (like LLM tokens).
- Supporting containerized runtimes (Docker, WASM) instead of just local OS processes.

## 10. References
- RFC-026 — Worker Fault Tolerance & Stdout Protocol
- RFC-027 — Worker Runtime Contract (v1)

## 11. Related RFCs
- RFC-003 — Runtime
- RFC-009 — Skills
