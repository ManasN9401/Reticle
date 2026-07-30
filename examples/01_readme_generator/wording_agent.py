import sys
import json

def main():
    line = sys.stdin.readline()
    if not line:
        return

    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        payload = req.get("payload", "")
        
        improved = payload.replace("Project Title", "HyperParallel Framework")
        improved = improved.replace("Getting Started", "Quick Start Guide")
        
        resp = {
            "id": req_id,
            "result": improved
        }
        
        print(json.dumps(resp))
        sys.stdout.flush()
    except Exception as e:
        print(json.dumps({"id": "unknown", "error": str(e)}))
        sys.stdout.flush()

if __name__ == "__main__":
    main()
