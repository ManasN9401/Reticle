from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions="Builds modern, interactive web applications and UI/UX using frontend technologies and local asset generation."
    report = load_skill_instructions(root/"skills", ["frontend-uiux"])
    instructions += report["text"]
    run(instructions,"frontend",skills={"applied": report["applied"], "missing": report["missing"]})
