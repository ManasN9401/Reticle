import os
import sys
import json
from litellm import completion
from forge_utils import TOOLS_REGISTRY, execute_tool, emit_log, ui_state

MODEL = os.environ.get("FORGE_MODEL", "qwen2.5-coder")
SYSTEM_PROMPT = """You are an elite Frontend UI/UX Designer and Engineer.
You have access to a tool `generate_local_asset` which commands a local ComfyUI GPU cluster.
NEVER use placeholder images. Always generate custom UI assets for your designs.
Build visually stunning, dynamic interfaces using modern CSS paradigms (Glassmorphism, animations).
"""

def parse_tool_calls(message):
    if not hasattr(message, "tool_calls") or not message.tool_calls:
        return []
    return message.tool_calls

def run_agent(task_prompt, context_files=None):
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": f"Task: {task_prompt}\nContext: {context_files or 'None'}"}
    ]

    emit_log(f"Starting Frontend UI/UX Agent with model {MODEL}")

    for step in range(15):
        try:
            response = completion(
                model=MODEL,
                messages=messages,
                tools=TOOLS_REGISTRY,
                temperature=0.4
            )
        except Exception as e:
            emit_log(f"API Error: {str(e)}")
            sys.exit(1)

        msg = response.choices[0].message
        
        # litellm handles dict conversion differently depending on version
        msg_dict = msg.model_dump() if hasattr(msg, "model_dump") else dict(msg)
        messages.append(msg_dict)

        tool_calls = parse_tool_calls(msg)
        if not tool_calls:
            print("--- FINAL OUTPUT ---")
            print(msg.content)
            emit_log("Frontend task completed successfully.")
            sys.exit(0)

        for tc in tool_calls:
            args = json.loads(tc.function.arguments)
            emit_log(f"Executing {tc.function.name} with args {args}")
            
            result = execute_tool(tc.function.name, args)
            
            emit_log(f"Tool {tc.function.name} result length: {len(str(result))}")
            
            messages.append({
                "role": "tool",
                "tool_call_id": tc.id,
                "name": tc.function.name,
                "content": str(result)
            })

    print("Error: Max iterations reached.")
    sys.exit(1)

if __name__ == "__main__":
    if len(sys.argv) > 1:
        with open(sys.argv[1], "r") as f:
            payload = json.load(f)
        run_agent(payload.get("prompt", ""), payload.get("context", []))
    else:
        print("Error: No payload provided.")
        sys.exit(1)
