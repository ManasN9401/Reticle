"""Compatibility compiler writer with workspace containment and no overwrites."""
import json
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "lib"))
from forge_utils import safe_path, MAX_FILE

def main():
    req = json.load(sys.stdin)
    workspace = req["memory"]["workspace_dir"]
    files = {}
    for artifact in req.get("inputs", []):
        if artifact.get("name") in ("YAML Files", "Python Files"):
            value = artifact.get("data", {})
            files.update(json.loads(value) if isinstance(value, str) else value)
    targets = []
    for name, content in files.items():
        if not isinstance(content, str) or len(content.encode()) > MAX_FILE:
            raise ValueError("Invalid generated text")
        target = safe_path(workspace, name, base="")
        if target.exists():
            raise ValueError("Refusing to overwrite existing generated file")
        targets.append((target, content))
    for target, content in targets:
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("x", encoding="utf-8", newline="") as stream:
            stream.write(content)
    print(json.dumps({"id":req["id"],"artifact":{"id":req["id"]+"_output","name":"Write Status","type":"text/plain","data":f"Wrote {len(targets)} generated files"}}))

if __name__ == "__main__":
    main()
