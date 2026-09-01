import sys
import json
import logging
import shutil
import subprocess
import os

real_stdout = sys.stdout
sys.stdout = sys.stderr

import litellm
litellm.suppress_debug_info = True

from litellm import completion
from tenacity import retry, stop_after_attempt, wait_exponential

logging.basicConfig(level=logging.ERROR)

from forge_utils import execute_terminal_command, read_file, search_codebase, write_file, list_dir, replace_file_content, read_url, tools

def is_low_quality_model(model_id):
    if not model_id:
        return True
    model_id = model_id.lower()
    if ":free" in model_id or "mini" in model_id or "flash-lite" in model_id or "8b" in model_id:
        return True
    return False

def check_binary(binary_name):
    return shutil.which(binary_name) is not None

def main():
    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    sys.stderr.reconfigure(encoding='utf-8')

    line = sys.stdin.readline()
    if not line:
        return
        
    try:
        req = json.loads(line)
    except Exception:
        return

    task_id = req.get("id", "unknown_task")
    params = req.get("parameters", {})
    llm_model = params.get("llm_model", "openrouter/anthropic/claude-3-5-sonnet-20241022")
    api_key_env = params.get("api_key")
    api_key = os.environ.get(api_key_env) if api_key_env else None
    
    mem = req.get("memory", {})
    workspace_dir = mem.get("workspace_dir", ".")
    allow_native_execution = mem.get("allow_native_execution", "false").lower() == "true"
    src_dir = os.path.join(workspace_dir, "src")
    os.makedirs(src_dir, exist_ok=True)
    
    # 1. Hallucination Guardrail Check
    if is_low_quality_model(llm_model):
        resp = {
            "id": task_id,
            "error": f"SECURITY_BLOCK: Model '{llm_model}' is flagged as low-quality or free-tier. It is strictly forbidden from executing DevOps tasks to prevent catastrophic infrastructure hallucinations."
        }
        real_stdout.write(json.dumps(resp) + "\n")
        sys.exit(1)

    # 2. Binary Tooling Check
    missing_binaries = []
    for tool in ["terraform", "docker", "kubectl"]:
        if not check_binary(tool):
            missing_binaries.append(tool)
            
    warnings = ""
    if missing_binaries:
        warnings = f"WARNING: Missing CLI binaries for {', '.join(missing_binaries)} on the host. The agent must fallback to Python SDKs (boto3, docker-py) and MUST request Human-in-the-Loop approval before proceeding.\n"
        print(warnings, file=sys.stderr)

    # Build ReAct Context
    sys_prompt = "You are a Senior DevOps Engineer Agent. " + warnings + """
You must generate, validate, and orchestrate infrastructure-as-code.
CRITICAL: You MUST use the tools to read the workspace, execute tests, and modify files.
CRITICAL: Do NOT execute destructive commands without prompting the user.
You must use `write_file` to save your work to the `src/` directory.
When finished, call `mark_task_complete` with a summary of what you did.
"""

    upstream_context = ""
    for inp in req.get("inputs", []):
        inp_name = inp.get("name", "Unknown")
        inp_data = inp.get("data", "")
        if inp_data:
            upstream_context += f"### From {inp_name}:\n{str(inp_data)[:3000]}\n\n"
            
    user_prompt = mem.get("user_prompt", "Complete your assigned DevOps task.")
    user_msg = f"## User's Goal\n{user_prompt}\n"
    if upstream_context:
        user_msg += f"\n## Context From Previous Agents\n{upstream_context}"

    messages = [
        {"role": "system", "content": sys_prompt},
        {"role": "user", "content": user_msg}
    ]

    @retry(stop=stop_after_attempt(7), wait=wait_exponential(multiplier=2, min=5, max=120))
    def do_completion(messages):
        try:
            resp = completion(
                model=llm_model,
                api_key=api_key,
                max_tokens=4000,
                messages=messages,
                tools=tools,
                parallel_tool_calls=False,
                timeout=60
            )
            return resp
        except Exception as e:
            err_str = str(e)
            if any(term in err_str.lower() for term in ["ratelimit", "429", "quota", "too large", "context_window", "invalid_request_error"]):
                print(f"[LLM] Hard limit reached on {llm_model}: {err_str[:150]}", file=sys.stderr)
                sys.exit(1)
            print(f"[LLM] Error: {err_str[:300]}", file=sys.stderr)
            raise e

    files_modified = {}
    memory_mutations = []
    task_completed = False
    
    for _iteration in range(25):
        response = do_completion(messages)
        msg = response.choices[0].message
        
        clean_msg = {"role": "assistant", "content": msg.content or ""}
        if msg.tool_calls:
            clean_msg["tool_calls"] = []
            for tc in msg.tool_calls:
                clean_msg["tool_calls"].append({
                    "id": tc.id,
                    "type": "function",
                    "function": {
                        "name": tc.function.name,
                        "arguments": tc.function.arguments
                    }
                })
        messages.append(clean_msg)
        
        if msg.content:
            for line in msg.content.split('\n'):
                print(f"[LLM] {line}", file=sys.stderr)
                
        if msg.tool_calls:
            for tool_call in msg.tool_calls:
                func_name = tool_call.function.name
                args = json.loads(tool_call.function.arguments)
                print(f"[TOOL] Executing {func_name}", file=sys.stderr)
                
                try:
                    if func_name == "execute_terminal_command":
                        res = execute_terminal_command(args.get("command"), workspace_dir, allow_native_execution)
                    elif func_name == "write_file":
                        res = write_file(args.get("path"), args.get("content"), workspace_dir, files_modified)
                        if "Successfully" in res:
                            files_modified[args.get("path")] = args.get("content")
                    elif func_name == "read_file":
                        res = read_file(args.get("path"), workspace_dir)
                    elif func_name == "list_dir":
                        res = list_dir(args.get("path"), workspace_dir)
                    elif func_name == "mark_task_complete":
                        task_completed = True
                        res = "Task complete."
                        break
                    else:
                        res = "Tool unsupported in this context or mocked."
                except Exception as e:
                    res = f"Tool execution failed: {e}"
                    
                messages.append({
                    "role": "tool",
                    "name": func_name,
                    "tool_call_id": tool_call.id,
                    "content": str(res)
                })
                
            if task_completed:
                break
        else:
            messages.append({
                "role": "user",
                "content": "ERROR: You stopped calling tools without calling 'mark_task_complete'. If you are finished, you MUST call 'mark_task_complete'."
            })

    result = messages[-1].get("content", "Task finished via tool.") if not task_completed else args.get("summary", "Task complete.")
    if files_modified:
        result += "\n\n### Files Modified By This Agent:\n"
        for p, c in files_modified.items():
            result += f"#### {p}\n```\n{c}\n```\n"

    artifact = {
        "id": f"{task_id}_output",
        "name": f"DevOps Output",
        "type": "document/markdown",
        "data": result
    }
    
    resp_obj = {
        "id": task_id,
        "artifact": artifact
    }
    
    real_stdout.write(json.dumps(resp_obj) + "\n")

if __name__ == "__main__":
    main()
