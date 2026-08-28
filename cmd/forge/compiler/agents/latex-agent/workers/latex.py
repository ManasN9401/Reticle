import sys, json, time, logging, os, re
import litellm
litellm.suppress_debug_info = True
from litellm import completion
from tenacity import retry, stop_after_attempt, wait_exponential

logging.basicConfig(level=logging.CRITICAL)
logger = logging.getLogger(__name__)

# Re-use forge_utils (imported dynamically or embedded). We'll embed it for simplicity.
import subprocess
def execute_terminal_command(command, workspace_dir, allow_native=False):
    try:
        src_dir = os.path.join(workspace_dir, "src")
        os.makedirs(src_dir, exist_ok=True)
        if allow_native:
            result = subprocess.run(command, shell=True, cwd=src_dir, capture_output=True, text=True, timeout=120)
        else:
            container_name = "forge_" + os.path.basename(os.path.abspath(workspace_dir)).replace(".", "_").replace("-", "_")
            check = subprocess.run(["docker", "ps", "-q", "-f", f"name=^{container_name}$"], capture_output=True, text=True)
            if not check.stdout.strip():
                subprocess.run([
                    "docker", "run", "-d", "--rm", "--name", container_name,
                    "-v", f"{os.path.abspath(workspace_dir)}:/workspace",
                    "-w", "/workspace",
                    "ghcr.io/astral-sh/uv:python3.12-bookworm-slim",
                    "sleep", "infinity"
                ], capture_output=True)
            docker_cmd = ["docker", "exec", container_name, "bash", "-c", command]
            result = subprocess.run(docker_cmd, capture_output=True, text=True, timeout=120)
        
        output = result.stdout + "\n" + result.stderr
        if result.returncode != 0:
            output = f"Command failed with exit code {result.returncode}:\n" + output
        return output[:10000]
    except Exception as e:
        return f"Error executing command: {e}"

def write_file(path, content, workspace_dir):
    full_path = os.path.join(workspace_dir, "src", path)
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    try:
        content = content.replace('\\n', '\n') if content else ""
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)
        return f"Successfully wrote to {path}"
    except Exception as e:
        return f"Error writing file: {e}"

def replace_file_content(path, target_content, replacement_content, workspace_dir):
    full_path = os.path.join(workspace_dir, "src", path)
    if not os.path.exists(full_path):
        return f"Error: File {path} not found."
    try:
        with open(full_path, "r", encoding="utf-8") as f:
            content = f.read()
        target_content = target_content.replace('\\n', '\n') if target_content else ""
        replacement_content = replacement_content.replace('\\n', '\n') if replacement_content else ""
        if target_content not in content:
            return "Error: target_content not found in file. Make sure the indentation and whitespace match exactly."
        content = content.replace(target_content, replacement_content, 1)
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)
        return f"Successfully replaced content in {path}"
    except Exception as e:
        return f"Error replacing content: {e}"

