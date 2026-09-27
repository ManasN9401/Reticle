---
status: accepted
owner: Reticle Project
updated: 2026-09-08
---
# Agent definitions

Use schemas/agent.schema.json. Both .yaml and .yml are accepted. Inputs, outputs, skills and memory are arrays of strings. Entrypoints resolve relative to the manifest and must exist. Python workers use the provisioned interpreter; binary/go entries name a built executable, not a source file.

`id`, `name`, `version`, `runtime` and `entrypoint` are required at registry load, matching the schema. `capabilities` is an array of values defined by the schema. It controls which shared SDK tools are advertised. Unknown values fail registry loading. Existing manifests without the field receive the intentional v1 compatibility grant; new or security-sensitive manifests must declare the smallest explicit set. `process.native` still requires the user's native-execution setting, and `cloud.apply` or `security.active` does not create an adapter or authorization by itself.

Unknown skill IDs are logged and skipped for v1 startup compatibility. This is a degraded worker configuration, not proof that the requested instructions loaded; generated execution definitions should use only skills present in the registry.

Generated definitions are scoped to one execution; public node agent IDs resolve to that execution's binding before built-ins. Coder, scaffolder and generator aliases are compiler services, not general task specialists.

## Architect discovery contract

Forge generates `architect_catalog` from the fully loaded runtime registry after project and compiler manifests have been parsed. The catalogue contains every dispatchable maintained agent with its description, runtime, inputs, outputs, skills and capabilities; every registered skill under its manifest `id` with description and dependency metadata; and the supported capability-to-worker-tool mapping. Architect, coder, scaffolder, writer and stress-test services are omitted because they are compiler implementation details rather than task specialists.

The architect must use this catalogue as its sole discovery source. It must not infer availability from filenames or scan `skills/` independently. A generated agent declares a non-empty, least-privilege `capabilities` list which the scaffolder preserves in its execution-scoped manifest. A reused maintained agent emits an empty list in the generated DAG because its checked-in manifest remains authoritative. RAG indexing/query tools are a maintained `rag-agent` specialization; granting `rag.local` to a generic generated coding worker does not change that worker into the RAG tool harness.
