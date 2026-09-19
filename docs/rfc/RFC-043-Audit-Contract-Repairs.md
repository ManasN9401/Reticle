---
status: accepted
owner: Reticle Project
updated: 2026-09-08
---

# RFC-043: Repair existing runtime contracts

The user authorized the audit findings' implementation on 8 September 2026. This RFC records that approval for repairs to the frozen protocol, manifest and event boundaries.

Preserve existing v1 JSON fields while enforcing identity and supporting the documented optional artifact. Stdin is one JSON request followed by EOF. Progress is stderr; stdout contains the final response. Unsupported legacy interactive stdin lock messages fail explicitly rather than receiving fictitious grants.

Accept both YAML extensions, validate manifests before execution, and preserve workflow parameters/modality. Add terminal lifecycle statuses/events and session-aware client handling. Bind control to authenticated loopback access; unauthenticated remote access is intentionally unsupported.

Drawbacks: scripts depending on permissive paths, invalid output, unvalidated manifests or unauthenticated control require migration. These behaviors were defects, not security guarantees to retain. Historical reports and protocol documents remain available with explicit supersession notes.
