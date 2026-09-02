import os

base_dir = r'd:\Reticle\cmd\forge\compiler\agents'
coder_path = os.path.join(base_dir, 'coder-agent', 'workers', 'coder.py')
browser_dir = os.path.join(base_dir, 'browser-agent', 'workers')
os.makedirs(browser_dir, exist_ok=True)

with open(coder_path, 'r', encoding='utf-8') as f:
    content = f.read()

sys_prompt_old = '''sys_prompt += """\n\nYou are an autonomous agent equipped with tools. You must use the tools to read the workspace, execute tests, and modify files.
You have full root access to a Debian terminal via `execute_terminal_command`. You can test your work by running standard compilation or execution commands for your assigned language (e.g. `node src/index.js`, `python src/main.py`, `go build`). Use `read_url` to look up documentation if you are stuck. Use `list_dir` to explore the workspace instead of guessing file paths.'''

sys_prompt_new = '''sys_prompt += """\n\nYou are a Browser Automation Expert equipped with tools.
You are tasked with navigating complex websites, extracting DOM elements, and automating UI interactions.
You have full root access to a Debian terminal via `execute_terminal_command`. 
CRITICAL REQUIREMENT: You MUST use Playwright for Python to accomplish your tasks. 
Before writing and running your python scraping scripts, you MUST install playwright and its browsers natively by running `uv pip install playwright && playwright install` via `execute_terminal_command`.
Use `write_file` to write your playwright scripts into the workspace, and `execute_terminal_command` to run them and scrape the data.'''

content = content.replace(sys_prompt_old, sys_prompt_new)

with open(os.path.join(browser_dir, 'browser.py'), 'w', encoding='utf-8') as f:
    f.write(content)

print("Created browser.py successfully")
