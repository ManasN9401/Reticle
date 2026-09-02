# RFC: IDE Context Injection for Reticle Agents

## 1. Motivation
Currently, Reticle agents operate entirely headlessly. They know the user's textual prompt (e.g., "Fix the physics bug") but lack the implicit context a human pair-programmer has. When a human asks a pair-programmer to "fix the bug", the programmer knows exactly which bug they are referring to because they can see what file the human has open on their screen and where their cursor is pointing.

By injecting the user's **Active IDE Context** (open files, active file, cursor position) into the agent's memory, we can enable "zero-context prompting". Users can simply say "Refactor this function" in their IDE, and the autonomous agents will instantly know which file and function to target without explicit instructions.

## 2. Architecture Flow
The proposed architecture bridges the gap between an external IDE extension (like VSCode) and the headless Forge Orchestrator.

1. **IDE Extension (The Producer)**
   - An IDE extension monitors the user's workspace state (active tab, open tabs, cursor line number).
   - When the user submits a prompt, the extension bundles the prompt with an `ide_context` payload.
   - It sends this bundle via WebSocket to the Forge Telemetry Server (`ws://localhost:8080/ws`).

2. **Telemetry Server & Waitlist (The Transport)**
   - The Telemetry Server parses the WebSocket `WaitlistCommand` and extracts the `ide_context` field.
   - The `WaitlistItem` struct stores this context.

3. **Dispatcher (The Injector)**
   - During `WaitlistManager.Pump()`, the context is published to the Orchestrator's event bus as a `MemoryWriteRequested` event (scoped to the specific execution ID).
   - Because of the implicit base context system in `dispatcher.go`, the `ide_context` memory key is automatically injected into every agent's runtime memory payload.

4. **Compiler Agents (The Consumers)**
   - **Architect Agent**: Uses the IDE context to design a better DAG. (e.g., If the user is looking at `player.py`, the architect assigns an agent to modify `player.py`).
   - **Worker Agents**: The `coder.py` bootstrap script intercepts the `ide_context` and prepends it to the LLM's user message.

## 3. Data Schema

### WebSocket Payload
The IDE extension will send the following JSON payload to the Telemetry WebSocket:

```json
{
  "action": "enqueue",
  "prompt": "Fix the physics collision bug here",
  "group": "",
  "mode": "parallel",
  "ide_context": "Active File: src/physics.py (Cursor at Line 42)\nOpen Files: src/main.py, src/player.py"
}
```

*Note: The `ide_context` is sent as a pre-formatted string to reduce parsing overhead in the Go backend, allowing the LLMs to consume it directly.*

## 4. Prompt Engineering

When the `ide_context` key is detected in the memory payload, the `coder.py` wrapper will automatically surface it to the agent before any tool calls are made.

**Injected User Message Prefix:**
```markdown
## IDE Context
The user currently has the following workspace context. Use this to infer what they are referring to (e.g., if they say "this file" or "this function"):
{ide_context}
```

## 5. Future Considerations
- **Selection Context**: If the user highlights a specific block of code, the IDE extension could include the actual string literal of the selected code in the `ide_context`.
- **Diagnostic Context**: If the user's cursor is hovering over a linter error, the extension could pass the exact error message to the agent.
