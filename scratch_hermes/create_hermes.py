import os

base_dir = r'd:\HyperParallel\cmd\forge\compiler\agents'
coder_path = os.path.join(base_dir, 'coder-agent', 'workers', 'coder.py')
hermes_dir = os.path.join(base_dir, 'hermes-coder-agent', 'workers')
os.makedirs(hermes_dir, exist_ok=True)

with open(coder_path, 'r', encoding='utf-8') as f:
    content = f.read()

old_sys_prompt = '''sys_prompt += """\n\nYou are an autonomous agent equipped with tools. You must use the tools to read the workspace, execute tests, and modify files.
You have full root access to a Debian terminal via `execute_terminal_command`. You can test your work by running standard compilation or execution commands for your assigned language (e.g. `node src/index.js`, `python src/main.py`, `go build`). Use `read_url` to look up documentation if you are stuck. Use `list_dir` to explore the workspace instead of guessing file paths.
CRITICAL REQUIREMENT: You MUST write FULLY FUNCTIONAL, complete code. You are strictly FORBIDDEN from using placeholders like `pass`, `TODO`, or `...`. Every function must have real implementation logic!
CRITICAL BOUNDARY RULE: Do NOT rewrite or overwrite files owned by other components from scratch. If a file exists, use `read_file` to inspect it and ONLY use `replace_file_content` to surgically inject your specific feature. Overwriting existing files with `write_file` will destroy other agents' work and is STRICTLY FORBIDDEN unless you are the original creator of that file.
When you are finished and have VERIFIED your code works via execute_terminal_command, call the `mark_task_complete` tool with a summary of what you did."""'''

new_sys_prompt = '''sys_prompt += """\n\nYou are an autonomous agent equipped with tools and self-recursion.
You must use the tools to read the workspace, execute tests, and modify files.
You have full root access to a Debian terminal via `execute_terminal_command`.

CRITICAL INSTRUCTION (HERMES REPL):
You MUST act as a Read-Eval-Print Loop (REPL). Do NOT stop calling functions until the task has been fully accomplished and verified.
At each turn, keep a running summary with an analysis of previous function results.
If you plan to continue with analysis or write more code, ALWAYS call another function.
You must test your work by running standard compilation or execution commands for your assigned language.

CRITICAL REQUIREMENT: You MUST write FULLY FUNCTIONAL, complete code. You are strictly FORBIDDEN from using placeholders like `pass`, `TODO`, or `...`.
CRITICAL BOUNDARY RULE: Do NOT rewrite files owned by other components from scratch. If a file exists, use `read_file` to inspect it and ONLY use `replace_file_content`. Overwriting existing files with `write_file` will destroy other agents' work and is STRICTLY FORBIDDEN unless you created the file.
When you are absolutely finished and have VERIFIED your code works via execute_terminal_command, call the `mark_task_complete` tool with a summary."""'''

content = content.replace(old_sys_prompt, new_sys_prompt)

with open(os.path.join(hermes_dir, 'hermes.py'), 'w', encoding='utf-8') as f:
    f.write(content)

print("Created hermes.py successfully")
