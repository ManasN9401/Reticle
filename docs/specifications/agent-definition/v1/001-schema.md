---
Status: Stable
Author: HyperParallel Core
Date: 2026-07-31
---

# Agent Definition Schema (v1)

HyperParallel agents are defined via YAML files loaded into the Agent Registry.

## File Extension
Files should end in `.yaml`.

## Schema

```yaml
id: string          # Required: A globally unique identifier for this agent.
name: string        # Required: Human-readable name.
version: string     # Required: Semantic version of this agent (e.g., 1.0.0).

runtime: string     # Required: The runtime environment (e.g., python).

entrypoint: string  # Required: The path to the executable script relative to the workspace.

inputs:             # Optional: A list of expected artifact data types.
  - string          # (e.g. document/markdown)

outputs:            # Optional: A list of produced artifact data types.
  - string

subscriptions:      # Optional: A list of automation triggers.
  - id: string      # Optional: Name of this subscription
    event: string   # Required: The runtime event to listen for.
    filters:        # Optional: Key-value string matches against the event payload.
      key: string

```

## Example
```yaml
id: wording-imp
name: Wording Improver
version: 1.0.0

runtime: python
entrypoint: workers/wording_agent.py

inputs:
  - document/markdown
outputs:
  - document/markdown

subscriptions:
  - id: audit-markdown
    event: ArtifactStored
    filters:
      producer: outline-gen
      type: document/markdown
```
