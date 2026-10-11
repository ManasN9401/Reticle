from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions="Specialized in generating, validating, and applying Infrastructure-as-Code (Terraform, Docker, Kubernetes) and CI/CD pipelines."
    report = load_skill_instructions(root/"skills", ["devops-infrastructure"])
    instructions += report["text"]
    run(instructions,"devops",skills={"applied": report["applied"], "missing": report["missing"]})
