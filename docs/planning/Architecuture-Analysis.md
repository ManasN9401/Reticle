# Forge Architecture: Root-Cause Analysis

## Executive Summary

The core problem is **not** the tools, the prompts, or the LLM model. The architecture has **5 systemic failures** that compound on each other. Even a perfect LLM would struggle to produce good output under these constraints. The tools aren't a burden — they're just irrelevant, because the agents are never given enough context to know *what* to build.

---

## The 5 Root Causes

### 🔴 1. Agents Have No Idea What They're Building (The Fatal Flaw)

This is the single biggest problem, and it explains everything.

Look at what `agent-3` (Main Entrypoint Creator) actually receives as its system prompt ([agent-3.py:L57](file:///d:/Reticle/cmd/forge/workspaces/forge_workspace_20260811_185059/workers/agent-3.py#L57)):

```
"Create the game's main entrypoint, including the game loop, event handling,
and rendering. Ensure that the entrypoint is well-structured, efficient, and
integrates seamlessly with the game's overall logic."
```

And the user message ([agent-3.py:L58](file:///d:/Reticle/cmd/forge/workspaces/forge_workspace_20260811_185059/workers/agent-3.py#L58)):

```
"Process this request: {json.dumps(req)}. CRITICAL INSTRUCTION: You are an 
autonomous agent. You MUST use the `write_file` tool..."
```

**The `req` payload** contains the task ID, the execution ID, memory keys... but **NOT the user's original prompt**. The agent literally doesn't know it's building a "2D platformer game in pixel art graphics". It just sees a vague instruction to "create the game's main entrypoint."

**Why?** The generated agent YAMLs ([agent-3.yaml](file:///d:/Reticle/cmd/forge/workspaces/forge_workspace_20260811_185059/agents/agent-3.yaml)) have **no `memory:` field**, so the dispatcher's memory injection ([dispatcher.go:L70-L86](file:///d:/Reticle/runtime/agent/dispatcher.go#L70-L86)) skips them entirely. The `workspace_dir` and `user_prompt` keys are never hydrated into `req.Memory`.

Compare this to the compiler agents like `architect-agent.yaml`:
```yaml
memory:
  - "user_prompt"
  - "available_agents"
```

The generated agents have *none of this*. The scaffolder ([scaffolder.py:L32-L38](file:///d:/Reticle/cmd/forge/compiler/workers/scaffolder.py#L32-L38)) only generates:
```yaml
id: agent-3
name: "Main Entrypoint Creator"
description: "Creates the game's main entrypoint..."
version: 1.0.0
runtime: python
entrypoint: workers/agent-3.py
```

No `memory:` key. So the agent never sees `user_prompt` or `workspace_dir`.

> [!CAUTION]
> **Without `workspace_dir`**, the `write_file` tool defaults to writing into `"."` (the CWD), which is the workspace root after `os.Chdir()` in [main.go:L242](file:///d:/Reticle/cmd/forge/main.go#L242). So `src/` ends up at `workspace_root/src/` — this works by accident but is fragile.
> 
> **Without `user_prompt`**, the agent is flying completely blind. It has NO idea what the user asked for. It only has the vague `system_prompt` the architect wrote.

---

### 🔴 2. Agents Can't See Each Other's Work (Broken Data Flow)

The DAG is supposed to pipe artifacts from parent nodes to child nodes. The engine does this correctly in [workflow_engine.go:L219-L233](file:///d:/Reticle/runtime/agent/workflow_engine.go#L219-L233):

```go
for _, parentID := range exec.Workflow.Parents[nodeID] {
    if art, exists := exec.Artifacts[parentID]; exists {
        inputs = append(inputs, TaskInput{
            ArtifactID: string(art.ID),
            Name:       art.Name,
            Data:       art.Data,
        })
    }
}
```

So `node-3` (Main Entrypoint Creator) receives `node-2`'s output artifact as an `input`. But look at the generated worker script ([agent-3.py:L58](file:///d:/Reticle/cmd/forge/workspaces/forge_workspace_20260811_185059/workers/agent-3.py#L58)):

```python
{"role": "user", "content": f"Process this request: {json.dumps(req)}.
CRITICAL INSTRUCTION: ..."}
```

The entire `req` JSON is dumped as a string into the user message. This means the agent *technically* receives upstream artifacts embedded in the JSON blob... but it's buried inside a wall of JSON that also contains task IDs, execution IDs, and tool-calling instructions. 

The LLM has to parse this raw JSON blob and extract the relevant upstream context. An 8B parameter model cannot reliably do this. The upstream artifact data is effectively invisible to the agent.

**Before tools were added**, the agents likely received simpler payloads (or the old `file-writer.py` pattern extracted structured `file_operations` blocks). The tools added enormous prompt overhead that further buried the upstream context.

---

### 🔴 3. The System Prompt Is Too Small, Too Vague (Architect Prompt Quality)

The architect designs agents with prompts like:

```json
"system_prompt": "Create the game's main entrypoint, including the game loop,
event handling, and rendering. Ensure that the entrypoint is well-structured,
efficient, and integrates seamlessly with the game's overall logic."
```

This is a **2-sentence instruction** asking Llama 3.1 8B to build an entire game engine from scratch. Compare this to what the same model produced "before tools" — the difference isn't the tools, it's that the architect's prompt quality degraded.

The architect is using `groq/llama-3.1-8b-instant` to design these prompts. At 8B parameters, it generates vague, generic instructions. It doesn't specify:
- What language/framework (pygame? raw SDL?)
- What the game loop should contain (physics? rendering? input?)  
- What files to create or what interfaces to expose
- What the other agents are doing (so it can coordinate)

The agents don't know what their siblings are building because the architect doesn't tell them.

---

### 🔴 4. `write_file` Blocks Collaboration (File Collision Guard Is Too Aggressive)

In [coder.py:L74-L75](file:///d:/Reticle/cmd/forge/compiler/workers/coder.py#L74-L75):

```python
if os.path.exists(full_path):
    return f"Error: File {path} already exists! You are working in a shared
    workspace and cannot blindly overwrite files."
```

This was added to prevent agents from overwriting each other. But it creates a new problem: **agents that run later in the DAG literally cannot contribute to files created by earlier agents**. 

For example, if `node-2` (Game Logic Developer) creates `main.py` with the game loop, then `node-3` (Main Entrypoint Creator) — whose *entire job* is to create `main.py` — gets blocked with "Error: File main.py already exists!"

The agent is then forced to either:
1. Use `replace_file_content` (which requires exact string matching — nearly impossible when you can't see the file)
2. Create a *different* file with a different name (leading to fragmented, disconnected code)
3. Give up and write placeholder code

Most agents choose option 3.

---

### 🔴 5. `mark_task_complete` Creates an Infinite Loop Without Docker

The `mark_task_complete` tool requires:
```json
"required": ["summary", "test_command_used", "test_output"]
```

And the prompt says:
```
You MUST proactively test your work by using package managers or running python
scripts to verify them (e.g. `python -m py_compile`).
```

But `execute_terminal_command` runs inside Docker ([coder.py:L9-L35](file:///d:/Reticle/cmd/forge/compiler/workers/coder.py#L9-L35)):

```python
docker_cmd = ["docker", "exec", container_name, "bash", "-c", command]
```

If Docker isn't running, this fails. The agent can't test. If it can't test, it can't call `mark_task_complete`. If it can't call `mark_task_complete`, the loop ([agent-3.py:L140-L152](file:///d:/Reticle/cmd/forge/workspaces/forge_workspace_20260811_185059/workers/agent-3.py#L140-L152)) injects:

```
"ERROR: You cannot just stop calling tools. You must explicitly call the
mark_task_complete tool..."
```

And the agent loops forever, burning API credits. Even when Docker IS running, Llama 8B often fabricates the test output (as seen in [exec-001_node-1_output.md](file:///d:/Reticle/cmd/forge/workspaces/forge_workspace_20260811_185059/outputs/exec-001/exec-001_node-1_output.md#L8)):

```
### Test Output
No syntax errors found.
```

This is hallucinated — the agent never actually ran `python -m py_compile`. It just filled in the `mark_task_complete` fields with plausible text to escape the loop.

---

## Why It Worked Better Before Tools

Before tools, the agents operated in a simpler paradigm:
1. They received a prompt and outputted raw text/code
2. The `file-writer.py` worker parsed `### FILE:` blocks and `file_operations` JSON from the output
3. There was no tool-calling overhead, no `mark_task_complete` gate, no Docker dependency

The LLM's **entire context window** was available for creative work. Now, the context window is consumed by:
- 8 tool definitions (~800 tokens)
- Tool-calling instructions (~200 tokens)  
- The raw `req` JSON dump (~500+ tokens)
- Accumulated tool call/response messages (grows with each iteration)

For an 8B model with an 8K effective context, this overhead is **devastating**. The model has barely any room left to think about the actual game.

---

## Proposed Fix Strategy

### Fix 1: Inject `memory` and `user_prompt` into Generated Agent YAMLs

In [scaffolder.py:L32-L38](file:///d:/Reticle/cmd/forge/compiler/workers/scaffolder.py#L32-L38), add memory keys:

```python
agent_yaml_str = f"""id: {agent_id}
name: "{...}"
description: "{...}"
version: 1.0.0
runtime: python
entrypoint: workers/{agent_id}.py
memory:
  - "user_prompt"
  - "workspace_dir"
"""
```

This single change fixes Root Causes #1 and partially #2.

### Fix 2: Surface Upstream Artifacts as Clean Context

Instead of dumping `json.dumps(req)` into the user message, extract and format the upstream artifacts cleanly in [coder.py](file:///d:/Reticle/cmd/forge/compiler/workers/coder.py):

```python
# Build context from upstream inputs
upstream_context = ""
for inp in req.get("inputs", []):
    upstream_context += f"### From {inp['name']}:\n{inp['data']}\n\n"

messages = [
    {"role": "system", "content": sys_prompt},
    {"role": "user", "content": f"The user wants: {user_prompt}\n\n"
     f"## Context from previous agents:\n{upstream_context}\n\n"
     f"Now complete YOUR task. Use write_file to save your work."}
]
```

### Fix 3: Relax the File Collision Guard

Change `write_file` to allow overwrites when the file was created by the same agent (or just allow overwrites with a warning):

```python
if os.path.exists(full_path):
    # Allow overwrite but warn
    return f"Warning: Overwriting existing file {path}"
```

### Fix 4: Make `mark_task_complete` Optional, Not Mandatory

Remove the infinite loop trap. Instead, use a max-iteration guard:

```python
MAX_ITERATIONS = 15
for iteration in range(MAX_ITERATIONS):
    response = do_completion(messages)
    ...
    if not msg.tool_calls:
        break  # Agent is done
```

Keep `mark_task_complete` as an *available* tool but don't force it. The agent's output artifact already captures `files_modified`.

### Fix 5: Improve Architect Prompt Quality

The architect should generate **detailed, technical** system prompts that include:
- The exact user request
- What language and framework to use
- What files to create
- What other agents are handling (so they don't overlap)
- Specific function signatures or interfaces to expose

---

## Summary Table

| Root Cause | Severity | Fix Complexity | Impact |
|---|---|---|---|
| Agents don't receive `user_prompt` or `workspace_dir` | 🔴 Critical | Low (1 line in scaffolder) | Agents will finally know what to build |
| Upstream artifacts buried in raw JSON | 🔴 Critical | Medium (refactor user message) | Agents will see and build upon predecessors' work |
| Architect generates vague prompts | 🟡 High | Medium (prompt engineering) | Better task decomposition |
| `write_file` blocks collaboration | 🟡 High | Low (relax guard) | Agents can contribute to shared files |
| `mark_task_complete` infinite loop | 🟠 Medium | Low (add max iterations) | Prevents stuck agents burning credits |
