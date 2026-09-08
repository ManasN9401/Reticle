import sys
import json
import subprocess
import os
import logging
import traceback
from pathlib import Path

logging.basicConfig(level=logging.ERROR)
logger = logging.getLogger(__name__)

def run_graphify(workspace):
    try:
        # Run graphify in the workspace
        subprocess.run(["graphify", ".", "--no-viz", "--backend", "gemini"], cwd=workspace, check=True, capture_output=True, text=True, timeout=180)

        report_path = Path(workspace) / "graphify-out" / "GRAPH_REPORT.md"
        if not report_path.exists():
            raise RuntimeError("Graphify did not generate a report")

        content = report_path.read_text(encoding="utf-8")

        # Extract God Nodes and Surprising Connections
        lines = content.split('\n')
        summary = []
        capture = False
        for line in lines:
            if line.startswith("## God Nodes") or line.startswith("## Surprising Connections") or line.startswith("## Suggested Questions"):
                capture = True
                summary.append(line)
            elif line.startswith("## ") and capture:
                capture = False
            elif capture:
                summary.append(line)

        return "\n".join(summary)
    except subprocess.CalledProcessError as e:
        logger.error(f"Graphify failed: {e.stderr}")
        raise RuntimeError("Graphify execution failed") from e
    except Exception as e:
        logger.error(f"Graphify error: {e}")
        raise

def main():
    line = sys.stdin.readline()
    if not line:
        return

    try:
        req = json.loads(line)
        req_id = req.get("id")
        workspace = str(Path(req["memory"]["workspace_dir"]) / "src")

        summary = run_graphify(workspace)

        artifact = {
            "id": f"{req_id}_graph_report",
            "name": "Graphify Architectural Summary",
            "type": "text/markdown",
            "data": summary
        }

        print(json.dumps({
            "id": req_id,
            "artifact": artifact
        }), flush=True)
    except Exception as e:
        logger.error(f"Worker crashed: {traceback.format_exc()}")
        sys.exit(1)

if __name__ == "__main__":
    main()
