"""Compatibility entrypoint for the shared worker generator."""
from pathlib import Path
import runpy

if __name__ == "__main__":
    runpy.run_path(str(Path(__file__).resolve().parents[2] / "coder-agent/workers/coder.py"), run_name="__main__")
