---
status: accepted
owner: Reticle Project
updated: 2026-09-29
---

# RFC-047: Plugin System

## Motivation

Before this RFC, `schemas/plugin.schema.json` was explicitly titled "reserved, no runtime loader" and accepted only `{id, version, description?}` with `additionalProperties: false`. `docs/rfc/RFC-001-Terminology.md` already defined a Plugin as "a bundle of capabilities that extends the core Framework... group[ing] together custom Skills, Subagents, and Tools into a distributable package." An earlier roadmap used the placeholder label RFC-013 for this topic, but no RFC-013 file was filed. RFC-047 is the filed and implemented contract; the old label is not an alias.

## Design

**Schema (structural rewrite, not additive).** Because `additionalProperties: false` on the current schema rejects any new field outright, extending it is a breaking change to the schema itself — acceptable here since the schema has never had a consumer. New fields: `agents: []`, `skills: []`, `tools: []`, `mcpServers: []` (referencing RFC-046's server-config shape for plugins that bundle their own MCP servers), `dependencies: [{plugin, versionRange}]`, `compatibility: {reticleVersion: versionRange}`.

**Bundle convention.** A plugin is a directory under `.reticle/plugins/<id>/` containing a manifest conforming to the schema above plus its payload (agent YAML, skill YAML, worker scripts).

**Loader.** `runtime/plugin/loader.go` validates a bundle against the schema and checks `compatibility.reticleVersion` against the running build before registering anything — an incompatible plugin is rejected with a reason surfaced to Studio, not silently skipped.

**Registry.** `runtime/plugin/manager.go` registers a bundle's agents and skills into the existing `runtime/agent/registry.go` `Registry`. Plugin tools are descriptors and adapters in RFC-050's runtime-owned broker registry; plugins may not create a second invocation path or bypass its attempt credentials, capability checks, limits, cancellation, effect certainty or telemetry. Bundled MCP declarations register through `runtime/mcp/registry.go`. This RFC therefore depends on RFC-050 and RFC-046.

**Load order and atomicity.** The loader validates compatibility, dependencies, identities, schemas, tool-name collisions, requested capabilities and MCP references before publishing any registration. A bundle becomes visible atomically; a partial load is rolled back. Disabling a plugin first prevents new dispatches and broker calls, then drains or cancels active work according to runtime policy before unregistering its entries.

**Hot loading.** V1 contributions are declarative YAML agents/skills, one-call subprocess tools and MCP server declarations. They can be enabled or disabled without restarting `forge.exe`; Python agent dependencies still use Reticle's existing lazy environment provisioner. Replacing the Forge runtime, loading code into its process, or contributing a new worker runtime is outside the v1 manifest and therefore rejected rather than represented by a misleading partial enable. `restartRequired` is retained in the API for a future version but is false for every accepted v1 bundle.

**Studio surface.** `GET/POST /api/plugins`, `DELETE /api/plugins/{id}`, `POST /api/plugins/{id}/enable`, `POST .../disable` on `runtime/telemetry/server.go`. `PluginsSection.tsx` shows compatibility, dependencies, contributed agents, skills, tools and MCP declarations, including the reserved restart-required state. Studio is a client of runtime state and cannot mark an invalid or partially loaded bundle active.

## Implementation order

1. Implement RFC-050's broker registry and attempt-scoped invocation path.
2. Implement RFC-046 before accepting plugin `mcpServers` declarations.
3. Extend and version the plugin schema, then add validation and atomic registration.
4. Add lifecycle APIs and Studio management after runtime state is authoritative.

## Drawbacks

No code sandboxing exists for plugin-bundled Python workers, direct tools or MCP processes — a plugin's code runs with the Reticle user's OS authority. This is a known, explicitly flagged gap rather than an oversight; closing it is a precondition for RFC-048's autonomous self-rewriting concept, which would otherwise let a system that writes its own plugins do so unsandboxed. Compatibility is revalidated whenever an installed bundle is listed or enabled, so a Reticle upgrade surfaces an incompatible bundle as invalid before its contributions are registered.

## Implementation status

Accepted and implemented on 29 September 2026. A plugin is copied into `.reticle/plugins/<id>/` only after bounded, symlink-free bundle validation. Enable validates compatibility, dependencies, identities and all contributions before registering agents, skills, broker tools, workers, subscriptions and MCP declarations; failures roll those registrations back. Disable first prevents new dispatch and broker discovery, then unregisters the bundle while already-started attempts retain their immutable attempt state.

Direct plugin tools use a deliberately small subprocess protocol: one JSON request on stdin and one JSON response on stdout. They still execute only through RFC-050, with timeout/output limits, cancellation, effect certainty and a `plugin:<id>` policy grant. Existing agents do not gain plugin tools implicitly; plugin-contributed agents receive their own plugin grant. A future manifest revision may define explicit grants for project agents after that authority model is reviewed.
