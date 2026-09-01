import os, subprocess, re

def execute_terminal_command(command, workspace_dir, allow_native=False):
    try:
        src_dir = os.path.join(workspace_dir, "src")
        if allow_native:
            # Run natively instead of Docker
            result = subprocess.run(command, shell=True, cwd=src_dir, capture_output=True, text=True, timeout=120)
            output = result.stdout + "\n" + result.stderr
            if result.returncode != 0:
                output = f"Command failed with exit code {result.returncode}:\n" + output
            return output[:10000]
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
                
            docker_cmd = [
                "docker", "exec", container_name,
                "bash", "-c", command
            ]
            result = subprocess.run(docker_cmd, capture_output=True, text=True, timeout=120)
            output = result.stdout + "\n" + result.stderr
            if result.returncode != 0:
                output = f"Command failed with exit code {result.returncode}:\n" + output
            return output[:10000]
    except Exception as e:
        return f"Error executing command: {e}"

def read_file(path, workspace_dir):
    clean_path = os.path.normpath(path).replace("\\", "/")
    if clean_path.startswith("skills/"):
        full_path = os.path.abspath(os.path.join(workspace_dir, "../../..", clean_path))
    else:
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
        return "\n".join(results[:500])
    except Exception as e:
        return f"Error searching codebase: {e}"

def write_file(path, content, workspace_dir, files_modified):
    full_path = os.path.join(workspace_dir, "src", path)
    
    if os.path.exists(full_path) and path not in files_modified:
        return f"ERROR: File {path} already exists! Overwriting is STRICTLY FORBIDDEN to prevent deleting other agents' work. You MUST use `read_file` to inspect the existing contents, and then use `replace_file_content` to make surgical additions."
        
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    try:
        content = content.replace('\\n', '\n') if content else ""
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
        return "\n".join(items) if items else "Directory is empty."
    except Exception as e:
        return f"Error listing directory: {e}"

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

def read_url(url, workspace_dir):
    try:
        import urllib.request
        import re
        req = urllib.request.Request(url, headers={'User-Agent': 'Mozilla/5.0'})
        with urllib.request.urlopen(req, timeout=10) as response:
            html = response.read().decode('utf-8', errors='ignore')
            text = re.sub('<[^<]+>', ' ', html)
            text = re.sub('\s+', ' ', text)
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
            "description": "Fetch and extract text content from a URL.",
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
            "name": "mark_task_complete",
            "description": "Call this tool to indicate you have fully completed and tested your assigned task.",
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
