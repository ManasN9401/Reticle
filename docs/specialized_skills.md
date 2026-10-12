# Specialized skills

Skill YAML declares environment dependencies and optional explicitly passed environment variables. SKILL.md contains instructions. These are separate mechanisms.

## How a skill reaches a worker

A skill is only guidance until its instruction file is copied into a worker's prompt. That happens in two places:

- **Generated agents.** The architect lists skill IDs per agent; the compiler appends each `skills/<id>/SKILL.md` to that agent's instructions and writes the result to `agents/<id>/workers/<id>.instructions.md` in the session folder. That file is exactly what the model is given, so it is the place to check what a worker was told. The entrypoint is a few lines that read it, which keeps Python tracebacks short and keeps skill text out of the failure classifier.
- **Maintained specialists** (frontend, devops, ml, pentester, rag, latex) read their own skill when they start.

Both use the same loader (`forge_utils.load_skill_instructions`). The whole file is added to the prompt, so a skill's size is its cost: a 15 KB skill is 15 KB of every request the agent makes. The compiled worker reports what it carries at startup, as the first lines of the LLM tab:

```
Skills applied: frontend-uiux (4.7 KB), verification-loop (1.5 KB)
Skills with no instruction file (dependencies only, or the file is missing): llm-worker
```

`Skills applied: none` means the agent was given no skill text. Each tool call in the same tab also names what it was about (the command, path or pattern, redacted and cut to 160 characters), and Studio's Tools view shows it beside the call. A skill with dependencies and no `SKILL.md` (`llm-worker`) is reported on the second line by design; a skill that should have instructions and appears there is a missing file.

## How the architect chooses

Every manifest should carry `use_when`: one or two sentences saying which agents should list the skill and, where it matters, which they should not. The architect sees `description`, `use_when` and `instructions_bytes` for each skill and is told to assign the one to three most specific skills. A DAG that lists more than three skills for one agent is rejected and re-planned. Write `use_when` about the work, not about the skill: "React or Next.js component work. Not for vanilla JavaScript, three.js or plain HTML/CSS pages" lets a model rule it out, where "frontend development patterns" does not.

The architect only sees skills that have a manifest in `skills/` (or `compiler/skills/`). A `SKILL.md` without a manifest cannot be assigned. `TestEverySkillIsAssignableOrExcluded` fails for any instruction file that has neither a manifest nor an entry in its explicit unassignable list with a reason, so a skill cannot silently become unreachable again.

## Not active automatically

For v1 compatibility, an unknown skill ID is logged and skipped instead of stopping all startup; generated workers also skip an instruction file that disappears after registry loading (the startup line above shows it). Treat either as a degraded worker and fix the manifest or file before relying on its output. Standalone reference skills and third-party hook examples (`continuous-learning`, `strategic-compact`, `eval-harness`, `project-guidelines-example`, `graphify`) are not assignable and not automatically active in Reticle.

Cloud/ML prerequisites are checked in the execution environment. Read the relevant skill and capability status before interpreting an agent's availability as deployment/training readiness.
