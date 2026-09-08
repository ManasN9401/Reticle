"""Legacy import compatibility; workspace must be supplied explicitly."""
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/"lib"))
from comfy_tools import *
