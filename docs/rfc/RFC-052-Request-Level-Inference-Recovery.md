---
status: draft
owner: Reticle Project
updated: 2026-10-01
---

# RFC-052: Request-Level Inference Recovery

## Motivation and status

Reticle currently routes a model when the dispatcher starts a worker. A recognized
provider failure can restart that worker only with explicit no-effects evidence.
This is unnecessarily expensive when only an inference call failed, and cannot
safely recover a provider outage after the worker has already executed tools.

This draft proposes recovery of the pending inference request while the same
worker retains its conversation. It does not authorize replaying a tool call,
restarting an unknown failed process, changing graph failure policy, generating
repair nodes, or expanding capabilities. It is not implemented by the routing
repair described below.

## Existing routing repair

The dispatcher now maintains task-local attempted and excluded route identities.
Request rejections cannot reappear through the router's confidence fallback.
Untried eligible routes are preferred. After two catalog/request incompatibilities
from a provider, an alternative provider is preferred when eligible; a single
transport failure also supplies that preference. Neither overrides availability,
modality or capability/cost/effort ranking within the selected pool. Temporary
transport failures cool the credential slot without labelling its key invalid.

An explicit output-token upper bound may reduce a configured request once per
route, using another ordinary worker attempt and requiring no-effects proof.
Limits are task-local and reset to the original request budget on other routes.
Context overflow is not treated as an output bound. Unknown failures stay terminal.
The existing total attempts (default 3, maximum 15) and task deadline remain.
Terminal diagnostics state the stop reason and remaining/untried route counts.

## Proposed authority and transport

The dispatcher remains the sole recovery-policy owner. Introduce a runtime-owned
inference service using an authenticated, attempt-scoped loopback side channel.
It may reuse the broker's listener and identity validation infrastructure, but
inference is a distinct internal operation, not a model-callable tool.

The worker submits an inference request with an attempt ID, request ID, bounded
conversation, admitted tool schemas and generation options. The service resolves
the allowed route and credential internally, performs inference, and streams a
bounded response. API secrets and arbitrary endpoint/header selection are never
returned to the worker or accepted from model content. A worker cannot broaden
the runtime's endpoint, model or capability policy.

Worker Protocol v1 keeps one stdin task plus EOF, stderr progress and one final
stdout response. The new service requires a separately versioned side-channel
contract and explicit acceptance before implementation. Existing workers retain
dispatch-time routing; mixed clients must not both perform provider retries.

## Error facts and decisions

Adapters supply bounded facts: origin, stable error code, HTTP status where
applicable, provider/model identity, reported limit, retry-after duration and
request progress. Preserve a redacted diagnostic for unknown responses. Structured
HTTP/SDK facts take precedence over provider-specific interpretation and legacy
text classification. An adapter's retry suggestion is not authorization to replay.

The dispatcher combines those facts with request progress, route history, the
task budget and broker effect records. Scope penalties narrowly: malformed
requests affect requests, unsupported capabilities affect model eligibility,
invalid credentials affect credentials, and documented account-wide quotas affect
that account. A timeout alone does not establish an invalid key or exhausted quota.

Deterministic adjustments require explicit evidence. Output limits can be clamped;
unsupported optional parameters need an adapter-defined equivalent or omission
rule. Never drop task instructions, tools, required schemas or reasoning settings
silently to force acceptance. Input-context reduction is a separate operation
governed by the existing context-selection contract, not an automatic generic fix.

## Recovery and effect boundaries

The worker executes tools only after the inference response is complete and
validated. Each retry of a pending request keeps completed conversation turns and
tool results, discards incomplete text/tool-call fragments from that request and
replaces them atomically when a complete response is accepted. Stream fragments
may be displayed as provisional telemetry but are never committed as tool actions.

Previously completed tools are not re-executed. A request ID binds the attempt and
canonical request hash: duplicates join the active request or return its bounded
committed result; changed content under the same ID conflicts. Failover revisions
remain children of that request. Late provider output is discarded once another
revision commits or the attempt is revoked. Cancellation closes the stream,
releases route capacity and prevents a late result commit; pause blocks new calls.

Only inference adapters that cannot themselves execute external tools qualify for
transparent replay. Provider-hosted tools or background jobs with external effects
require their own effect records, idempotency and reconciliation. A timeout can
still incur provider charges twice even when no workspace effect occurred.

Worker crashes, interrupted effects and runtime persistence failures retain
RFC-045 semantics. A conversation in memory is not a durable crash checkpoint.
This proposal does not claim restart recovery for in-flight inference.

## Budget and selection

Keep one task recovery budget and deadline under dispatcher ownership. The initial
worker launch uses one attempt credit. Each automatic request adjustment, provider
failover or whole-worker restart consumes another credit from that same budget;
ordinary successful inference turns do not. Disable nested SDK/provider retries.
Backoff and retry-after waits remain within the original task deadline. Exhaustion
must say which budget stopped recovery and which eligible routes remained untried.

Preserve effort, capability, price and availability ranking. Diversity is a
response to failure evidence, not a requirement to visit every provider. A working
high-capability route can perform every suitable node without gratuitous switching.
Successful inference and successful verified task completion are distinct signals;
do not treat a provider HTTP success as evidence that generated code is correct.

## Provider compatibility

An OpenAI-compatible profile is an endpoint contract, not a promise that every
model accepts every parameter. Native OpenAI models require verified support for
the selected API, tools and generation options; models requiring Responses need
that adapter. Native Anthropic Messages likewise requires a reviewed adapter or
a compatible gateway. Merely adding either key to the environment creates neither
an adapter nor a verified capability score. Provider access does not override
Reticle's verification or effect contracts.

## Observability and acceptance

On acceptance, specify request/revision events and update Event Taxonomy and
runtime/worker side-channel documentation together. Diagnostics correlate task,
attempt, request, route, recovery category and stop reason, without credentials,
conversation bodies or raw tool arguments. Studio distinguishes request recovery
from worker restart and shows routes attempted and why others were excluded.

Tests must exercise: a successful strong route with no rotation; a mixed catalog
escaping to another provider; an explicit token-limit correction; transport
backoff; invalid credentials; single-provider recovery; unknown errors; aggregate
budget exhaustion; duplicate/mismatched request IDs; cancellation and pause;
late/partial streams; a failure after a file write with no repeated tool; a broker
call with uncertain effects that cannot be replayed; and an old worker using the
legacy dispatch path without multiplying retries. Use deterministic fixtures before
opt-in paid-provider smoke tests, reporting those results separately.

## Drawbacks and alternatives

Runtime-owned inference adds provider streaming and conversation-data handling to
the trusted runtime. It needs strict body limits, redaction, backpressure and
adapter maintenance. A failed request can still consume time and money. It does
not correct bad generated code, broken dependencies, or unsafe external effects.

Issuing replacement provider secrets to a live worker would be simpler but expands
credential exposure and makes central request accounting harder. Restarting the
whole worker remains the compatible fallback when no effects occurred, but loses
conversation progress. Keeping the current repair alone is a valid smaller option.

## References

- [Runtime orchestration](../specifications/runtime-orchestration.md)
- [Worker Protocol v1](../specifications/worker-protocol/v1/001-protocol.md)
- [RFC-045](RFC-045-Durable-Execution-Capabilities-Effects.md)
- [RFC-050](RFC-050-Attempt-Scoped-Tool-Broker.md)
- [RFC-051](RFC-051-Configurable-Model-Providers.md)
