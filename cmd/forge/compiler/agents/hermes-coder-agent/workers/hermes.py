from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions='Coder that works in a read-eval-print loop: writes tests first, writes code, runs it in the terminal, reads the failure, fixes one thing and runs again. It finishes only when the code has run clean after its last change.'
    report = load_skill_instructions(root/"skills", ['hermes-repl'])
    instructions += report["text"]
    run(instructions,'hermes',skills={"applied": report["applied"], "missing": report["missing"]})
