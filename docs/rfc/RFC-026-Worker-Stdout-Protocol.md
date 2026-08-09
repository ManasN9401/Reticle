# RFC-026 — Worker Fault Tolerance & Stdout Protocol

Status: Draft
Version: 1.0.0
Author: HyperParallel Core
Last Updated: 2026-08-04

---

## 1. Purpose
This document formalizes the fault-tolerance guarantees and specific parsing behaviors required for the `Worker Protocol (v1)`. It outlines how the runtime must handle noisy agent standard outputs and how agents must implement fallback mitigations.

## 2. Motivation
During stress testing (e.g., a 22-node execution graph), reliance on external third-party APIs (like LLM providers) frequently leads to timeouts, rate limits, and authentication errors (e.g., missing API keys). Furthermore, third-party libraries (like `litellm`) often pollute `stdout` with unsuppressable warnings, breaking naive JSON parsers.

## 3. Scope
This RFC covers:
- The Robust Stdout Parsing requirement for the Go Orchestrator.
- The Mock Fallback paradigm for Worker Agents.
- The distinction between terminal failures and mitigatable failures.

## 4. Philosophy
The orchestrator must be incredibly defensively programmed against rogue or noisy workers, assuming that standard streams will be polluted. Workers must strive to provide continuity of the DAG execution even if their primary intelligence source fails.

## 5. Principles
- **Noisy by Default**: The runtime must expect non-JSON strings on `stdout` and discard them gracefully until the final payload is found.
- **Graceful Degradation**: If an LLM call fails, the worker should fall back to deterministic mock data rather than crashing the entire graph, unless strictly configured otherwise.

## 6. Architectural Laws
1. The Orchestrator's execution loop (`worker.go`) must scan `stdout` line-by-line indefinitely until the process exits.
2. The Orchestrator uses a custom `bufio.Scanner` buffer configured to support up to **10MB** output payloads (extended from the standard 64KB limit). Workers must ensure their final JSON payload does not exceed this hard limit.
3. The Orchestrator must attempt to `json.Unmarshal` every parsed line. The last valid JSON payload successfully parsed before the process exits with code `0` is considered the definitive artifact. All other output is treated as auxiliary logging.
4. Worker scripts (e.g., `worker_llm.py`) must wrap critical API calls in broad `try/except` blocks.
5. If a critical API call fails (e.g., `Invalid API Key`), the worker must dump the stack trace to `stderr` and emit a Mock JSON payload to `stdout` to allow downstream nodes to continue testing topology.
6. Worker scripts MUST redirect native standard output (`sys.stdout = sys.stderr`) globally before loading third-party libraries (e.g. LiteLLM), capturing the original `stdout` file descriptor exclusively for the final JSON artifact dump.

## 7. Rationale
A single string warning from a Python library (e.g., "Warning: API endpoint deprecated") or logging strings (e.g. `Provider List: https://...`) previously caused the entire Go orchestrator to panic when it tried to parse `stdout` as JSON. By implementing a continuous scanner that seeks the valid payload, and strictly enforcing the `sys.stdout = sys.stderr` redirection inside the worker processes, we achieve total immunity to library noise. By enforcing mock fallbacks, we enable developers to test massive, highly-concurrent graph topologies locally without incurring API costs or hitting rate limits. Furthermore, the 10MB buffer size resolves `bufio.ErrTooLong` freezes encountered during massive agent DAG compilations.

## 8. Trade-offs
- Searching every line of `stdout` for a JSON payload incurs a slight CPU penalty compared to reading the stream directly into a single unmarshal buffer.
- Mock fallbacks can mask real architectural bugs if a user is unaware that the output was mocked. The telemetry UI must make it obvious when an agent has fallen back to mock data.

## 9. Future Considerations
- Establishing an explicit handshake protocol (v2) where the worker explicitly demarcates the boundaries of the JSON payload (e.g., `---BEGIN ARTIFACT---`), rather than relying on trial-and-error JSON parsing.

## 10. References
- Proven in `03_startup_launch` stress test via the implementation of `bufio.Scanner` loops in `worker.go`.

## 11. Related RFCs
- RFC-008 — Agent Architecture
