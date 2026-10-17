from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions='Reads and extracts information from public websites with a real headless browser (Playwright): navigates multi-step pages, clicks, fills search forms, and returns what the pages show. Read-only by default, stops at access walls, and records every page it read under browse/.'
    report = load_skill_instructions(root/"skills", ['browser-automation'])
    instructions += report["text"]
    run(instructions,'browser',skills={"applied": report["applied"], "missing": report["missing"]})
