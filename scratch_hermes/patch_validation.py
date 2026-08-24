import os
import glob

base_dir = r'd:\HyperParallel\cmd\forge\compiler\agents'
worker_files = glob.glob(os.path.join(base_dir, '*', 'workers', '*.py'))

validator_code = """                        args = json.loads(tool_call.function.arguments)
                        
                        # Strict Schema Validation
                        tool_schema = next((t["function"]["parameters"] for t in tools if t["function"]["name"] == func_name), None)
                        if tool_schema and "required" in tool_schema:
                            missing = [req for req in tool_schema["required"] if req not in args]
                            if missing:
                                raise ValueError(f"Schema Validation Failed: Missing required arguments: {missing}. Please fix and try again.")
"""

for path in worker_files:
    if "forge_utils.py" in path:
        continue
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    if "Schema Validation Failed" not in content:
        content = content.replace('                        args = json.loads(tool_call.function.arguments)\n', validator_code)
        
        with open(path, 'w', encoding='utf-8') as f:
            f.write(content)

print("Injected schema validation into all worker files successfully")
