---
status: accepted
owner: Reticle Project
updated: 2026-09-08
---
# Agent definitions

Use schemas/agent.schema.json. Both .yaml and .yml are accepted. Inputs, outputs, skills and memory are arrays of strings. Entrypoints resolve relative to the manifest and must exist. Python workers use the provisioned interpreter; binary/go entries name a built executable, not a source file.

`id`, `name`, `version`, `runtime` and `entrypoint` are required at registry load, matching the schema. `capabilities` is an array of values defined by the schema. It controls which shared SDK tools are advertised. Unknown values fail registry loading. Existing manifests without the field receive the intentional v1 compatibility grant; new or security-sensitive manifests must declare the smallest explicit set. `process.native` still requires the user's native-execution setting, and `cloud.apply` or `security.active` does not create an adapter or authorization by itself.

`mcp_servers` is an optional array of registered server IDs. A non-empty value requires `mcp.call`; neither field is sufficient alone. At dispatch the runtime derives immutable `mcp.server:<id>` broker policies from this trusted manifest, and only enabled, successfully discovered tools matching both grants enter that attempt's catalogue. Workers cannot add grants in task input.

Unknown skill IDs are logged and skipped for v1 startup compatibility. This is a degraded worker configuration, not proof that the requested instructions loaded; generated execution definitions should use only skills present in the registry.

Generated definitions are scoped to one execution; public node agent IDs resolve to that execution's binding before built-ins. Coder, scaffolder and generator aliases are compiler services, not general task specialists.

## Architect discovery contract

Forge generates `architect_catalog` from the fully loaded runtime registry after project, compiler and enabled plugin manifests have been parsed. The catalogue contains every dispatchable maintained agent with its description, runtime, inputs, outputs, skills, capabilities, MCP allowlist and non-secret broker policy labels; every registered skill under its manifest `id` with description and dependency metadata; and the supported static and broker tool mappings. It is refreshed when MCP discovery or plugin lifecycle changes. Architect, coder, scaffolder, writer and stress-test services are omitted because they are compiler implementation details rather than task specialists.

The architect must use this catalogue as its sole discovery source. It must not infer availability from filenames or scan `skills/` independently. A generated agent declares a non-empty, least-privilege `capabilities` list which the scaffolder preserves in its execution-scoped manifest. A reused maintained agent emits an empty list in the generated DAG because its checked-in manifest remains authoritative. RAG indexing/query tools are a maintained `rag-agent` specialization; granting `rag.local` to a generic generated coding worker does not change that worker into the RAG tool harness.
