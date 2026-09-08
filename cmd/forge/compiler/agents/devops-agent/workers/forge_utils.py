"""Compatibility exports for the shared tools."""
from pathlib import Path
import runpy
globals().update(runpy.run_path(str(Path(__file__).resolve().parents[3]/"lib/forge_utils.py")))
