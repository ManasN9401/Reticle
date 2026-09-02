import re

with open(r"d:\Reticle\cmd\forge\compiler\workers\scaffolder.py", "r", encoding="utf-8") as f:
    content = f.read()

# 1. Remove file-writer-node from workflow_yaml_str
content = content.replace('''    workflow_yaml_str += """
  - id: file-writer-node
    agent: file-writer
    parameters: {}"""
''', '')

# 2. Remove edges to file-writer-node
content = re.sub(r'    for node in dag\.get\("nodes", \[\]\):\n        workflow_yaml_str \+= f"""\n  - from: \{node\.get\(\'id\'\)\}\n    to: file-writer-node"""\n', '', content)

# 3. Remove generated_files["agents/file-writer.yaml"]
file_writer_yaml = '''    generated_files["agents/file-writer.yaml"] = """id: file-writer
name: "File Writer"
description: "Writes extracted code to disk."
version: 1.0.0
runtime: python
entrypoint: workers/file-writer.py
memory:
  - "workspace_dir"
"""'''
content = content.replace(file_writer_yaml, '')

# 4. Remove generated_files["workers/file-writer.py"]
# We will just split and cut out from `    generated_files["workers/file-writer.py"] = ` to `    import os`
start_idx = content.find('    generated_files["workers/file-writer.py"] = """')
if start_idx != -1:
    end_idx = content.find('"""\n\n    import os', start_idx)
    if end_idx != -1:
        content = content[:start_idx] + content[end_idx+4:]

with open(r"d:\Reticle\cmd\forge\compiler\workers\scaffolder.py", "w", encoding="utf-8") as f:
    f.write(content)
print("Successfully patched scaffolder.py")
