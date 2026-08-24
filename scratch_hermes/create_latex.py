import os

base_dir = r'd:\HyperParallel\cmd\forge\compiler\agents'
coder_path = os.path.join(base_dir, 'coder-agent', 'workers', 'coder.py')
latex_dir = os.path.join(base_dir, 'latex-agent', 'workers')
os.makedirs(latex_dir, exist_ok=True)

with open(coder_path, 'r', encoding='utf-8') as f:
    content = f.read()

sys_prompt_old = '''sys_prompt += """\n\nYou are an autonomous agent equipped with tools. You must use the tools to read the workspace, execute tests, and modify files.
You have full root access to a Debian terminal via `execute_terminal_command`. You can test your work by running standard compilation or execution commands for your assigned language (e.g. `node src/index.js`, `python src/main.py`, `go build`). Use `read_url` to look up documentation if you are stuck. Use `list_dir` to explore the workspace instead of guessing file paths.'''

sys_prompt_new = '''sys_prompt += """\n\nYou are a Professional LaTeX Typesetter equipped with tools.
You must use your tools to compile raw markdown chapters into a beautifully formatted, formal academic book in LaTeX.
You have full root access to a Debian terminal via `execute_terminal_command`. You MUST use standard LaTeX distribution tools like `pdflatex` to compile your `.tex` files. If you need LaTeX packages, you can install them natively on the Debian image (e.g., `apt-get update && apt-get install -y texlive-latex-extra texlive-pictures`).
You should use advanced TikZ diagrams to visually represent statistics concepts provided in the chapters.
Your final output MUST be a compiled `.pdf` file. Use the workspace to store your source files and build artifacts.'''

content = content.replace(sys_prompt_old, sys_prompt_new)

with open(os.path.join(latex_dir, 'latex.py'), 'w', encoding='utf-8') as f:
    f.write(content)

print("Created latex.py successfully")
