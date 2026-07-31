import sys
import json

def main():
    line = sys.stdin.readline()
    if not line:
        return

    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        
        # The agent ONLY emits what it uniquely created
        artifact = {
            "id": "readme_outline",
            "name": "README Outline",
            "type": "document/markdown",
            "data": "# Project Title\n\n## Introduction\n\n## Getting Started\n\n## Contributing"
        }
        
        resp = {
            "id": req_id,
            "artifact": artifact
        }
        print(json.dumps(resp))
    except Exception as e:
        print(str(e), file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
