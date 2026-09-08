"""Serialize validated manifests as JSON, a YAML-compatible subset."""
import json
from pathlib import Path
import re
import sys

def identifier(value):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,119}", value):
        raise ValueError(f"Invalid identifier: {value!r}")
    return value

def main():
    req = json.load(sys.stdin)
    dag = json.loads(next(i["data"] for i in req["inputs"] if i.get("name") == "DAG JSON"))
    session = identifier(req.get("execution", req["id"]).removeprefix("compile-"))
    root = Path(req["memory"]["workspace_dir"]).resolve()
    generated = {}
    for agent in dag.get("agents", []):
        if not agent.get("is_new") and not agent.get("system_prompt"):
            continue
        agent_id = identifier(agent["id"])
        definition = {"id":agent_id,"name":agent.get("name",agent_id),"description":agent.get("description",""),"version":"1.0.0","runtime":"python","entrypoint":f"workers/{agent_id}.py"}
        for key in ("inputs","outputs","memory","skills"):
            values = agent.get(key, [])
            if not isinstance(values,list) or not all(isinstance(v,str) for v in values):
                raise ValueError(f"{key} must be an array of strings")
            definition[key] = values
        generated[f"agents/{agent_id}/{agent_id}.yaml"] = definition
    nodes = [{"id":identifier(n["id"]),"agent":identifier(n["agent_id"]),"parameters":n.get("parameters",{}),"modality":n.get("modality","")} for n in dag.get("nodes",[])]
    if not nodes or len({n["id"] for n in nodes}) != len(nodes):
        raise ValueError("Workflow requires unique nodes")
    workflow = {"id":"workflow_"+session,"name":dag.get("workflow_name","Generated Workflow"),"version":"1.0.0","nodes":nodes,"edges":dag.get("edges",[])}
    generated[f"workflows/{workflow['id']}.yaml"] = workflow
    for relative, definition in generated.items():
        target = root / relative
        if target.resolve()!=target:
            raise ValueError("Aliased manifest path")
        target.parent.mkdir(parents=True,exist_ok=True)
        target.write_text(json.dumps(definition,indent=2)+"\n",encoding="utf-8")
    print(json.dumps({"id":req["id"],"artifact":{"id":req["id"]+"_output","name":"YAML Files","type":"application/json","data":"{}"}}))

if __name__ == "__main__":
    main()
