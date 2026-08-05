import sys
import json

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    
    # Read memory injected by Orchestrator
    mem = req.get("memory", {})
    secret = mem.get("secret_code", "UNKNOWN")
    
    print(f"[{req_id}] Reader Agent: I found the secret code: {secret}", file=sys.stderr)
    
    resp = {
        "id": req_id,
        "artifact": {
            "id": f"{req_id}_output",
            "name": "Reader Output",
            "data": f"The secret was: {secret}"
        }
    }
    print(json.dumps(resp))

if __name__ == "__main__":
    main()
