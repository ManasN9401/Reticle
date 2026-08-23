"""
HyperParallel Worker Script
"""
import sys
import json

utils_code = """import os, subprocess, re

def execute_terminal_command(command, workspace_dir, allow_native=False):
    try:
        src_dir = os.path.join(workspace_dir, "src")
        if allow_native:
            # Run natively instead of Docker
            result = subprocess.run(command, shell=True, cwd=src_dir, capture_output=True, text=True, timeout=120)
            output = result.stdout + "\\n" + result.stderr
            if result.returncode != 0:
                output = f"Command failed with exit code {result.returncode}:\\n" + output
            return output[:10000]
        else:
            container_name = "forge_" + os.path.basename(os.path.abspath(workspace_dir)).replace(".", "_").replace("-", "_")
            
            # Check if container is running
            check = subprocess.run(["docker", "ps", "-q", "-f", f"name=^{container_name}$"], capture_output=True, text=True)
            if not check.stdout.strip():
                # Start it in background
                subprocess.run([
                    "docker", "run", "-d", "--rm", "--name", container_name,
                    "-v", f"{os.path.abspath(workspace_dir)}:/workspace",
                    "-w", "/workspace",
                    "ghcr.io/astral-sh/uv:python3.12-bookworm-slim",
                    "sleep", "infinity"
                ], capture_output=True)
                
            docker_cmd = [
                "docker", "exec", container_name,
                "bash", "-c", command
            ]
            result = subprocess.run(docker_cmd, capture_output=True, text=True, timeout=120)
            output = result.stdout + "\\n" + result.stderr
            if result.returncode != 0:
                output = f"Command failed with exit code {result.returncode}:\\n" + output
            return output[:10000]
    except Exception as e:
        return f"Error executing command: {e}"

def read_file(path, workspace_dir):
    full_path = os.path.join(workspace_dir, "src", path)
    if not os.path.exists(full_path):
        return f"Error: File {path} not found."
    try:
        with open(full_path, "r", encoding="utf-8") as f:
            return f.read()[:20000]
    except Exception as e:
        return f"Error reading file: {e}"

def search_codebase(regex_pattern, workspace_dir):
    results = []
    src_dir = os.path.join(workspace_dir, "src")
    try:
        pattern = re.compile(regex_pattern)
        for root, _, files in os.walk(src_dir):
            for file in files:
                full_path = os.path.join(root, file)
                rel_path = os.path.relpath(full_path, src_dir)
                try:
                    with open(full_path, "r", encoding="utf-8") as f:
                        lines = f.readlines()
                    for i, line in enumerate(lines):
                        if pattern.search(line):
                            results.append(f"{rel_path}:{i+1}: {line.strip()}")
                except:
                    pass
        if not results:
            return "No matches found."
        return "\\n".join(results[:500])
    except Exception as e:
        return f"Error searching codebase: {e}"

def write_file(path, content, workspace_dir, files_modified):
    full_path = os.path.join(workspace_dir, "src", path)
    
    if os.path.exists(full_path) and path not in files_modified:
        return f"ERROR: File {path} already exists! Overwriting is STRICTLY FORBIDDEN to prevent deleting other agents' work. You MUST use `read_file` to inspect the existing contents, and then use `replace_file_content` to make surgical additions."
        
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    try:
        content = content.replace('\\\\n', '\\n') if content else ""
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)
        return f"Successfully wrote to {path}"
    except Exception as e:
        return f"Error writing file: {e}"
def list_dir(path, workspace_dir):
    full_path = os.path.join(workspace_dir, "src", path)
    if not os.path.exists(full_path):
        return f"Error: Path {path} not found."
    try:
        if not os.path.isdir(full_path):
            return f"Error: {path} is not a directory."
        items = os.listdir(full_path)
        return "\\n".join(items) if items else "Directory is empty."
    except Exception as e:
        return f"Error listing directory: {e}"

def replace_file_content(path, target_content, replacement_content, workspace_dir):
    full_path = os.path.join(workspace_dir, "src", path)
    if not os.path.exists(full_path):
        return f"Error: File {path} not found."
    try:
        with open(full_path, "r", encoding="utf-8") as f:
            content = f.read()
        target_content = target_content.replace('\\\\n', '\\n') if target_content else ""
        replacement_content = replacement_content.replace('\\\\n', '\\n') if replacement_content else ""
        if target_content not in content:
            return "Error: target_content not found in file. Make sure the indentation and whitespace match exactly."
        content = content.replace(target_content, replacement_content, 1)
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)
        return f"Successfully replaced content in {path}"
    except Exception as e:
        return f"Error replacing content: {e}"

def read_url(url, workspace_dir):
    try:
        import urllib.request
        import re
        req = urllib.request.Request(url, headers={'User-Agent': 'Mozilla/5.0'})
        with urllib.request.urlopen(req, timeout=10) as response:
            html = response.read().decode('utf-8', errors='ignore')
            text = re.sub('<[^<]+>', ' ', html)
            text = re.sub('\\s+', ' ', text)
            return text[:20000]
    except Exception as e:
        return f"Error reading URL: {e}"

tools = [
    {
        "type": "function",
        "function": {
            "name": "execute_terminal_command",
            "description": "Execute a terminal command directly in the workspace/src directory.",
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {"type": "string", "description": "The command to execute (e.g. 'pytest', 'python main.py', 'dir')"}
                },
                "required": ["command"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "read_file",
            "description": "Read the contents of a file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Relative path to the file inside src/"}
                },
                "required": ["path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "search_codebase",
            "description": "Search the codebase using a regex pattern.",
            "parameters": {
                "type": "object",
                "properties": {
                    "regex_pattern": {"type": "string", "description": "Regex pattern to search for"}
                },
                "required": ["regex_pattern"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "write_file",
            "description": "Write or overwrite a file with new content.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Relative path to the file inside src/"},
                    "content": {"type": "string", "description": "The full content to write to the file"}
                },
                "required": ["path", "content"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "list_dir",
            "description": "List files and directories in a given path.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Relative path to list (e.g. '.', 'src', 'tests')"}
                },
                "required": ["path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "replace_file_content",
            "description": "Replace an exact target string with a new string in a file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Relative path to the file inside src/"},
                    "target_content": {"type": "string", "description": "The exact string to be replaced. MUST MATCH EXACTLY including whitespace!"},
                    "replacement_content": {"type": "string", "description": "The content to replace it with."}
                },
                "required": ["path", "target_content", "replacement_content"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "read_url",
            "description": "Fetch and extract text content from a URL (e.g. documentation, API references, tutorials).",
            "parameters": {
                "type": "object",
                "properties": {
                    "url": {"type": "string", "description": "The URL to read"}
                },
                "required": ["url"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "write_memory",
            "description": "Write a value to Shared Memory.",
            "parameters": {
                "type": "object",
                "properties": {
                    "key": {"type": "string", "description": "The key to write"},
                    "value": {"type": "string", "description": "The value to store"},
                    "scope": {"type": "string", "enum": ["global", "workflow", "execution", "agent"], "description": "The scope of the memory (defaults to execution)"}
                },
                "required": ["key", "value"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "read_memory",
            "description": "Read a value from Shared Memory that was injected by the orchestrator at startup.",
            "parameters": {
                "type": "object",
                "properties": {
                    "key": {"type": "string", "description": "The key to read"}
                },
                "required": ["key"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "mutate_graph",
            "description": "Dynamically rewrite the DAG to trigger another agent.",
            "parameters": {
                "type": "object",
                "properties": {
                    "action": {"type": "string", "enum": ["delegate"], "description": "The mutation action"},
                    "target_agent": {"type": "string", "description": "The ID of the agent to delegate to"},
                    "return_to_supervisor": {"type": "boolean", "description": "Whether control should return to the supervisor afterwards"}
                },
                "required": ["action", "target_agent"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "mark_task_complete",
            "description": "Call this tool to indicate you have fully completed and tested your assigned task. You MUST test your code using execute_terminal_command BEFORE calling this.",
            "parameters": {
                "type": "object",
                "properties": {
                    "summary": {"type": "string", "description": "A summary of what you did and the results of your tests."}
                },
                "required": ["summary"]
            }
        }
    }
]
"""


