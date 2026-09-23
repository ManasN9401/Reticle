---
status: draft
owner: Reticle Project
updated: 2026-09-23
---

# RFC-047: Plugin System

## Motivation

`schemas/plugin.schema.json` exists today but is explicitly titled "reserved, no runtime loader" and accepts only `{id, version, description?}` with `additionalProperties: false`. `docs/rfc/RFC-001-Terminology.md` already defines a Plugin as "a bundle of capabilities that extends the core Framework... group[ing] together custom Skills, Subagents, and Tools into a distributable package," and `docs/ROADMAP.md`'s RFC-013 entry lists the intended scope — lifecycle, discovery, registration, hot loading/unloading, dependencies, version compatibility — but no such file was ever filed and no loader exists. This RFC is that filing.

## Design

**Schema (structural rewrite, not additive).** Because `additionalProperties: false` on the current schema rejects any new field outright, extending it is a breaking change to the schema itself — acceptable here since the schema has never had a consumer. New fields: `agents: []`, `skills: []`, `mcpServers: []` (referencing RFC-046's server-config shape for plugins that bundle their own MCP servers), `dependencies: [{plugin, versionRange}]`, `compatibility: {reticleVersion: versionRange}`.

**Bundle convention.** A plugin is a directory under `.reticle/plugins/<id>/` containing a manifest conforming to the schema above plus its payload (agent YAML, skill YAML, worker scripts).

**Loader.** `runtime/plugin/loader.go` validates a bundle against the schema and checks `compatibility.reticleVersion` against the running build before registering anything — an incompatible plugin is rejected with a reason surfaced to Studio, not silently skipped.

**Registry.** `runtime/plugin/registry.go` registers a bundle's agents and skills into the existing `runtime/agent/registry.go` `Registry`, and any bundled MCP server declarations into `runtime/mcp/registry.go` (RFC-046) — this RFC depends on RFC-046 landing first for that path.

**Hot loading.** A plugin that only declares YAML agents/skills/capabilities can be enabled or disabled without restarting `forge.exe` — the loader unregisters it cleanly from the `Registry`. A plugin that bundles a new Python virtual environment or worker runtime cannot: it is surfaced with `restartRequired: true` on its state, and enabling it takes effect only after the next `forge.exe` launch. This is a documented limitation, not a defect to be silently worked around.

**Studio surface.** `GET/POST /api/plugins`, `DELETE /api/plugins/{id}`, `POST /api/plugins/{id}/enable`, `POST .../disable` on `runtime/telemetry/server.go`. `PluginsSection.tsx` mirrors the MCP Servers CRUD pattern, with a restart-required indicator per plugin and a "Restart Forge" shortcut reusing Studio's existing stop/start actions.

## Drawbacks

No code sandboxing exists for plugin-bundled Python workers or agent definitions — a plugin's code runs with the same trust level as a built-in agent. This is a known, explicitly flagged gap rather than an oversight; closing it is a precondition for RFC-048's autonomous self-rewriting concept, which would otherwise let a system that writes its own plugins do so unsandboxed. Version-compatibility checking happens at install time only; it does not re-verify compatibility on every dispatch, so a Reticle upgrade that breaks an installed plugin's assumptions surfaces as a runtime failure rather than an install-time rejection.
