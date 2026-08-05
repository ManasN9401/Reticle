"""
HyperParallel Worker Script
"""
import sys
import json
from litellm import completion

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
        
        system_msg = f"""
You are the Forge Coder. You are generating a python worker script for a new agent.
Agent ID: {agent_id}
Agent System Prompt: {sys_prompt}

The python script MUST read a JSON line from sys.stdin, process it using an LLM (litellm groq/llama-3.1-8b-instant), and output a JSON artifact to stdout.

Use this boilerplate structure:
import sys, json
from litellm import completion

def main():
    line = sys.stdin.readline()
    if not line: return
    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        
        # your code calling LLM
        result = "hello"
        
        artifact = {{
            "id": f"{{req_id}}_output",
            "name": "{agent_id} Output",
            "type": "document/markdown",
            "data": result
        }}
        print(json.dumps({{"id": req_id, "artifact": artifact}}))
    except Exception as e:
        print(f"ERROR: {{e}}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()

Output ONLY the raw python code. Do not output markdown code blocks (like ```python).
"""

        response = completion(
            model="groq/llama-3.1-8b-instant",
            messages=[
                {"role": "system", "content": system_msg},
                {"role": "user", "content": f"Generate the {agent_id}.py file."}
            ]
        )
        
        code = response.choices[0].message.content.strip()
        if code.startswith("```python"):
            code = code[9:]
        if code.startswith("```"):
            code = code[3:]
        if code.endswith("```"):
            code = code[:-3]
            
        generated_files[f"workers/{agent_id}.py"] = code.strip()
        
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