tools = [
    {
        "type": "function",
        "function": {
            "name": "execute_terminal_command",
            "description": "Execute a terminal bash command.",
            "parameters": {
                "type": "object",
                "properties": {
                    "command": {"type": "string"}
                },
                "required": ["command"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "write_file",
            "description": "Write a complete file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "content": {"type": "string"}
                },
                "required": ["path", "content"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "replace_file_content",
            "description": "Surgically replace text in a file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "target_content": {"type": "string"},
                    "replacement_content": {"type": "string"}
                },
                "required": ["path", "target_content", "replacement_content"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "mark_task_complete",
            "description": "Mark the LaTeX compilation as complete.",
            "parameters": {
                "type": "object",
                "properties": {
                    "summary": {"type": "string"}
                },
                "required": ["summary"]
            }
        }
    }
]

def main():
    real_stdout = sys.stdout
    sys.stdout = sys.stderr

    sys.stdin.reconfigure(encoding='utf-8')
    real_stdout.reconfigure(encoding='utf-8')
    sys.stderr.reconfigure(encoding='utf-8')

    line = sys.stdin.readline()
    if not line: return
    try:
        req = json.loads(line)
        req_id = req.get("id")
        
        mem = req.get("memory", {})
        workspace_dir = mem.get("workspace_dir", ".")
        allow_native = mem.get("allow_native_execution", "false").lower() == "true"
        
        model = req.get("parameters", {}).get("llm_model", "openrouter/anthropic/claude-3-5-sonnet-20241022")
        api_key_env = req.get("parameters", {}).get("api_key")
        api_key = os.environ.get(api_key_env) if api_key_env else None

        upstream_context = ""
        for inp in req.get("inputs", []):
            inp_name = inp.get("name", "Unknown")
            inp_data = inp.get("data", "")
            if inp_data:
                upstream_context += f"### From {inp_name}:\n{str(inp_data)[:15000]}\n\n"

        sys_prompt = """You are a LaTeX Typesetting Expert Agent. Your job is to convert the incoming Markdown chapters into a fully styled, beautifully formatted PDF book using LaTeX.
You have full access to a Debian terminal via `execute_terminal_command`.

INSTRUCTIONS:
1. Ensure Tectonic is installed. Run `curl -sL https://drop-sh.fullyjustified.net | sh && mv tectonic /usr/local/bin/` in your terminal.
2. Read the provided Markdown text from your context.
3. Use `write_file` to create a `book.tex` file. Use packages like `tcolorbox`, `tikz`, `pgfplots`, and `geometry`. Make it look professional!
4. Compile the book by running `tectonic book.tex`.
5. Fix any compilation errors you encounter by reading the logs, editing `book.tex`, and recompiling.
6. Once compiled successfully, call `mark_task_complete`."""

        user_msg = f"## Context From Previous Agents\n{upstream_context}\n\nPlease generate and compile the PDF."

        messages = [
            {"role": "system", "content": sys_prompt},
            {"role": "user", "content": user_msg}
        ]

        @retry(stop=stop_after_attempt(5), wait=wait_exponential(multiplier=2, min=5, max=120))
        def do_completion(messages):
            return completion(model=model, api_key=api_key, max_tokens=8000, messages=messages, tools=tools, parallel_tool_calls=False)

        task_completed = False
        for _ in range(30):
            response = do_completion(messages)
            msg = response.choices[0].message
            
            clean_msg = {"role": "assistant", "content": msg.content or ""}
            if msg.tool_calls:
                clean_msg["tool_calls"] = []
                for tc in msg.tool_calls:
                    clean_msg["tool_calls"].append({
                        "id": tc.id,
                        "type": "function",
                        "function": {"name": tc.function.name, "arguments": tc.function.arguments}
                    })
            messages.append(clean_msg)
            
            if msg.content:
                print(f"[LLM] {msg.content[:100]}...", file=sys.stderr)
                
            if msg.tool_calls:
                for tc in msg.tool_calls:
                    func_name = tc.function.name
                    print(f"[TOOL] Executing {func_name}", file=sys.stderr)
                    args = json.loads(tc.function.arguments)
                    
                    if func_name == "execute_terminal_command":
                        res = execute_terminal_command(args.get("command"), workspace_dir, allow_native)
                    elif func_name == "write_file":
                        res = write_file(args.get("path"), args.get("content"), workspace_dir)
                    elif func_name == "replace_file_content":
                        res = replace_file_content(args.get("path"), args.get("target_content"), args.get("replacement_content"), workspace_dir)
                    elif func_name == "mark_task_complete":
                        task_completed = True
                        res = "Task complete."
                        break
                    else:
                        res = "Unknown tool."
                    
                    print(f"[TOOL_RES] {res[:100]}...", file=sys.stderr)
                    messages.append({
                        "role": "tool",
                        "name": func_name,
                        "tool_call_id": tc.id,
                        "content": str(res)
                    })
                
                if task_completed:
                    break

        if not task_completed:
            raise Exception("Agent failed to complete task within iteration limit.")

        pdf_path = os.path.join(workspace_dir, "src", "book.pdf")
        has_pdf = os.path.exists(pdf_path)

        artifact = {
            "id": f"{req_id}_pdf",
            "name": "Compiled Book PDF",
            "type": "document/pdf" if has_pdf else "text/plain",
            "data": f"file://{pdf_path}" if has_pdf else "Compilation failed to produce PDF."
        }
        
        resp_obj = {
            "id": req_id,
            "artifact": artifact
        }
        
        real_stdout.write(json.dumps(resp_obj) + "\n")
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
