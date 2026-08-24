import os

base_dir = r'd:\HyperParallel\cmd\forge\compiler\agents'
hermes_path = os.path.join(base_dir, 'hermes-coder-agent', 'workers', 'hermes.py')

with open(hermes_path, 'r', encoding='utf-8') as f:
    content = f.read()

interpreter_code = """
_persistent_namespace = {}

def code_interpreter(code_markdown, workspace_dir):
    try:
        if code_markdown.strip().startswith("```"):
            code_lines = code_markdown.strip().split('\\n')[1:-1]
            code = '\\n'.join(code_lines)
        else:
            code = code_markdown
            
        import sys, io
        old_stdout = sys.stdout
        sys.stdout = buffer = io.StringIO()
        
        exec(code, _persistent_namespace)
        
        sys.stdout = old_stdout
        return buffer.getvalue() or "Code executed successfully with no stdout."
    except Exception as e:
        sys.stdout = old_stdout
        return f"Error: {e}"
"""

interpreter_schema = """    {
        "type": "function",
        "function": {
            "name": "code_interpreter",
            "description": "Execute Python code directly in a persistent namespace (like a Jupyter Notebook) and return the stdout. You can use this to define variables, run math, or test logic incrementally without writing to files.",
            "parameters": {
                "type": "object",
                "properties": {
                    "code_markdown": {"type": "string", "description": "The python code to execute"}
                },
                "required": ["code_markdown"]
            }
        }
    },"""

content = content.replace('def read_url(url, workspace_dir):', interpreter_code + '\ndef read_url(url, workspace_dir):')
content = content.replace('tools = [', 'tools = [\n' + interpreter_schema)

dispatch_logic = """                        elif func_name == "code_interpreter":
                            res = code_interpreter(args.get("code_markdown"), workspace_dir)
                        elif func_name == "read_file":"""
content = content.replace('elif func_name == "read_file":', dispatch_logic)

imports_old = 'from forge_utils import execute_terminal_command, read_file'
imports_new = 'from forge_utils import execute_terminal_command, code_interpreter, read_file'
content = content.replace(imports_old, imports_new)

with open(hermes_path, 'w', encoding='utf-8') as f:
    f.write(content)

print("Patched hermes.py successfully")
