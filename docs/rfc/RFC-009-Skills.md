---
status: historical
owner: Reticle Project
updated: 2026-09-19
---

> Historical design record. Current versioned specifications and schemas take precedence.

# RFC-009 — Skills

Original status: Stable
Version: 1.0.0
Author: Reticle Core
Original last updated: 2026-08-03

---

## 1. Purpose
This document defines how the runtime handles Agent Skills, which are reusable, composable capabilities that can be dynamically attached to agents to grant them specific environments, dependencies, and credentials.

## 2. Motivation
Instead of monolithic agents that hardcode all their dependencies and API keys, the framework needs a way to decouple capabilities. If multiple agents need to browse the web, they should all inherit a common `web-browser` skill rather than reinventing the integration.

## 3. Scope
This RFC covers:
- Skill Definitions (YAML format).
- Agent Composition (how agents declare skills).
- Dependency Management (Virtual Environment provisioning).
- Environment Variable injection.

## 4. Philosophy
Skills are first-class citizens. They define exactly what an agent needs to execute a capability, and the runtime is responsible for fulfilling those requirements *before* the agent starts.

## 5. Principles
- **Reusability**: A skill must be defined once and usable by any number of agents.
- **Isolation**: Skills must not bleed dependencies into the global host environment.
- **Aggregation**: The runtime must aggregate requirements across all of an agent's inherited skills and provision them cohesively.

## 6. Architectural Laws
1. Skills must be defined declaratively in YAML within a centralized registry.
2. The Orchestrator (`EnvironmentManager`) must dynamically provision isolated ephemeral environments (e.g., Python Virtual Environments) for each agent based on its aggregated skills.
3. The Orchestrator must inject aggregated `env_vars` safely into the execution context (the `os.Environ()` wrapper) of the worker sandbox.

## 7. Rationale
By utilizing an `EnvironmentManager` to dynamically provision `.reticle/envs/<agent_id>` sandboxes, we ensure that dependencies (like pip packages) are isolated per-agent. This prevents version conflicts between agents that might require different versions of the same library.

## 8. Trade-offs
- Creating virtual environments and installing dependencies at runtime (JIT provisioning) introduces a startup delay on the first execution. However, this is mitigated by caching the environments.

## 9. Future Considerations
- Supporting skills that require system-level dependencies (e.g., `apt-get install chromium`).
- Dynamic skill discovery and marketplace integration.

## 10. References
- Deprecated Skills Spec: `docs/specifications/skills/v1/001-skills.md`

## 11. Related RFCs
- RFC-008 — Agent Architecture
- RFC-011 — Runtime Instructions
