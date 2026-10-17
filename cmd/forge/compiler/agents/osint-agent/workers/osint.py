from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions='Open-source intelligence analyst. Runs live web searches, reads many public pages in parallel, cross-checks claims across sources and writes a cited report backed by a source ledger (research/sources.json). Passive, public sources only.'
    report = load_skill_instructions(root/"skills", ['osint-investigation'])
    instructions += report["text"]
    run(instructions,'osint',skills={"applied": report["applied"], "missing": report["missing"]})
