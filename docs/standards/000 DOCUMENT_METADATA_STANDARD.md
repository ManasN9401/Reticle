---
title: Document Metadata Standard
document_type: Standard
authority: Normative
status: Draft
version: 0.1.0
scope: Repository
stability: Experimental
owner: Reticle Project
created: YYYY-MM-DD
updated: YYYY-MM-DD

purpose: >
  Defines the standard metadata that shall appear at the beginning of
  repository documents.

audience:
  - Contributors
  - Architects
---

# 000 — Document Metadata Standard

## 1. Purpose

This standard defines the metadata that shall appear at the beginning of
documents within the Reticle repository.

The purpose of metadata is to allow both humans and software to immediately
understand the role, authority and lifecycle of a document before reading its
contents.

This standard intentionally defines only the structure and meaning of document
metadata. It does not define document writing conventions, document templates,
or repository governance.

## Document Applicability

For the purposes of this standard, a document is any repository file intended
to communicate information to human readers.

This includes, but is not limited to:

- Standards
- RFCs
- Planning documents
- ADRs
- Specifications
- Templates
- Guides
- Journal entries
- Repository documentation

Unless explicitly exempted, all documents shall include metadata conforming to
this standard.

---

# 2. Design Principles

Document metadata exists to support the following principles.

## 2.1 Self-Describing Repository

Every document shall introduce itself before presenting its contents.

Readers should never be required to infer the purpose or authority of a document
from its filename or location alone.

---

## 2.2 Human First

Metadata shall be written primarily for human readers.

Where possible, the chosen format should also be straightforward for software to
parse.

---

## 2.3 Simplicity

Only metadata that provides lasting value shall be included.

Fields shall not be added solely for anticipated future requirements.

---

## 2.4 Consistency

Equivalent information shall always be represented in the same manner throughout
the repository.

---

## 2.5 Structural Validation

Metadata should support structural validation.

Validation should confirm that metadata is correctly formed.

Validation should not attempt to determine whether the document itself is
architecturally correct.

---

# 3. Metadata Format

All document metadata shall be expressed using YAML front matter.

Metadata shall appear before all document content.

Example:

```yaml
---
title: Example Document
document_type: RFC
authority: Normative
status: Draft
version: 0.1.0
scope: Runtime
stability: Experimental
owner: Reticle Project
created: 2026-07-29
updated: 2026-07-29

purpose: >
  Briefly describes why this document exists.

audience:
  - Contributors
---
```

---

# 4. Required Metadata Fields

Every document shall define the following fields.

| Field | Description |
|-------|-------------|
| title | Human-readable document title. |
| document_type | Classification of the document. |
| authority | Whether the document defines architecture or provides supporting information. |
| status | Current lifecycle state of the document. |
| version | Document version. |
| scope | Primary architectural or repository area covered by the document. |
| stability | Expected maturity of the document. |
| owner | Primary owner or maintaining group. |
| created | Original creation date. |
| updated | Most recent modification date. |
| purpose | Concise explanation of why the document exists. |
| audience | Intended readership. |

---

# 5. Metadata Definitions

## 5.1 Title

A concise human-readable document name.

Titles should prioritise clarity over brevity.

---

## 5.2 Document Type

Identifies the role of the document.

Initial document types include:

- Standard
- RFC
- Planning
- ADR
- Specification
- Journal
- Template
- Guide

Additional document types may be introduced through future standards.

---

## 5.3 Authority

Defines whether the document establishes project rules.

Valid values:

- Normative
- Informative

Normative documents define architecture, requirements or standards.

Informative documents explain, discuss or support the project but do not define
the architecture.

---

## 5.4 Status

Represents the document's current lifecycle.

Initial values include:

- Draft
- Review
- Accepted
- Deprecated
- Historical

Additional lifecycle states may be introduced by future governance documents.

---

## 5.5 Version

Documents shall use Semantic Versioning.

The version represents the document itself rather than any implementation.

---

## 5.6 Scope

Defines the primary area to which the document applies.

Examples include:

- Repository
- Governance
- Runtime
- Scheduler
- Memory
- Plugins
- Agents
- API
- Documentation

Scope is descriptive rather than restrictive.

---

## 5.7 Stability

Describes the maturity of the document's contents.

Suggested values include:

- Experimental
- Stable
- Deprecated
- Historical

Stability is independent of document status.

An accepted document may still be experimental.

---

## 5.8 Owner

Identifies the individual or group responsible for maintaining the document.

Ownership exists to provide accountability rather than authority.

---

## 5.9 Created

The date on which the document was originally created.

This value shall not change.

---

## 5.10 Updated

The date of the most recent modification.

This value should be updated whenever the document is materially changed.

---

## 5.11 Purpose

A brief explanation describing why the document exists.

Purpose should describe intent rather than implementation.

---

## 5.12 Audience

Identifies the intended readers.

Examples include:

- Contributors
- Architects
- Core Developers
- Plugin Developers
- End Users

Documents may specify multiple audiences.

---

# 6. Metadata Evolution

The metadata standard is expected to evolve alongside the project.

New metadata fields should only be introduced when they provide clear,
long-term value.

Removing existing metadata fields should be considered a breaking change.

---

# 7. Future Work

Future standards may define:

- metadata validation
- document schemas
- repository tooling
- document relationships
- automated repository analysis

These topics are intentionally outside the scope of this document.

---

# 8. References

None.