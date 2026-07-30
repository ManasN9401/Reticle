import sys
import json
import time

def main():
    line = sys.stdin.readline()
    if not line:
        return

    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        
        # Build the DAG Artifact
        artifact = {
            "id": "readme_outline",
            "name": "README Outline",
            "type": "document/markdown",
            "producer": "outline-gen",
            "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "version": 1,
            "data": "# Project Title\n\n## Introduction\n\n## Getting Started\n\n## Contributing"
        }
        
        resp = {
            "id": req_id,
            "artifact": artifact
        }
        print(json.dumps(resp))
    except Exception as e:
        print(json.dumps({"id": "unknown", "error": str(e)}))

if __name__ == "__main__":
    main()
