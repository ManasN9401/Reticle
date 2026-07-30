# Skeleton Runtime Plan

## Purpose

This document defines the initial implementation milestone for HyperParallel.

Rather than attempting to build the complete platform immediately, the project
will first develop a minimal "walking skeleton" that exercises the core
architectural ideas.

The objective is not performance, scalability or completeness.

The objective is validation.

If this skeleton proves successful, the remaining architecture can evolve around
a working foundation rather than theoretical assumptions.

---

# Vision

The Skeleton Runtime should be capable of solving a real task using multiple
cooperating agents sharing a common execution context.

Every future capability—including plugins, distributed execution, advanced
memory systems and graphical supervision—should be evolutions of this runtime
rather than separate systems.

---

# Success Criteria

The Skeleton Runtime is considered successful when it can demonstrate the
following workflow:

Human
    ↓
Supervisor
    ↓
Task Graph
    ↓
Worker Agents (executing concurrently)
    ↓
Shared Memory
    ↓
Supervisor Review
    ↓
Final Result

The implementation does not need to be fast.

It does not need persistence.

It does not require networking.

It only needs to demonstrate the architecture.

---

# Guiding Principles

## Build Vertically

Each milestone should produce a complete, executable workflow.

Avoid building isolated subsystems that cannot yet be exercised.

---

## Validate Before Expanding

New features should only be introduced after the previous layer has been
demonstrated to work reliably.

---

## Simplicity First

Every component should initially implement the smallest possible version of
itself.

Complexity should be earned.

---

## Human Supervision

Humans remain the highest authority.

The runtime should always allow human inspection, intervention and approval.

---

# Initial Runtime Components

The Skeleton Runtime consists of only five architectural components.

## 1. Orchestrator

Responsibilities

- Start runtime
- Load configuration
- Create execution session
- Spawn supervisor
- Coordinate shutdown

The orchestrator contains almost no business logic.

It exists to connect components together.

---

## 2. Supervisor

Responsibilities

- Accept work from the user
- Decompose work into tasks
- Assign tasks to workers
- Monitor execution
- Review completed work
- Produce final output

Initially only a single supervisor exists.

Future versions may support supervision graphs.

---

## 3. Worker Runtime

Responsibilities

- Execute assigned tasks
- Read shared memory
- Produce artifacts
- Publish events
- Report completion

Workers are intentionally interchangeable.

No worker owns global state.

---

## 4. Shared Memory

The shared memory acts as the runtime's single source of truth.

Initially this may be implemented using simple in-memory data structures.

Persistence is not required.

Shared memory should eventually contain concepts such as:

- execution state
- task state
- artifacts
- observations
- locks
- references
- runtime metadata

Workers should communicate primarily through shared memory rather than direct
peer-to-peer messaging.

---

## 5. Event Bus

Every significant runtime action should produce an event.

Examples include:

- TaskCreated
- TaskStarted
- TaskCompleted
- MemoryUpdated
- WorkerIdle
- WorkerFailed
- SupervisorReviewRequested

Initially these events may simply be delivered synchronously.

The event system establishes the communication model for future runtime
expansion.

---

# First Demonstration

The first successful demonstration should intentionally solve a very small
problem.

Example workflow

User:

"Create a project README."

Supervisor:

- Analyse request
- Produce task graph

Worker A

- Generate outline

Worker B

- Improve wording

Supervisor

- Merge results

Result

README produced.

The objective is proving collaboration rather than document quality.

---

# Shared Memory Goals

The Skeleton Runtime should directly address one of the project's original
motivations:

Multiple AI agents frequently overwrite each other's work because they operate
using independent snapshots of the workspace.

The Skeleton Runtime should instead demonstrate:

- shared state
- shared artifacts
- shared task progress
- shared observations

This validates the project's central hypothesis.

---

# Explicitly Deferred

The following features are intentionally excluded.

## Persistence

The runtime may lose all state when stopped.

---

## Networking

Execution occurs entirely on a single machine.

---

## Distributed Execution

All workers execute locally.

---

## Plugins

No plugin system is required.

---

## Permissions

Security model is postponed.

---

## Authentication

Not required.

---

## GUI

Console output is sufficient.

---

## Vector Databases

Not required.

---

## Embeddings

Not required.

---

## Long-Term Memory

Not required.

---

## Distributed Event Streaming

Simple synchronous events are sufficient.

---

## Performance Optimisation

Correctness is significantly more important than speed.

---

# Repository Impact

The Skeleton Runtime will primarily affect:

runtime/

agents/

tests/

Examples should be added alongside implementation whenever practical.

---

# Milestone Order

Milestone 1

Basic runtime startup.

---

Milestone 2

Supervisor capable of accepting work.

---

Milestone 3

Worker execution.

---

Milestone 4

Shared memory.

---

Milestone 5

Event bus.

---

Milestone 6

End-to-end demonstration.

---

# Completion Criteria

This planning document is complete when HyperParallel can demonstrate:

✓ Multiple concurrent workers

✓ Shared runtime memory

✓ Supervisor coordination

✓ Event-driven execution

✓ Human supervision

✓ Successful completion of a real task

without requiring any advanced runtime features.

Only after these objectives are achieved should development expand toward
plugins, distributed execution, persistent memory and graphical supervision.

---

# Looking Beyond the Skeleton

The Skeleton Runtime is not intended to become production software.

Its purpose is to establish confidence in the project's architectural direction.

Once the Skeleton Runtime is complete, future RFCs should be based on lessons
learned from implementation rather than assumptions made during planning.

The long-term vision remains unchanged:

Build a reusable, highly parallel, graph-supervised multi-agent runtime capable
of coordinating large numbers of specialised agents while maintaining shared
context, deterministic execution and meaningful human oversight.

The Skeleton Runtime is simply the first step toward that vision.