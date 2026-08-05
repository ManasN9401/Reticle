import sys
import json

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    
    print(f"[{req_id}] Writer Agent: I am writing to memory...", file=sys.stderr)
    
    resp = {
        "id": req_id,
        "memory": [
            {
                "key": "secret_code",
                "value": "42-OMEGA",
                "scope": "execution"
            }
        ],
        "artifact": {
            "id": f"{req_id}_output",
            "name": "Writer Output",
            "data": "I wrote to memory."
        }
    }
    print(json.dumps(resp))

if __name__ == "__main__":
    main()
