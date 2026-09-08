---
status: accepted
owner: Reticle Project
updated: 2026-09-08
---
# Event delivery and lifecycle

The local bus assigns sequence IDs at enqueue and dispatches domain handlers serially. Handlers may enqueue follow-up events; slow observers must use bounded I/O queues. Subscription handles support removal. Call Close outside a handler to reject new work and drain queued events.

The event queue holds at most 4,096 events; excess diagnostic WorkerLog messages are shed once 1,024 events are queued. Exceeding the core limit replaces queued work with RuntimeOverloaded, cancels active tasks and marks active executions/queue entries failed. The runtime rejects further domain work until restart. This explicit fatal-overload policy avoids blocking a reentrant publisher or silently losing a required event. RuntimeShutdown remains accepted for cleanup.

An execution uses running, paused, cancelled, completed or failed status. Nodes use pending/running/done/failed. ExecutionPaused/Resumed/Killed are control events; WorkerCompleted follows a validated committed result. Artifact availability alone does not mark a node complete. Studio must handle lifecycle events and distinguish backend sessions.

WorkflowSnapshot restores current node and execution states on reconnect. Event history is a bounded client diagnostic view; a snapshot is not proof of complete historical timing or log data. Durable distributed replay is outside this local v1 contract.
