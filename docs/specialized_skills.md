# Specialized skills

Skill YAML declares environment dependencies and optional explicitly passed environment variables. SKILL.md contains instructions. These are separate mechanisms.

Generated SDK workers and configured specialists read their declared instruction files explicitly. Missing instruction files fail generation/configuration. Standalone reference skills and third-party hook examples are not automatically active in Reticle.

Cloud/ML prerequisites are checked in the execution environment. Read the relevant skill and capability status before interpreting an agent's availability as deployment/training readiness.
