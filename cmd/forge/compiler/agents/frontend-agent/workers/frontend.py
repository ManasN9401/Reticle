from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions="Builds modern, interactive web applications and UI/UX using frontend technologies and local asset generation."
    for skill in ["frontend-uiux"]:
        instructions += "\n\n"+(root/"skills"/skill/"SKILL.md").read_text(encoding="utf-8")
    run(instructions,"frontend")
