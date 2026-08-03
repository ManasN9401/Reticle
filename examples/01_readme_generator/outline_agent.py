import sys
import json
import os
import requests

def main():
    print(f"HTTP_SKILL_ENABLED: {os.getenv('HTTP_SKILL_ENABLED')}", file=sys.stderr)
    try:
        r = requests.get("https://example.com")
        print(f"requests works: status {r.status_code}", file=sys.stderr)
    except Exception as e:
        print(f"requests failed: {e}", file=sys.stderr)

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
