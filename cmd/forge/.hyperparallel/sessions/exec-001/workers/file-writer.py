import sys, json, os, re, logging

logging.basicConfig(level=logging.ERROR)
logger = logging.getLogger(__name__)

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id", "unknown")
    exec_id = req.get("execution", "unknown")
    inputs = req.get("inputs", [])
    mem = req.get("memory", {})
    workspace_dir = mem.get("workspace_dir", f"./.hyperparallel/sessions/{exec_id}")
    src_dir = os.path.join(workspace_dir, "src")
    os.makedirs(src_dir, exist_ok=True)
    
    operations_executed = 0
    errors = []
    success_log = []
    
    for inp in inputs:
        data = inp.get("data", "")
        # Extract ```json blocks
        matches = re.finditer(r'```json\s*(.*?)\s*```', data, re.DOTALL)
        for match in matches:
            try:
                block = json.loads(match.group(1))
                ops = block.get("file_operations", [])
                for op in ops:
                    action = op.get("action")
                    path = op.get("path")
                    clean_path = os.path.normpath(path).lstrip(os.sep)
                    if clean_path.startswith("src" + os.sep): clean_path = clean_path[4:]
                    if ".." in clean_path:
                        errors.append(f"Invalid path: {path}")
                        continue
                        
                    full_path = os.path.join(src_dir, clean_path)
                    os.makedirs(os.path.dirname(full_path), exist_ok=True)
                    
                    if action == "create":
                        content = op.get("content", "")
                        with open(full_path, "w", encoding="utf-8") as f:
                            f.write(content)
                        operations_executed += 1
                        success_log.append(f"Created: {clean_path}")
                    elif action == "edit":
                        search = op.get("search", "")
                        replace = op.get("replace", "")
                        if not os.path.exists(full_path):
                            errors.append(f"Cannot edit non-existent file: {path}")
                            continue
                        with open(full_path, "r", encoding="utf-8") as f:
                            current_content = f.read()
                        if search not in current_content:
                            errors.append(f"Search string not found in {path}")
                            continue
                        new_content = current_content.replace(search, replace, 1)
                        with open(full_path, "w", encoding="utf-8") as f:
                            f.write(new_content)
                        operations_executed += 1
                        success_log.append(f"Edited: {clean_path}")
            except Exception as e:
                pass
                
        # Extract markdown files
        file_matches = re.finditer(r'### FILE:\s*([^\n]+)\n```[a-zA-Z]*\n(.*?)```', data, re.DOTALL)
        for match in file_matches:
            try:
                path = match.group(1).strip()
                content = match.group(2)
                
                clean_path = os.path.normpath(path).lstrip(os.sep)
                if clean_path.startswith("src" + os.sep): clean_path = clean_path[4:]
                if ".." in clean_path:
                    errors.append(f"Invalid path: {path}")
                    continue
                    
                full_path = os.path.join(src_dir, clean_path)
                os.makedirs(os.path.dirname(full_path), exist_ok=True)
                
                with open(full_path, "w", encoding="utf-8") as f:
                    f.write(content)
                operations_executed += 1
                success_log.append(f"Created: {clean_path}")
            except Exception as e:
                pass
                
    result_text = f"Executed {operations_executed} file operations in src/:"
    if success_log:
        result_text += "\n" + "\n".join(success_log)
    if errors:
        result_text += f"\n\nErrors ({len(errors)}):\n" + "\n".join(errors)
    
    artifact = {
        "id": f"{req_id}_output",
        "name": "File Writer Output",
        "type": "text/plain",
        "data": result_text
    }
    print(json.dumps({"id": req_id, "artifact": artifact}))

if __name__ == "__main__":
    main()
