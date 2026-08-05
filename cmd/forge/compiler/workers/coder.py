"""
HyperParallel Worker Script
"""
import sys
import json

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    inputs = req.get("inputs", [])
    
    dag_json_str = ""
    for i in inputs:
        if i.get("name") == "DAG JSON":
            dag_json_str = i.get("data", "{}")
            break
            
    dag = json.loads(dag_json_str)
    
    generated_files = {}
    
    for agent in dag.get("agents", []):
        if not agent.get("is_new"):
            continue
            
        agent_id = agent.get("id")
        sys_prompt = agent.get("system_prompt", "You are a helpful assistant.")
        
        # Generate the Python code using LLM
        # We will use a standard boilerplate and inject the system prompt
        boilerplate = f'''"""
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
        
        artifact = {{
            "id": f"{{req_id}}_output",
            "name": "{agent_id} Output",
            "type": "document/markdown",
            "data": result
        }}
        
        print(json.dumps({{
            "id": req_id,
            "artifact": artifact
        }}))
        
    except Exception as e:
        print(f"ERROR: {{e}}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
'''
        generated_files[f"workers/{agent_id}.py"] = boilerplate
        
    artifact = {
        "id": f"{req_id}_output",
        "name": "Python Files",
        "type": "application/json",
        "data": json.dumps(generated_files)
    }
    
    print(json.dumps({
        "id": req_id,
        "artifact": artifact
    }))

if __name__ == "__main__":
    main()
