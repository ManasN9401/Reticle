---
status: accepted
owner: Reticle Project
updated: 2026-09-29
---

# Plugin manifest v1

The normative structural schema is `schemas/plugin.schema.json`. An installed
bundle lives at `.reticle/plugins/<id>/plugin.json`; all listed agent, skill and
executable payload paths are relative to that directory and may not traverse
outside it or use symlinks.

Required fields are `id`, semantic `version`, and
`compatibility.reticleVersion`. Optional contributions are `agents`, `skills`,
`tools`, `mcpServers` and `dependencies`. Installation copies a bounded bundle
after validation but leaves it disabled. Enable revalidates compatibility and
dependencies, rejects registry/name collisions, and publishes all
contributions as one managed operation with rollback on failure.

A direct tool declares its broker identity, model-safe name, description,
object JSON schema, required capability, timeout, output limit, effect
certainty, command and arguments. At invocation Reticle starts the command in
the plugin directory, writes one line of JSON to stdin—`id`, `tool` and
`arguments`—and accepts one JSON object from stdout with `result` or `error`.
Diagnostics belong on stderr. The broker adds the independent
`plugin:<plugin-id>` policy, so a capability alone does not expose the tool.

`mcpServers` uses RFC-046's server configuration. The plugin controls lifecycle;
the server is enabled only with the plugin and cannot be edited independently
in Studio. Environment entries remain mappings from child variable names to
host variable names, never secret values.

Plugins are trusted local code. Validation and broker policy limit accidental
authority, but v1 does not provide signatures, provenance verification or an OS
sandbox. A marketplace and autonomous plugin generation are therefore outside
this specification.
