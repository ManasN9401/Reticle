import sys
import json
import logging
import os

real_stdout = sys.stdout
sys.stdout = sys.stderr

import litellm
litellm.suppress_debug_info = True

from litellm import completion
from tenacity import retry, stop_after_attempt, wait_exponential

logging.basicConfig(level=logging.ERROR)

from forge_utils import execute_terminal_command, read_file, search_codebase, write_file, list_dir, replace_file_content, read_url, tools

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
    
    # Pre-flight Hardware Warning
    warnings = ""
    if not allow_native_execution:
        warnings = "CRITICAL WARNING: `allow_native_execution` is FALSE. You are running in an isolated Docker sandbox without GPU passthrough (which usually requires `--gpus all`). Any PyTorch training scripts you execute here will fallback to the CPU. If you execute a training run here, you MUST artificially limit the dataset (e.g. 100 samples) and train for EXACTLY 1 epoch to prevent freezing the orchestrator. For full runs, instruct the user to run the script natively on their host machine."
        print(f"\n[WARNING] ML Agent detected Docker isolation without GPU passthrough. PyTorch will default to CPU. To use your GPU, you must run the generated scripts natively on your host machine.", file=sys.stderr)

    # Build ReAct Context
    sys_prompt = "You are a Senior Machine Learning Engineer. " + warnings + """
You are strictly limited to the PyTorch and HuggingFace ecosystems. You MUST NOT use TensorFlow or Keras.
CRITICAL VRAM MANAGEMENT: You must assume the user has a 16GB VRAM limit. You MUST use gradient accumulation (e.g. `gradient_accumulation_steps=8`) and small batch sizes to prevent OOM errors.
CRITICAL REPRODUCIBILITY: You MUST set deterministic random seeds (`torch.manual_seed`, `np.random.seed`) at the top of every script.
CRITICAL TRACKING: You MUST implement model checkpointing to save weights periodically.
When finished, call `mark_task_complete` with a summary of the scripts you generated.
"""

    upstream_context = ""
    for inp in req.get("inputs", []):
        inp_name = inp.get("name", "Unknown")
        inp_data = inp.get("data", "")
        if inp_data:
            upstream_context += f"### From {inp_name}:\n{str(inp_data)[:3000]}\n\n"
            
    user_prompt = mem.get("user_prompt", "Perform an ML Engineering task.")
    ide_context = mem.get("ide_context", "")
    prompt_history = mem.get("prompt_history", "")
    
    def build_messages(hist, upstr):
        umsg = ""
        if ide_context:
            umsg += f"## IDE Context\nThe user currently has the following workspace context. Use this to infer what they are referring to (e.g., if they say 'this file' or 'this function'):\n{ide_context}\n\n"
        if hist:
            umsg += f"## Previous Iterations History\nThis task is a continuation of previous work. Here is the history of previous prompts in this group:\n{hist}\n\n"
        umsg += f"## User's Goal\n{user_prompt}\n"
        if upstr:
            umsg += f"\n## Context From Previous Agents\n{upstr}"
        return [
            {"role": "system", "content": sys_prompt},
            {"role": "user", "content": umsg}
        ]

    messages = build_messages(prompt_history, upstream_context)

    # Dynamic Context Compression (4-Stage Heuristic)
    try:
        from litellm import token_counter, get_max_tokens
        max_tokens = get_max_tokens(llm_model)
        if not max_tokens:
            max_tokens = 8192
        
        curr_tokens = token_counter(model=llm_model, messages=messages)
        # Stage 1: Handled by skipping Workspace Tree (not present in ML agent by default)
        
        if curr_tokens > max_tokens * 0.85:
            print(f"[LLM] Context overflow detected ({curr_tokens} > {int(max_tokens * 0.85)}). Stage 2 Compression: Truncating Prompt History...", file=sys.stderr)
            trunc_hist = prompt_history[-1500:] if len(prompt_history) > 1500 else prompt_history
            messages = build_messages(trunc_hist, upstream_context)
            curr_tokens = token_counter(model=llm_model, messages=messages)
            
        if curr_tokens > max_tokens * 0.85:
            print(f"[LLM] Still overflowing ({curr_tokens}). Stage 3 Compression: Middle-Out truncation on Upstream Context...", file=sys.stderr)
            base_tokens = token_counter(model=llm_model, messages=build_messages(trunc_hist, ""))
            allowed_upstr_tokens = (max_tokens * 0.85) - base_tokens
            if allowed_upstr_tokens < 100: allowed_upstr_tokens = 100
            allowed_chars = int(allowed_upstr_tokens * 3.5)
            
            if len(upstream_context) > allowed_chars:
                keep_front = int(allowed_chars * 0.20)
                keep_back = int(allowed_chars * 0.80)
                trunc_upstr = upstream_context[:keep_front] + "\n...[CONTENT TRUNCATED FOR CONTEXT LIMIT]...\n" + upstream_context[-keep_back:]
            else:
                trunc_upstr = upstream_context[:allowed_chars]
                
            messages = build_messages(trunc_hist, trunc_upstr)
            curr_tokens = token_counter(model=llm_model, messages=messages)
            
        if curr_tokens > max_tokens * 0.85:
            print(f"[LLM] Still overflowing ({curr_tokens}). Stage 4 Compression: Stripping IDE Context...", file=sys.stderr)
            ide_context = ""
            messages = build_messages(trunc_hist, trunc_upstr)
            curr_tokens = token_counter(model=llm_model, messages=messages)
            
        print(f"[LLM] Compression successful. Final tokens: {curr_tokens}", file=sys.stderr)
            
    except Exception as e:
        print(f"[LLM] Warning: Dynamic context compression failed: {e}", file=sys.stderr)

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
    task_completed = False
    
    for _iteration in range(30):
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
                    elif func_name == "replace_file_content":
                        res = replace_file_content(args.get("path"), args.get("target_content"), args.get("replacement_content"), workspace_dir)
                        if "Successfully" in res:
                            full_path = os.path.join(workspace_dir, "src", args.get("path"))
                            with open(full_path, "r", encoding="utf-8") as f:
                                files_modified[args.get("path")] = f.read()
                    elif func_name == "read_file":
                        res = read_file(args.get("path"), workspace_dir)
                    elif func_name == "list_dir":
                        res = list_dir(args.get("path"), workspace_dir)
                    elif func_name == "search_codebase":
                        res = search_codebase(args.get("regex_pattern"), workspace_dir)
                    elif func_name == "mark_task_complete":
                        task_completed = True
                        res = "Task complete."
                        break
                    else:
                        res = "Tool unsupported in this context."
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
            result += f"#### {p}\n```python\n{c}\n```\n"

    artifact = {
        "id": f"{task_id}_output",
        "name": f"Machine Learning Engineering Report",
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
