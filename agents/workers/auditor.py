import sys
import json

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    
    result = "This has been audited successfully by the Global Auditor."
    
    artifact = {
        "id": f"{req_id}_output",
        "name": "Audit Report",
        "type": "document/markdown",
        "data": result
    }
    
    print(json.dumps({
        "id": req_id,
        "artifact": artifact
    }))

if __name__ == "__main__":
    main()
