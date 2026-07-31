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
        inputs = req.get("inputs", [])
        payload = inputs[0].get("data", "") if inputs else ""

        improved = payload.replace("Project Title", "HyperParallel Framework")
        improved = improved.replace("Getting Started", "Quick Start Guide")
        
        # Build the DAG Artifact with extended metadata
        artifact = {
            "id": "readme_final",
            "name": "Final README",
            "type": "document/markdown",
            "producer": "wording-imp",
            "workflow": req.get("workflow", ""),
            "execution": req.get("execution", ""),
            "task": req_id,
            "parents": ["readme_outline"], # DAG Link!
            "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "version": 1,
            "data": improved
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
