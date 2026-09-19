# Specialized skills

Skill YAML declares environment dependencies and optional explicitly passed environment variables. SKILL.md contains instructions. These are separate mechanisms.

Configured specialists load skill IDs from the registry and pass their instruction paths to generated SDK workers. For v1 compatibility, an unknown skill ID is logged and skipped instead of stopping all startup; generated workers also warn and skip an instruction file that disappears after registry loading. Treat either message as a degraded worker and fix the manifest or file before relying on its output. Standalone reference skills and third-party hook examples are not automatically active in Reticle.

Cloud/ML prerequisites are checked in the execution environment. Read the relevant skill and capability status before interpreting an agent's availability as deployment/training readiness.
