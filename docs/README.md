# Reticle documentation

## Authority and lifecycle

When documents disagree, use this order:

1. versioned contracts under `docs/specifications/` and the schemas they name;
2. current operational guides linked from this index;
3. accepted ADRs and current RFCs;
4. historical RFCs, planning notes, diagrams and dated audits.

RFCs marked `historical` preserve design context and do not override an accepted specification or current code. `docs/planning/` is non-authoritative work history. Dated audits preserve evidence from their recorded baseline; use a later repair ledger to determine whether a finding remains open. The full document-metadata standard remains Draft; the enforced minimum for RFCs and specifications is `status`, `owner`, and `updated` frontmatter.

- [Logic and documentation review, 18 September 2026](audit/2026-09-18/01-logic-and-documentation-review.md): current code findings, verification evidence and documentation-quality assessment.
- [Repair status for the 18 September review](audit/2026-09-18/02-repair-status.md): implemented fixes, retained compatibility behavior, and validation results.
- [Audit findings, 8 September 2026](audit/2026-09-08/01-findings.md): historical findings against the recorded commit.
- [Architecture recommendations](audit/2026-09-08/02-recommendations.md).
- [Repair status](audit/2026-09-08/03-repair-status.md): implementation and verification follow-up.
- [Shared-memory effectiveness review](audit/2026-09-08/04-shared-memory-review.md): additional fixes, benchmarks and remaining limits.
- [Credential exposure follow-up](audit/2026-09-08/05-credential-exposure.md): removed tracked literals and required provider rotation.
- [ML environment selection](ml-environments.md): current capabilities and recommended hardware profiles.
- [Custom agent guide](custom_agent_guide.md).
- [Core agents](core_agents.md).
- [Environment configuration](environment.md).
- [Studio workspace and activity views](studio-workspaces.md).
- [Memory lifecycle RFC](rfc/RFC-044-Memory-Lifecycle-Recovery-Evaluation.md).
- [Durable execution, capabilities and effects RFC](rfc/RFC-045-Durable-Execution-Capabilities-Effects.md).
- [Historical material boundary](archive/README.md).
- [RFC process](governance/RFC_PROCESS.md).

Audit reports belong under `docs/audit/<date>/`. Preserve their baseline evidence; record fixes separately rather than rewriting historical observations as if they never occurred.
