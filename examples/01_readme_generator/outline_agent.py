import sys
import json

def main():
    line = sys.stdin.readline()
    if not line:
        return

    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        
        result = "# Project Title\n\n## Introduction\n\n## Getting Started\n\n## Contributing"
        
        resp = {
            "id": req_id,
            "result": result
        }
        
        print(json.dumps(resp))
        sys.stdout.flush()
    except Exception as e:
        print(json.dumps({"id": "unknown", "error": str(e)}))
        sys.stdout.flush()

if __name__ == "__main__":
    main()
