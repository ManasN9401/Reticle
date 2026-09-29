---
status: accepted
owner: Reticle Project
updated: 2026-09-29
---

# MCP servers and plugins

Reticle's extension boundary preserves the runtime's main goal: a model may
plan work, but the Go control plane owns process lifecycle, permissions,
cancellation, limits and observable effects. MCP servers and plugins therefore
extend the existing registries and RFC-050 broker; they do not give workers a
second execution channel.

## Local MCP servers

Open **Studio → Settings → Extensions → MCP Servers**, choose **Add server**,
and provide a local command plus one argument per line. Only `stdio` transport
is accepted in v1. A working directory must remain inside the Reticle checkout.
Environment mappings use `CHILD_NAME=HOST_NAME`: Reticle resolves the host
variable only when spawning the server and never stores or returns its value.

Saving an enabled server starts it and discovers its tools. **Test & discover**
repeats that check and shows lifecycle errors, missing variable names and the
discovered catalogue. Configuration is persisted in
`.reticle/mcp/servers.json`; Studio and the authenticated control API read the
same runtime state.

An agent must declare both:

```yaml
capabilities: [mcp.call]
mcp_servers: [my-server]
```

The architect sees only enabled, successfully discovered servers. Generated
agents can receive those two explicit grants; they cannot invent a server or
widen a task's grants at runtime.

## Local plugins

A plugin is a directory containing `plugin.json` plus its declared payload.
Use **Studio → Settings → Extensions → Plugins → Install folder**. Installation
copies and validates the bundle but leaves it disabled. Enabling atomically
registers its agents, skills, direct broker tools and optional MCP servers;
disabling removes them from future dispatch and discovery.

The normative fields and direct-tool stdin/stdout contract are defined in
[Plugin manifest v1](specifications/plugin-manifest-v1.md) and
`schemas/plugin.schema.json`. A direct tool is available only to an agent with
its declared capability and the plugin's internal policy grant. Existing core
or project agents never acquire plugin tools merely because a bundle is
enabled.

## Trust boundary

Both features execute local processes with the Reticle user's OS authority.
V1 validates paths, bounds protocols and output, redacts diagnostics, and gates
calls per attempt; it is not an OS sandbox and it does not verify signatures.
Install only trusted bundles and server commands. Remote MCP transports, OAuth,
a marketplace and autonomous plugin generation are intentionally unsupported.

See [RFC-046](rfc/RFC-046-MCP-Integration.md),
[RFC-047](rfc/RFC-047-Plugin-System.md), and
[RFC-050](rfc/RFC-050-Attempt-Scoped-Tool-Broker.md) for the design rationale.
