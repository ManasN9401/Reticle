from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from worker_sdk import run
from forge_utils import load_skill_instructions
if __name__ == "__main__":
    root=Path(__file__).resolve().parents[6]
    instructions='Quantitative finance analyst. Pulls quotes, price history and financial statements from Yahoo Finance, computes technical indicators and risk metrics in code, and reports findings with the data behind them. Analysis, not investment advice.'
    report = load_skill_instructions(root/"skills", ['quant-finance'])
    instructions += report["text"]
    run(instructions,'quant',skills={"applied": report["applied"], "missing": report["missing"]})
