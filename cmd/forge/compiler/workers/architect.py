"""
HyperParallel Worker Script
MANDATORY READING:
- RFC-008: Agent Architecture
- RFC-027: Worker Runtime Contract
"""
import sys
import json
def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    mem = req.get("memory", {})
    user_prompt = mem.get("user_prompt", "")
    available_agents = mem.get("available_agents", "None")
    
    try:
        print(f"[{req_id}] Architecting DAG...", file=sys.stderr)
        
        result = """
        {
          "workflow_name": "Mocked Workflow",
          "agents": [
            {
              "id": "mock-writer-agent",
              "name": "Mock Writer",
              "description": "Writes mock things",
              "is_new": true, 
              "system_prompt": "You are a mocked writer agent."
            }
          ],
          "nodes": [
            { "id": "writer-1", "agent_id": "mock-writer-agent" }
          ],
          "edges": []
        }
        """
        
        artifact = {
            "id": f"{req_id}_output",
            "name": "DAG JSON",
            "type": "application/json",
            "data": result
        }
        
        print(json.dumps({
            "id": req_id,
            "artifact": artifact
        }))
        
    except Exception as e:
        print(f"[{req_id}] LLM failed (missing key?), falling back to mock DAG. Error: {e}", file=sys.stderr)
        result = """
        {
          "workflow_name": "Mocked Workflow",
          "agents": [
            {
              "id": "mock-writer-agent",
              "name": "Mock Writer",
              "description": "Writes mock things",
              "is_new": true, 
              "system_prompt": "You are a mocked writer agent."
            }
          ],
          "nodes": [
            { "id": "writer-1", "agent_id": "mock-writer-agent" }
          ],
          "edges": []
        }
        """
        
        artifact = {
            "id": f"{req_id}_output",
            "name": "DAG JSON",
            "type": "application/json",
            "data": result
        }
        
        print(json.dumps({
            "id": req_id,
            "artifact": artifact
        }))

if __name__ == "__main__":
    main()
