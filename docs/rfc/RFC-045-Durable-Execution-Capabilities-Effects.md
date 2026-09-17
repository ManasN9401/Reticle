---
status: accepted
owner: Reticle Project
updated: 2026-09-17
---
# Durable execution, capabilities and external effects

## Decision

Reticle persists local workflow executions in `.reticle/executions/state.json`. The snapshot contains the workflow graph, graph revision, node states, attempt records, active attempt identities and terminal run state. A write uses an owner-only temporary file, flush and atomic replacement. The local runtime is the single writer.

Each concrete dispatch receives a random attempt ID. Result commits use that ID as an idempotency key, and memory persists committed IDs with its snapshot. A duplicate result is acknowledged without applying its mutations twice. Work found running after restart becomes `interrupted`; Reticle never guesses that it succeeded. An operator or adapter must reconcile it to retry, fail or cancel.

Agent manifests declare typed capabilities. The registry rejects unknown capabilities, and the worker SDK advertises only tools allowed by the resolved grant. Native execution requires both the `process.native` grant and the user's native-execution setting. A model can request work through a granted tool but cannot add a grant.

External adapters record effects in `.reticle/effects/state.json` before execution. Records bind an operation and attempt to an adapter, target and request hash, with external/idempotency IDs and cleanup ownership where available. Prepared or running records recover as `interrupted` and must be reconciled from observed external state before retry.

ML workers use an explicit `cpu`, `amd-rocm`, `nvidia-cuda` or `user` profile. Accelerator profiles require explicit numeric device selection. The runtime leases those devices to one attempt at a time. Profile selection configures admission and container wiring; it does not prove driver/framework compatibility or scientific quality.

## Current limits

The execution and effect stores are durable local snapshots rather than a transactional database and outbox. Memory/result idempotency and execution state are separate atomic files, so a machine failure between them can still require reconciliation. Multi-process writers and remote schedulers are unsupported.

The generic DevOps terminal remains read-only/validation-only. The effect ledger is infrastructure for future provider adapters; it does not authorize AWS, Terraform, Kubernetes or other mutation. A provider adapter must define its target identity, plan hash, approval binding, idempotency behavior and reconciliation logic before receiving `cloud.apply`.

GPU leases coordinate attempts in one Reticle process. They do not reserve devices across unrelated processes and do not replace an AMD ROCm or NVIDIA CUDA compatibility smoke test.

## Verification

Runtime tests cover restart interruption, effect reconciliation, duplicate result commits across restart, GPU lease contention, unknown capability rejection, unpinned skill rejection and graph mutation admission by active attempt, registered worker and node budget.
