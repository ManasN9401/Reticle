"""
Reticle Worker Script
"""
import sys
import json
import os

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    inputs = req.get("inputs", [])
    mem = req.get("memory", {})
    
    workspace_dir = mem.get("workspace_dir", "./workspaces/default")
    
    # Ensure workspace exists
    os.makedirs(os.path.join(workspace_dir, "workflows"), exist_ok=True)
    os.makedirs(os.path.join(workspace_dir, "agents"), exist_ok=True)
    os.makedirs(os.path.join(workspace_dir, "workers"), exist_ok=True)
    
    all_files = {}
    for i in inputs:
        if i.get("name") in ["YAML Files", "Python Files"]:
            files = json.loads(i.get("data", "{}"))
            all_files.update(files)
            
    print(f"[{req_id}] Writing {len(all_files)} files to {workspace_dir}...", file=sys.stderr)
    
    for filepath, content in all_files.items():
        full_path = os.path.join(workspace_dir, filepath)
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)
            
    artifact = {
        "id": f"{req_id}_output",
        "name": "Write Status",
        "type": "text/plain",
        "data": f"Successfully wrote {len(all_files)} files to {workspace_dir}."
    }
    
    print(json.dumps({
        "id": req_id,
        "artifact": artifact
    }))

if __name__ == "__main__":
    main()
