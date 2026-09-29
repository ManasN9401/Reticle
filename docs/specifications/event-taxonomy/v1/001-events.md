---
status: accepted
owner: Reticle Project
updated: 2026-09-29
---
# Event delivery and lifecycle

The local bus assigns sequence IDs at enqueue and dispatches domain handlers serially. Handlers may enqueue follow-up events; slow observers must use bounded I/O queues. Subscription handles support removal. A panicking subscriber is quarantined and reported as `SubscriberPanicked`; surviving handlers and later events continue. Call Close outside a handler to reject new work and drain queued events.

The event queue holds at most 4,096 events; excess diagnostic WorkerLog messages are shed once 1,024 events are queued. Exceeding the core limit replaces queued work with RuntimeOverloaded, cancels active tasks and marks active executions/queue entries failed. The runtime rejects further domain work until restart. This explicit fatal-overload policy avoids blocking a reentrant publisher or silently losing a required event. RuntimeShutdown remains accepted for cleanup.

An execution uses running, paused, interrupted, cancelled, completed or failed status. Nodes use pending, running, done, failed, blocked or interrupted. AttemptStarted/AttemptFinished identify each concrete retry. ExecutionPaused/Resumed/Killed/Reconciled are control events; WorkerCompleted follows a validated idempotent result commit. Artifact availability alone does not mark a node complete. Studio must handle lifecycle events and distinguish backend sessions.

`RuntimePersistenceFailed` is fatal to active admission. Its payload identifies the failed phase and affected execution IDs. The dispatcher cancels owned work, the graph projection becomes interrupted, matching running waitlist items fail, pending items remain queued for a restart, and no more work is admitted by that process. Restart/reconciliation is required. ExternalEffectBeginRequested and ExternalEffectFinishRequested persist adapter-owned side effects; interrupted effects cannot return to running without a new operation identity.

`EnvironmentProvisioningStarted`, `EnvironmentProvisioningCompleted` and `EnvironmentProvisioningFailed` carry an operation ID plus execution, task and attempt identity when provisioning was requested by a task. `WorkerVerificationRecorded` carries the structured checks accepted from a worker result.

`BrokerToolCallStarted` and `BrokerToolCallFinished` identify the execution,
task, attempt, call, canonical tool and adapter. Finished events add duration,
outcome and effect certainty. Broker credentials, arguments and results are
excluded. A result arriving after attempt revocation is reported as discarded
and cannot be committed to the attempt.

`McpServerConnected`, `McpServerDisconnected`, `McpServerError` and
`McpToolsChanged` describe bounded server lifecycle state. `McpToolInvoked`
contains server, canonical tool, broker call identity, duration and outcome,
but never arguments, results or environment values. `PluginInstalled`,
`PluginEnabled`, `PluginDisabled`, `PluginDeleted` and `PluginError` describe
bundle lifecycle without embedding plugin file contents.

WorkflowSnapshot restores current node and execution states, attempt history and graph revision on reconnect. Event history is a bounded client diagnostic view; the durable execution snapshot is authoritative for current state but is not a complete event outbox. Durable distributed replay is outside this local v1 contract.
