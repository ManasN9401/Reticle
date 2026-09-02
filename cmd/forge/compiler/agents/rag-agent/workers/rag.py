import os
import sys
import json
from litellm import completion
from forge_utils import TOOLS_REGISTRY, execute_tool, emit_log

MODEL = os.environ.get("FORGE_MODEL", "qwen2.5-coder")
SYSTEM_PROMPT = """You are an elite Knowledge Retrieval and RAG Agent.
Your job is to navigate vast directories, index them into the local vector space, and perform semantic similarity searches to extract specific context.
When a user asks a complex question about a codebase, use `query_knowledge` to retrieve chunks.
IMPORTANT: Do not just spit out raw code chunks to the user. You must SYNTHESIZE and SUMMARIZE the retrieved chunks into a clean, concise, highly-dense answer. Keep your final output extremely relevant and free of boilerplate so it doesn't break the 8k token limit of downstream agents.
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

    emit_log(f"Starting RAG Agent with model {MODEL}")

    for step in range(15):
        try:
            response = completion(
                model=MODEL,
                messages=messages,
                tools=TOOLS_REGISTRY,
                temperature=0.3
            )
        except Exception as e:
            emit_log(f"API Error: {str(e)}")
            sys.exit(1)

        msg = response.choices[0].message
        msg_dict = msg.model_dump() if hasattr(msg, "model_dump") else dict(msg)
        messages.append(msg_dict)

        tool_calls = parse_tool_calls(msg)
        if not tool_calls:
            print("--- FINAL OUTPUT ---")
            print(msg.content)
            emit_log("RAG task completed successfully.")
            sys.exit(0)

        for tc in tool_calls:
            args = json.loads(tc.function.arguments)
            emit_log(f"Executing {tc.function.name} with args {args}")
            
            result = execute_tool(tc.function.name, args)
            
            # Truncate result logging if it's huge
            res_str = str(result)
            emit_log(f"Tool {tc.function.name} result length: {len(res_str)}")
            
            messages.append({
                "role": "tool",
                "tool_call_id": tc.id,
                "name": tc.function.name,
                "content": res_str
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
