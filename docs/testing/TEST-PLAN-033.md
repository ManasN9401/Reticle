# TEST-PLAN-033: Isolated Session Workspaces & File Locking

## 1. Overview
This document outlines the rigorous testing strategy for RFC-033. Because `forge.exe` will now handle concurrent agent execution across decoupled session namespaces, it is critical that we prevent race conditions, memory leakage between sessions, and file corruption during simultaneous edits.

## 2. Unit Testing (Go Event Bus)
**Component:** `runtime/bus_test.go` and `runtime/mutex_store_test.go`
- **Scenario A: High-Concurrency Mutex Queuing**
  - Spawn 100 goroutines simultaneously publishing a `FileLockRequested` event for the same mock file (`src/mock_target.go`).
  - *Assertion:* The MutexStore must strictly serialize these requests, emitting exactly 100 `FileLockGranted` events sequentially without dropping any locks.
- **Scenario B: Crash Recovery & Deadlock Prevention**
  - Simulate an agent crash (`exit_non_zero`) while it currently holds a lock on `src/mock_target.go`.
  - *Assertion:* The Go orchestrator must intercept the crash event, detect the orphaned lock, and immediately release it, allowing the next queued agent to acquire it.

## 3. Integration Testing (Session Sandboxing)
**Component:** `cmd/forge/executor_test.go`
- **Scenario:** Enqueue two Waitlist items simultaneously (e.g., `exec-001` and `exec-002`).
- **Assertion 1 (Disk Isolation):** Check the physical file system to ensure exactly two directories exist: `.reticle/sessions/exec-001/` and `.reticle/sessions/exec-002/`.
- **Assertion 2 (Memory Isolation):** Inspect the `memory.json` dumped in both directories. Verify that keys written by the `exec-001` agent are physically absent from the `exec-002` execution context payload.

## 4. End-to-End Stress Test (The "Messy Merge" Preventer)
**Component:** `tests/e2e/test_file_locking.py` running against `forge.exe`
- **Setup:** Create a dummy file `shared.txt` with the text: `Line 1`.
- **Mock Agent Integration (`mock_stress_tester.py`):**
  We will introduce a highly specialized mock agent (`compiler/agents/mock_stress_tester.py`) that bypasses LLM inference entirely. Its sole purpose is to rapidly hammer the orchestrator with `FileLockRequested` payloads.
- **Action:** Queue two `mock_stress_tester` agents concurrently. Both agents are programmed to append their unique Session ID to `shared.txt`.
- **Expected Flow:**
  1. Agent 1 acquires the lock, reads `Line 1`, appends `Agent 1`, writes via `stdout` artifact, and releases the lock.
  2. Agent 2 is forcefully paused/blocked by the Orchestrator Mutex until Agent 1 finishes.
  3. Agent 2 acquires the lock, reads the *fresh* state from disk (`Line 1 \n Agent 1`), appends `Agent 2`, writes, and releases.
- **Assertion:** `shared.txt` MUST read exactly:
```text
Line 1
Agent 1
Agent 2
```
If the pessimistic lock fails, Agent 2 will patch based on stale context, overriding Agent 1's work.
