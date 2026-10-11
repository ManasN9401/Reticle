from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions="A professional typesetter skilled in LaTeX, TikZ for statistics diagrams, and book compilation. Transforms raw markdown content into gorgeous PDF books."
    report = load_skill_instructions(root/"skills", ["academic-writing"])
    instructions += report["text"]
    run(instructions,"writing",skills={"applied": report["applied"], "missing": report["missing"]})
