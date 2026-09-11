import os

utils_code_str = '''utils_code = """import os, subprocess, re

def execute_terminal_command(command, workspace_dir):
    try:
        docker_cmd = [
            "docker", "run", "--rm", 
            "-v", f"{os.path.abspath(workspace_dir)}:/workspace", 
            "-w", "/workspace/src", 
            "python:3.10-slim", 
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

def write_file(path, content, workspace_dir):
    full_path = os.path.join(workspace_dir, "src", path)
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    try:
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)
        return f"Successfully wrote to {path}"
    except Exception as e:
        return f"Error writing file: {e}"

tools = [
    {
        "type": "function",
        "function": {
            "name": "execute_terminal_command",
            "description": "Execute a bash command in a secure Docker container inside the workspace/src directory.",
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {"type": "string", "description": "The bash command to execute (e.g. 'pytest', 'python main.py', 'ls -la')"}
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
    }
]
"""
'''

agent_template = '''        sys_prompt = agent.get("system_prompt", "You are a helpful assistant.")
        sys_prompt += """\\n\\nYou are an autonomous agent equipped with tools. You must use the tools to read the workspace, execute tests, and modify files.
When you are completely finished with your task, you must output a final summary. DO NOT output code blocks like ```json file_operations``` anymore, you must use your write_file tool to apply changes!"""
        
        code = f"""import sys, json, time, logging, os

real_stdout = sys.stdout
sys.stdout = sys.stderr

import litellm
litellm.suppress_debug_info = True

from litellm import completion
from tenacity import retry, stop_after_attempt, wait_exponential, before_sleep_log

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

from utils import execute_terminal_command, read_file, search_codebase, write_file, tools

def main():
    line = sys.stdin.readline()
    if not line: return
    try:
        req = json.loads(line)
        req_id = req.get("id")
        
        mem = req.get("memory", {{}})
        workspace_dir = mem.get("workspace_dir", ".")
        src_dir = os.path.join(workspace_dir, "src")
        
        endpoints = [
            {{"model": os.environ.get("RETICLE_GROQ_MODEL", "groq/allam-2-7b"), "api_key": os.environ.get("GROQ_API_KEY", "")}},
            {{"model": os.environ.get("RETICLE_GROQ_MODEL", "groq/allam-2-7b"), "api_key": os.environ.get("GROQ_API_KEY_2", "")}},
            {{"model": os.environ.get("RETICLE_OPENROUTER_MODEL", ""), "api_key": os.environ.get("OPENROUTER_API_KEY", "")}},
            {{"model": os.environ.get("RETICLE_OPENROUTER_MODEL", ""), "api_key": os.environ.get("OPENROUTER_API_KEY_2", "")}}
        ]
        
        @retry(stop=stop_after_attempt(10), wait=wait_exponential(multiplier=2, min=4, max=60), before_sleep=before_sleep_log(logger, logging.WARNING))
        def do_completion(messages):
            import random
            random.shuffle(endpoints)
            last_err = None
            for ep in endpoints:
                try:
                    resp = completion(
                        model=ep["model"],
                        api_key=ep["api_key"],
                        messages=messages,
                        tools=tools
                    )
                    return resp
                except Exception as e:
                    last_err = e
                    logger.warning(f"Failed with {{ep['model']}}: {{e}}")
            raise last_err
            
        messages = [
            {{"role": "system", "content": {json.dumps(sys_prompt)}}},
            {{"role": "user", "content": f"Process this request: {{json.dumps(req)}}. The workspace is located at {{src_dir}}. Use your tools to inspect and modify it."}}
        ]
        
        while True:
            response = do_completion(messages)
            msg = response.choices[0].message
            messages.append(msg.model_dump())
            
            if msg.tool_calls:
                for tool_call in msg.tool_calls:
                    func_name = tool_call.function.name
                    args = json.loads(tool_call.function.arguments)
                    
                    if func_name == "execute_terminal_command":
                        res = execute_terminal_command(args.get("command"), workspace_dir)
                    elif func_name == "read_file":
                        res = read_file(args.get("path"), workspace_dir)
                    elif func_name == "search_codebase":
                        res = search_codebase(args.get("regex_pattern"), workspace_dir)
                    elif func_name == "write_file":
                        res = write_file(args.get("path"), args.get("content"), workspace_dir)
                    else:
                        res = "Unknown tool."
                        
                    messages.append({{
                        "role": "tool",
                        "name": func_name,
                        "tool_call_id": tool_call.id,
                        "content": str(res)
                    }})
            else:
                break
                
        result = messages[-1].get("content", "")
        
        artifact = {{
            "id": f"{{req_id}}_output",
            "name": "{agent_id} Output",
            "type": "document/markdown",
            "data": result
        }}
        real_stdout.write(json.dumps({{"id": req_id, "artifact": artifact}}) + "\\n")
    except Exception as e:
        print(f"ERROR: {{e}}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
"""
        generated_files[f"workers/{agent_id}.py"] = code.strip()
        generated_files["workers/utils.py"] = utils_code.strip()
'''

with open(r"d:\Reticle\cmd\forge\compiler\workers\coder.py", "r", encoding="utf-8") as f:
    orig = f.read()

import re

# find the loop
start_idx = orig.find('sys_prompt = agent.get("system_prompt", "You are a helpful assistant.")')
end_idx = orig.find('import os\n    mem = req.get("memory", {})')

if start_idx == -1 or end_idx == -1:
    print("Could not find boundaries!")
else:
    new_content = orig[:start_idx] + agent_template + "\n    " + orig[end_idx:]
    
    # insert utils_code at top
    new_content = new_content.replace('import json\n\ndef main():', f'import json\n\n{utils_code_str}\n\ndef main():')

    with open(r"d:\Reticle\cmd\forge\compiler\workers\coder.py", "w", encoding="utf-8") as f:
        f.write(new_content)
    print("Successfully patched coder.py")
