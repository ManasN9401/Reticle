"""Generate small agent entrypoints using the maintained worker SDK."""
import json
from pathlib import Path
import re
import sys

LIB = Path(__file__).resolve().parents[3] / "lib"
sys.path.insert(0, str(LIB))
from forge_utils import load_skill_instructions

def main():
    req = json.load(sys.stdin)
    dag = json.loads(next(i["data"] for i in req["inputs"] if i.get("name") == "DAG JSON"))
    workspace = Path(req["memory"]["workspace_dir"]).resolve()
    root = Path(__file__).resolve().parents[6]
    for agent in dag.get("agents", []):
        # Registered agents already have a maintained manifest and entrypoint.
        # A model commonly emits the schema placeholder "TBD" for these; that
        # must never cause the compiler to replace a specialist with a generic
        # generated worker.
        if not agent.get("is_new"):
            continue
        agent_id = agent.get("id", "")
        if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}", agent_id):
            raise ValueError("Invalid generated agent ID")
        instructions = agent.get("system_prompt", "Complete the assigned task and verify the result.")
        report = load_skill_instructions(root / "skills", agent.get("skills", []))
        instructions += report["text"]
        for skill in report["missing"]:
            print(f"Warning: Missing skill instructions for '{skill}', skipping.", file=sys.stderr)
        # The generated worker reports this at startup so the LLM panel shows what it was built with.
        skills = {"applied": report["applied"], "missing": report["missing"]}
        target = workspace / "agents" / agent_id / "workers"
        if target.resolve() != target:
            raise ValueError("Aliased generated worker path")
        target.mkdir(parents=True, exist_ok=True)
        # The instructions (task prompt plus skill text, tens of KB) live in their own file. Embedded in
        # the entrypoint they were echoed whole by every Python traceback, burying the real error and
        # putting skill text in front of the failure classifier.
        (target / f"{agent_id}.instructions.md").write_text(instructions, encoding="utf-8")
        (target / f"{agent_id}.py").write_text(
            "from pathlib import Path\n"
            "from worker_sdk import run\n\n"
            "if __name__ == '__main__':\n"
            f"    instructions = Path(__file__).with_name({agent_id + '.instructions.md'!r}).read_text(encoding='utf-8')\n"
            f"    run(instructions, skills={skills!r})\n",
            encoding="utf-8",
        )
        for name in ("forge_utils.py", "worker_sdk.py", "comfy_tools.py", "image_tools.py", "web_tools.py", "browser_tools.py",
                     "browse_tools.py", "research_tools.py", "quant_tools.py"):
            (target / name).write_bytes((LIB / name).read_bytes())
    (workspace / "src").mkdir(exist_ok=True)
    print(json.dumps({"id":req["id"],"artifact":{"id":req["id"]+"_output","name":"Python Files","type":"application/json","data":"{}"}}))

if __name__ == "__main__":
    main()
