from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions="Indexes codebases into a local ChromaDB vector space and retrieves/summarizes dense context for other agents."
    report = load_skill_instructions(root/"skills", ["rag-knowledge-base"])
    instructions += report["text"]
    run(instructions,"rag",skills={"applied": report["applied"], "missing": report["missing"]})
