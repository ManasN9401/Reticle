# Request for Comments (RFC) Process

## What is an RFC?
The RFC (Request for Comments) process is the formal mechanism for proposing significant changes to the Reticle Framework. While internal runtime implementations (like the Go engine or dispatcher algorithms) can evolve fluidly, the public-facing boundaries of the system are **Frozen Contracts**.

## Frozen Contracts
The following domains are explicitly considered Frozen Interfaces. Any breaking changes to them **must** undergo the RFC process:

1. **Worker Protocol** (`docs/specifications/worker-protocol/`)
2. **Agent Definition Schema** (`docs/specifications/agent-definition/`)
3. **Workflow Definition Schema** (`docs/specifications/workflow-definition/`)
4. **Event Taxonomy** (`docs/specifications/event-taxonomy/`)

## Versioning Policy
Specifications are versioned strictly by directory (e.g., `v1/`, `v2/`). 
- When a contract is established, it resides in the current version directory (e.g., `v1/001-protocol.md`).
- A new major version of a contract requires an RFC and moves authoritative documentation to the next version folder (e.g., `v2/`).
- Historical versions are retained as read-only historical references and annotated with `Status: Superseded` or `Status: Archived`.

## Document Metadata
All specifications and RFCs must include metadata frontmatter at the top of the file:
```yaml
Status: Stable | Draft | Review | Accepted | Deprecated | Superseded | Archived
Author: [Name]
Date: YYYY-MM-DD
```

## How to Submit an RFC
1. Create a markdown file in `docs/rfc/` named `XXXX-brief-title.md`.
2. Set `Status: Draft`.
3. Provide a clear motivation, detailed design, and a section for drawbacks.
4. Solicit feedback from maintainers.
5. Upon approval, change status to `Accepted`, and implement the changes into the official `docs/specifications/` folders.
