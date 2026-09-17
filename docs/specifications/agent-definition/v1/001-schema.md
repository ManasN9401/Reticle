---
status: accepted
owner: Reticle Project
updated: 2026-09-08
---
# Agent definitions

Use schemas/agent.schema.json. Both .yaml and .yml are accepted. Inputs, outputs, skills and memory are arrays of strings. Entrypoints resolve relative to the manifest and must exist. Python workers use the provisioned interpreter; binary/go entries name a built executable, not a source file.

`capabilities` is an array of values defined by the schema. It controls which shared SDK tools are advertised. Unknown values fail registry loading. Existing manifests without the field receive the v1 compatibility grant; new or security-sensitive manifests should declare the smallest explicit set. `process.native` still requires the user's native-execution setting, and `cloud.apply` or `security.active` does not create an adapter or authorization by itself.

Generated definitions are scoped to one execution; public node agent IDs resolve to that execution's binding before built-ins. Coder, scaffolder and generator aliases are compiler services, not general task specialists.
