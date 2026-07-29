# Project State

**Current Phase:** Foundation

**Last Updated:** 2026-07-29

**Status:** Active

---

# Project State

> **Purpose**
>
> This document provides a concise but comprehensive snapshot of the project's
> current state. It should always answer:
>
> - Where are we?
> - Why are we here?
> - What have we already decided?
> - What still needs to be decided?
> - What should happen next?

---

# Current Phase

**Phase:**

Foundation Architecture

**Status:**

Repository structure established.

Core documentation standards are currently being designed.

---

# Current Objective

The immediate objective is to establish the repository's constitutional
documents before any implementation begins.

The project currently prioritises architecture over code.

Implementation work should not begin until the foundational standards provide
sufficient guidance.

---

# Repository Status

Current repository structure has been established.

Major top-level areas include:

- Documentation
- Runtime
- Agents
- Plugins
- Skills
- Schemas
- Tests
- Tools
- Examples
- Assets

Documentation has been separated from executable artefacts.

---

# Architectural Principles Agreed

The following principles have broad agreement and should be considered stable
unless deliberately revisited.

## Documentation

Documentation defines architecture.

Documentation should always precede implementation.

---

## Standards

Standards define enduring rules.

Standards should avoid implementation details.

---

## RFCs

RFCs propose architectural changes.

Accepted RFCs become part of the project's architectural history.

---

## Templates

Templates are reference implementations of repository standards.

Templates demonstrate compliance rather than define requirements.

---

## Metadata

Every repository document shall include YAML front matter.

Metadata exists to make documents self-describing.

Metadata should remain concise, descriptive and human-first.

---

## Repository Philosophy

Repository organisation should reflect architectural organisation.

Top-level directories represent long-lived architectural concerns.

Temporary implementation details should not appear as top-level directories.

---

# Major Decisions Reached

The following decisions have already been made.

- Documentation resides under `docs/`.
- Schemas remain outside `docs/`.
- Standards and templates are separate concepts.
- Diagrams are documentation assets.
- Tests are first-class project artefacts.
- Tools contain project utilities rather than only scripts.
- Documentation should prioritise human readability before machine processing.

---

# Open Questions

The following topics remain unresolved.

## Metadata

- Final review of `000 DOCUMENT_METADATA_STANDARD.md`.
- Whether `owner` should remain a required metadata field.
- Future controlled vocabularies.

---

## Governance

- Document lifecycle process.
- RFC numbering policy.
- Review procedures.
- Acceptance procedures.

---

## Runtime

High-level runtime architecture remains intentionally undefined.

Implementation decisions should not precede architectural standards.

---

# Immediate Next Steps

Priority order:

1. Finalise `000 DOCUMENT_METADATA_STANDARD.md`.
2. Write `001 DOCUMENT_WRITING_STANDARD.md`.
3. Produce repository templates.
4. Begin planning RFC-000.
5. Write RFC-000 after planning reaches sufficient maturity.

No implementation work is currently planned.

---

# Future Work

Following completion of the foundation standards, attention will shift toward:

- Runtime architecture
- Agent lifecycle
- Shared memory
- Event system
- Scheduler
- Supervision graph
- Plugin architecture
- Validation
- Runtime implementation

---

# Notes for Future Contributors

Before making architectural decisions:

- Read the repository standards.
- Prefer extending existing principles over introducing new ones.
- Avoid implementation-driven architecture.
- Challenge assumptions.
- Record significant architectural decisions.
- Keep documents focused on a single responsibility.

When uncertain, prefer simplicity.

---

# Context

This repository intentionally invests significant effort into architectural
planning before implementation.

The expectation is that careful architectural design will reduce long-term
complexity and improve maintainability.

Architectural discussion should prioritise reasoning over speed.