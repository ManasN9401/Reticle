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
    
    # Generate Agent YAMLs
    for agent in dag.get("agents", []):
        if not agent.get("is_new") and not agent.get("system_prompt"):
            continue
            
        agent_id = agent.get("id")
        
        agent_yaml_str = f"""id: {agent_id}
name: {agent.get('name', agent_id)}
description: {agent.get('description', '')}
version: 1.0.0
runtime: python
entrypoint: workers/{agent_id}.py
"""
        generated_files[f"agents/{agent_id}.yaml"] = agent_yaml_str
        
    # Generate Workflow YAML
    
    workflow_yaml_str = f"""id: generated-workflow
name: {dag.get('workflow_name', 'Generated Workflow')}
version: 1.0.0
nodes:"""
    for i, node in enumerate(dag.get("nodes", [])):
        workflow_yaml_str += f"""
  - id: {node.get('id')}
    agent: {node.get('agent_id')}
    parameters:
      llm_model: groq/llama-3.1-8b-instant"""
        
    workflow_yaml_str += "\nedges:"
    for edge in dag.get("edges", []):
        workflow_yaml_str += f"""
  - from: {edge.get('from')}
    to: {edge.get('to')}"""
        
    generated_files["workflows/workflow.yaml"] = workflow_yaml_str
    
    artifact = {
        "id": f"{req_id}_output",
        "name": "YAML Files",
        "type": "application/json",
        "data": json.dumps(generated_files)
    }
    
    print(json.dumps({
        "id": req_id,
        "artifact": artifact
    }))

if __name__ == "__main__":
    main()
