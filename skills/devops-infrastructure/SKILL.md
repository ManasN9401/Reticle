---
name: devops-infrastructure
description: Generate infrastructure definitions and verify them in an explicitly configured tool environment.
---
# DevOps and infrastructure

Discover the tools needed by the specific operation inside the actual execution environment. Check versions and target identity. A host binary check does not establish container availability. Python Terraform wrappers still require Terraform; Docker SDKs require a daemon. Report missing capabilities instead of claiming an SDK can always replace them.

Keep generation, validation, planning, approval, application and post-change verification distinct. The current shared specialist permits generation and bounded validation/read-only commands. Protected application and active infrastructure changes require a separately authorized execution adapter; do not claim these occurred through the validation worker.

Before any enabled protected action, show a concrete plan, target account/project/region/cluster, configuration revision and affected resources. Approval must bind to that exact plan. Prompt text and model output cannot grant approval. Reconcile uncertain external outcomes before retrying; do not blindly repeat applies.

Use scoped credentials and pinned tool/provider environments. Do not choose a cloud provider, state backend or deployment topology without project evidence. Verify resulting state before reporting deployment success. Include command results, remaining uncertainty and deliverable locations.
