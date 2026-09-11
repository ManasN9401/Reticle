"""Generate small agent entrypoints using the maintained worker SDK."""
import json
from pathlib import Path
import re
import sys

LIB = Path(__file__).resolve().parents[3] / "lib"

def main():
    req = json.load(sys.stdin)
    dag = json.loads(next(i["data"] for i in req["inputs"] if i.get("name") == "DAG JSON"))
    workspace = Path(req["memory"]["workspace_dir"]).resolve()
    root = Path(__file__).resolve().parents[6]
    for agent in dag.get("agents", []):
        if not agent.get("is_new") and not agent.get("system_prompt"):
            continue
        agent_id = agent.get("id", "")
        if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}", agent_id):
            raise ValueError("Invalid generated agent ID")
        instructions = agent.get("system_prompt", "Complete the assigned task and verify the result.")
        for skill in agent.get("skills", []):
            if not re.fullmatch(r"[a-zA-Z0-9_-]+", skill):
                raise ValueError("Invalid skill ID")
            source = root / "skills" / skill / "SKILL.md"
            if not source.is_file():
                print(f"Warning: Missing skill instructions for '{skill}', skipping.", file=sys.stderr)
                continue
            instructions += "\n\n" + source.read_text(encoding="utf-8")
        target = workspace / "agents" / agent_id / "workers"
        if target.resolve() != target:
            raise ValueError("Aliased generated worker path")
        target.mkdir(parents=True, exist_ok=True)
        (target / f"{agent_id}.py").write_text("from worker_sdk import run\n\nif __name__ == '__main__':\n    run(" + repr(instructions) + ")\n", encoding="utf-8")
        for name in ("forge_utils.py", "worker_sdk.py", "comfy_tools.py"):
            (target / name).write_bytes((LIB / name).read_bytes())
    (workspace / "src").mkdir(exist_ok=True)
    print(json.dumps({"id":req["id"],"artifact":{"id":req["id"]+"_output","name":"Python Files","type":"application/json","data":"{}"}}))

if __name__ == "__main__":
    main()
