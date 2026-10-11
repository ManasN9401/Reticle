from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions="Specialized in designing, writing, and debugging Deep Learning models exclusively using PyTorch and HuggingFace."
    report = load_skill_instructions(root/"skills", ["ml-engineering"])
    instructions += report["text"]
    run(instructions,"ml",skills={"applied": report["applied"], "missing": report["missing"]})