def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    inputs = req.get("inputs", [])
    
    dag_json_str = ""
    for i in inputs:
        if i.get("name") == "DAG JSON":
            dag_json_str = i.get("data", "{}")
            break
            
    dag = json.loads(dag_json_str)
    
    generated_files = {}
    
    for agent in dag.get("agents", []):
        if not agent.get("is_new") and not agent.get("system_prompt"):
            continue
            
        agent_id = agent.get("id")
        sys_prompt = agent.get("system_prompt", "You are a helpful assistant.")
        sys_prompt += """\n\nYou are an autonomous agent equipped with tools. You must use the tools to read the workspace, execute tests, and modify files.
You have full root access to a Debian terminal via `execute_terminal_command`. You can test your work by running standard compilation or execution commands for your assigned language (e.g. `node src/index.js`, `python src/main.py`, `go build`). Use `read_url` to look up documentation if you are stuck. Use `list_dir` to explore the workspace instead of guessing file paths.
CRITICAL REQUIREMENT: You MUST write FULLY FUNCTIONAL, complete code. You are strictly FORBIDDEN from using placeholders like `pass`, `TODO`, or `...`. Every function must have real implementation logic!
CRITICAL BOUNDARY RULE: Do NOT rewrite or overwrite files owned by other components from scratch. If a file exists, use `read_file` to inspect it and ONLY use `replace_file_content` to surgically inject your specific feature. Overwriting existing files with `write_file` will destroy other agents' work and is STRICTLY FORBIDDEN unless you are the original creator of that file.
When you are finished and have VERIFIED your code works via execute_terminal_command, call the `mark_task_complete` tool with a summary of what you did."""
        
        code = f"""import sys, json, time, logging, os

real_stdout = sys.stdout
sys.stdout = sys.stderr

import litellm
litellm.suppress_debug_info = True

from litellm import completion
from tenacity import retry, stop_after_attempt, wait_exponential, before_sleep_log

logging.basicConfig(level=logging.CRITICAL)
logger = logging.getLogger(__name__)

from forge_utils import execute_terminal_command, read_file, search_codebase, write_file, list_dir, replace_file_content, read_url, tools

def main():
    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    sys.stderr.reconfigure(encoding='utf-8')

    line = sys.stdin.readline()
    if not line: return
    try:
        req = json.loads(line)
        req_id = req.get("id")
        
        mem = req.get("memory", {{}})
        workspace_dir = mem.get("workspace_dir", ".")
        allow_native_execution = mem.get("allow_native_execution", "false").lower() == "true"
        src_dir = os.path.join(workspace_dir, "src")
        os.makedirs(src_dir, exist_ok=True)
        
        model = req.get("parameters", {{}}).get("llm_model", "openrouter/anthropic/claude-3-5-sonnet-20241022")
        api_key_env = req.get("parameters", {{}}).get("api_key")
        api_key = os.environ.get(api_key_env) if api_key_env else None
        
        @retry(stop=stop_after_attempt(7), wait=wait_exponential(multiplier=2, min=5, max=120))
        def do_completion(messages):
            try:
                resp = completion(
                    model=model,
                    api_key=api_key,
                    messages=messages,
                    tools=tools,
                    parallel_tool_calls=False,
                    max_tokens=8192
                )
                return resp
            except Exception as e:
                err_str = str(e)
                if "RateLimit" in err_str or "429" in err_str or "quota" in err_str.lower() or "overloaded" in err_str.lower() or "NotFoundError" in err_str or "404" in err_str or "APIError" in err_str or "APIConnectionError" in err_str or "502" in err_str or "503" in err_str:
                    # We fail FAST on hard limits so the Go orchestrator can catch it and route to a new model
                    print(f"[LLM] Hard limit reached on {{model}}: {{err_str[:150]}}", file=sys.stderr)
                    sys.exit(1)
                else:
                    print(f"[LLM] Error: {{err_str[:300]}}{{'...' if len(err_str) > 300 else ''}}", file=sys.stderr)
                raise e
            
        # Build clean context from upstream inputs
        upstream_context = ""
        for inp in req.get("inputs", []):
            inp_name = inp.get("name", "Unknown")
            inp_data = inp.get("data", "")
            if inp_data:
                upstream_context += f"### From {{inp_name}}:\\n{{str(inp_data)[:3000]}}\\n\\n"
        
        user_prompt = mem.get("user_prompt", "Complete your assigned task.")
        ide_context = mem.get("ide_context", "")
        prompt_history = mem.get("prompt_history", "")
        
        user_msg = ""
        if ide_context:
            user_msg += f"## IDE Context\\nThe user currently has the following workspace context. Use this to infer what they are referring to (e.g., if they say 'this file' or 'this function'):\\n{{ide_context}}\\n\\n"
        
        if prompt_history:
            user_msg += f"## Previous Iterations History\\nThis task is a continuation of previous work. Here is the history of previous prompts in this group:\\n{{prompt_history}}\\n\\n"
            
        user_msg += "## User's Goal\\n" + user_prompt + "\\n"
        if upstream_context:
            user_msg += "\\n## Context From Previous Agents\\n" + upstream_context
            
        def build_tree(dir_path, prefix=""):
            if not os.path.exists(dir_path):
                return ""
            tree_str = ""
            try:
                entries = sorted(os.listdir(dir_path))
                for i, entry in enumerate(entries):
                    if entry.startswith(".") and entry != ".env": continue
                    path = os.path.join(dir_path, entry)
                    is_last = (i == len(entries) - 1)
                    connector = "└── " if is_last else "├── "
                    tree_str += f"{prefix}{connector}{entry}\\n"
                    if os.path.isdir(path):
                        extension = "    " if is_last else "│   "
                        tree_str += build_tree(path, prefix=prefix + extension)
            except Exception as e:
                pass
            return tree_str
            
        tree_txt = build_tree(os.path.join(workspace_dir, "src"))
        if tree_txt.strip():
            user_msg += "\\n## Workspace State\\nThe `src/` directory is automatically managed for you. Do not worry about creating it. Here is the current file tree of `src/`:\\n" + tree_txt + "\\n"
        else:
            user_msg += "\\n## Workspace State\\nThe `src/` directory is automatically managed for you. Do not worry about creating it. It is currently empty.\\n"
            
        user_msg += "\\n## Your Instructions\\nYou MUST use the `write_file` tool to save your work. You must maintain a standard project directory structure. Place source code inside the `src/` directory (e.g. `src/main.py`, `src/utils.py`) and top-level configs in the root. Use `read_file` to inspect existing files before modifying them. If you need to test your code using external libraries, you MUST run 'uv pip install --system <library>' using the 'execute_terminal_command' tool BEFORE running your script! Do not assume third-party packages are pre-installed."
        
        messages = [
            {{"role": "system", "content": {json.dumps(sys_prompt)}}},
            {{"role": "user", "content": user_msg}}
        ]
        
        files_modified = {{}}
        memory_mutations = []
        graph_mutation = None
        task_completed = False
        MAX_ITERATIONS = 50
        for _iteration in range(MAX_ITERATIONS):
            response = do_completion(messages)
            msg = response.choices[0].message
            
            clean_msg = {{"role": "assistant", "content": msg.content or ""}}
            if msg.tool_calls:
                clean_msg["tool_calls"] = []
                for tc in msg.tool_calls:
                    clean_msg["tool_calls"].append({{
                        "id": tc.id,
                        "type": "function",
                        "function": {{
                            "name": tc.function.name,
                            "arguments": tc.function.arguments
                        }}
                    }})
            messages.append(clean_msg)
            
            # Streaming LLM content to stderr for UI
            if msg.content:
                for line in msg.content.split('\\n'):
                    print(f"[LLM] {{line}}", file=sys.stderr)
            
            if msg.tool_calls:
                for tool_call in msg.tool_calls:
                    func_name = tool_call.function.name
                    print(f"[TOOL] Executing {{func_name}}", file=sys.stderr)
                    args_str = tool_call.function.arguments
                    if len(args_str) > 150:
                        args_str = args_str[:150] + "... [TRUNCATED]"
                    print(f"[TOOL] Args: {{args_str}}", file=sys.stderr)
                    try:
                        args = json.loads(tool_call.function.arguments)
                        
                        if func_name == "execute_terminal_command":
                            res = execute_terminal_command(args.get("command"), workspace_dir, allow_native_execution)
                        elif func_name == "read_file":
                            res = read_file(args.get("path"), workspace_dir)
                        elif func_name == "search_codebase":
                            res = search_codebase(args.get("regex_pattern"), workspace_dir)
                        elif func_name == "write_file":
                            res = write_file(args.get("path"), args.get("content"), workspace_dir, files_modified)
                            if "Successfully" in res:
                                files_modified[args.get("path")] = args.get("content")
                        elif func_name == "list_dir":
                            res = list_dir(args.get("path"), workspace_dir)
                        elif func_name == "replace_file_content":
                            res = replace_file_content(args.get("path"), args.get("target_content"), args.get("replacement_content"), workspace_dir)
                            if "Successfully" in res:
                                full_path = os.path.join(workspace_dir, "src", args.get("path"))
                                with open(full_path, "r", encoding="utf-8") as f:
                                    files_modified[args.get("path")] = f.read()
                        elif func_name == "read_url":
                            res = read_url(args.get("url"), workspace_dir)
                        elif func_name == "write_memory":
                            scope = args.get("scope", "execution")
                            memory_mutations.append({{
                                "key": args.get("key"),
                                "value": args.get("value"),
                                "scope": scope
                            }})
                            mem[args.get("key")] = args.get("value")
                            res = f"Successfully wrote {{args.get('key')}} to {{scope}} memory."
                        elif func_name == "read_memory":
                            key = args.get("key")
                            if key in mem:
                                res = mem[key]
                            else:
                                res = f"Key '{{key}}' not found in injected shared memory."
                        elif func_name == "mutate_graph":
                            graph_mutation = {{
                                "action": args.get("action"),
                                "target_agent": args.get("target_agent"),
                                "return_to_supervisor": args.get("return_to_supervisor", False)
                            }}
                            res = f"Graph mutation registered. Control will transfer to {{args.get('target_agent')}} upon exit."
                        elif func_name == "mark_task_complete":
                            task_completed = True
                            summary = args.get("summary", "")
                            
                            # Fake the tool response and the final assistant response
                            messages.append({{
                                "role": "tool",
                                "name": func_name,
                                "tool_call_id": tool_call.id,
                                "content": "Task complete."
                            }})
                            messages.append({{
                                "role": "assistant",
                                "content": f"### Verification Summary\\n{{summary}}"
                            }})
                            print(f"[TOOL_RES] {{func_name}}: Task complete.", file=sys.stderr)
                            break # Break the inner loop
                        else:
                            res = "Unknown tool."
                    except json.JSONDecodeError as e:
                        res = f"Error decoding JSON tool arguments. Please ensure you output valid JSON. Exception: {{e}}"
                    except Exception as e:
                        res = f"Tool execution failed: {{e}}"
                        
                    res_str = str(res)
                    if len(res_str) > 300:
                        res_str = res_str[:300] + "... [TRUNCATED]"
                    print(f"[TOOL_RES] {{func_name}}: {{res_str}}", file=sys.stderr)
                    messages.append({{
                        "role": "tool",
                        "name": func_name,
                        "tool_call_id": tool_call.id,
                        "content": str(res)
                    }})
                    
                if task_completed:
                    break
            else:
                if msg.content and (":function=" in msg.content or "<function=" in msg.content):
                    messages.append({{
                        "role": "user",
                        "content": "ERROR: You attempted to call a tool by outputting raw text. You MUST use the native JSON tool calling API. Do not output raw function strings."
                    }})
                    continue
                else:
                    # Agent stopped calling tools without marking task complete
                    messages.append({{
                        "role": "user",
                        "content": "ERROR: You stopped calling tools without calling 'mark_task_complete'. If you are finished, you MUST call 'mark_task_complete'. If you are not finished, continue using tools to write code."
                    }})
                    continue
                
        if not task_completed:
            raise Exception("Agent failed to complete the task within the maximum number of iterations.")
            
        result = messages[-1].get("content", "")
        if files_modified:
            result += "\\n\\n### Files Modified By This Agent:\\n"
            for p, c in files_modified.items():
                result += f"#### {{p}}\\n```\\n{{c}}\\n```\\n"
        
        artifact = {{
            "id": f"{{req_id}}_output",
            "name": f"{agent_id} Output",
            "type": "document/markdown",
            "data": result
        }}
        
        resp_obj = {{
            "id": req_id,
            "artifact": artifact
        }}
        if memory_mutations:
            resp_obj["memory"] = memory_mutations
        if graph_mutation:
            resp_obj["graph_mutation"] = graph_mutation
            
        real_stdout.write(json.dumps(resp_obj) + "\\n")
    except Exception as e:
        print(f"ERROR: {{e}}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
"""
        generated_files[f"agents/{agent_id}/workers/{agent_id}.py"] = code.strip()
        generated_files[f"agents/{agent_id}/workers/forge_utils.py"] = utils_code.strip()

    import os
    mem = req.get("memory", {})
    workspace_dir = mem.get("workspace_dir", ".")
    
    os.makedirs(os.path.join(workspace_dir, "src"), exist_ok=True)
    for path, content in generated_files.items():
        full_path = os.path.join(workspace_dir, path)
        os.makedirs(os.path.dirname(full_path), exist_ok=True)
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)
            
    artifact = {
        "id": f"{req_id}_output",
        "name": "Python Files",
        "type": "application/json",
        "data": "{}"
    }
    
    print(json.dumps({
        "id": req_id,
        "artifact": artifact
    }))

if __name__ == "__main__":
    main()
