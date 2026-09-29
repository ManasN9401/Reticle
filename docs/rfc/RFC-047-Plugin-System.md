---
status: draft
owner: Reticle Project
updated: 2026-09-29
---

# RFC-047: Plugin System

## Motivation

`schemas/plugin.schema.json` exists today but is explicitly titled "reserved, no runtime loader" and accepts only `{id, version, description?}` with `additionalProperties: false`. `docs/rfc/RFC-001-Terminology.md` already defines a Plugin as "a bundle of capabilities that extends the core Framework... group[ing] together custom Skills, Subagents, and Tools into a distributable package." An earlier roadmap used the placeholder label RFC-013 for this topic, but no RFC-013 file was filed and no loader exists. RFC-047 is the filed proposal; the old label is not an alias.

## Design

**Schema (structural rewrite, not additive).** Because `additionalProperties: false` on the current schema rejects any new field outright, extending it is a breaking change to the schema itself — acceptable here since the schema has never had a consumer. New fields: `agents: []`, `skills: []`, `tools: []`, `mcpServers: []` (referencing RFC-046's server-config shape for plugins that bundle their own MCP servers), `dependencies: [{plugin, versionRange}]`, `compatibility: {reticleVersion: versionRange}`.

**Bundle convention.** A plugin is a directory under `.reticle/plugins/<id>/` containing a manifest conforming to the schema above plus its payload (agent YAML, skill YAML, worker scripts).

**Loader.** `runtime/plugin/loader.go` validates a bundle against the schema and checks `compatibility.reticleVersion` against the running build before registering anything — an incompatible plugin is rejected with a reason surfaced to Studio, not silently skipped.

**Registry.** `runtime/plugin/registry.go` registers a bundle's agents and skills into the existing `runtime/agent/registry.go` `Registry`. Plugin tools are descriptors and adapters in RFC-050's runtime-owned broker registry; plugins may not create a second invocation path or bypass its attempt credentials, capability checks, limits, cancellation, effect certainty or telemetry. Bundled MCP declarations register through `runtime/mcp/registry.go` only after RFC-046 is implemented. This RFC therefore depends on RFC-050, and its `mcpServers` field additionally depends on RFC-046.

**Load order and atomicity.** The loader validates compatibility, dependencies, identities, schemas, tool-name collisions, requested capabilities and MCP references before publishing any registration. A bundle becomes visible atomically; a partial load is rolled back. Disabling a plugin first prevents new dispatches and broker calls, then drains or cancels active work according to runtime policy before unregistering its entries.

**Hot loading.** A plugin that only declares YAML agents/skills/capabilities can be enabled or disabled without restarting `forge.exe` — the loader unregisters it cleanly from the `Registry`. A plugin that bundles a new Python virtual environment or worker runtime cannot: it is surfaced with `restartRequired: true` on its state, and enabling it takes effect only after the next `forge.exe` launch. This is a documented limitation, not a defect to be silently worked around.

**Studio surface.** `GET/POST /api/plugins`, `DELETE /api/plugins/{id}`, `POST /api/plugins/{id}/enable`, `POST .../disable` on `runtime/telemetry/server.go`. `PluginsSection.tsx` shows compatibility, dependencies, contributed agents, skills, tools and MCP declarations, with a restart-required indicator per plugin and a "Restart Forge" shortcut reusing Studio's existing stop/start actions. Studio is a client of runtime state and cannot mark an invalid or partially loaded bundle active.

## Implementation order

1. Implement RFC-050's broker registry and attempt-scoped invocation path.
2. Implement RFC-046 before accepting plugin `mcpServers` declarations.
3. Extend and version the plugin schema, then add validation and atomic registration.
4. Add lifecycle APIs and Studio management after runtime state is authoritative.

## Drawbacks

No code sandboxing exists for plugin-bundled Python workers or agent definitions — a plugin's code runs with the same trust level as a built-in agent. This is a known, explicitly flagged gap rather than an oversight; closing it is a precondition for RFC-048's autonomous self-rewriting concept, which would otherwise let a system that writes its own plugins do so unsandboxed. Version-compatibility checking happens at install time only; it does not re-verify compatibility on every dispatch, so a Reticle upgrade that breaks an installed plugin's assumptions surfaces as a runtime failure rather than an install-time rejection.
