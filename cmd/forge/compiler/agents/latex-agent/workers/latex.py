from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions="A professional typesetter skilled in LaTeX, TikZ for statistics diagrams, and book compilation. Transforms raw markdown content into gorgeous PDF books."
    for skill in ["academic-writing"]:
        instructions += "\n\n"+(root/"skills"/skill/"SKILL.md").read_text(encoding="utf-8")
    run(instructions,"writing")
