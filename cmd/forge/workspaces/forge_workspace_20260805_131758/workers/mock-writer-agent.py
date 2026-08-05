"""
HyperParallel Worker Script
"""
import sys
import json

def main():
    line = sys.stdin.readline()
    if not line: return
    
    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        
        result = "Hello I am a mock agent. Done!"
        
        artifact = {
            "id": f"{req_id}_output",
            "name": "mock-writer-agent Output",
            "type": "document/markdown",
            "data": result
        }
        
        print(json.dumps({
            "id": req_id,
            "artifact": artifact
        }))
        
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
