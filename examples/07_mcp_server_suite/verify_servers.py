"""Exercise discovery, catalogue listing, and one call on every example server."""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent
WORKSPACE = ROOT.parents[1]
META = {
    "io.modelcontextprotocol/protocolVersion": "2026-07-28",
    "io.modelcontextprotocol/clientInfo": {"name": "fixture-verifier", "version": "1.0.0"},
    "io.modelcontextprotocol/clientCapabilities": {},
}


CASES = [
    ("text_server.py", "transform_text", {"text": "Reticle MCP Test", "operation": "slug"}, "reticle-mcp-test"),
    ("math_server.py", "fibonacci", {"count": 7}, [0, 1, 1, 2, 3, 5, 8]),
    (
        "planning_server.py",
        "assess_risks",
        {"risks": [{"name": "Unverified output", "likelihood": 3, "impact": 5}]},
        "high",
    ),
    ("workspace_server.py", "read_text", {"path": "README.md", "maxCharacters": 80}, "README.md"),
]


def exchange(process, payload):
    process.stdin.write(json.dumps(payload) + "\n")
    process.stdin.flush()
    line = process.stdout.readline()
    if not line:
        raise RuntimeError(process.stderr.read() or "server closed stdout")
    response = json.loads(line)
    if "error" in response:
        raise RuntimeError(response["error"])
    return response["result"]


def verify(script, tool, arguments, expected):
    process = subprocess.Popen(
        [sys.executable, str(ROOT / script)],
        cwd=WORKSPACE,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
    )
    try:
        discovery = exchange(process, {
            "jsonrpc": "2.0", "id": 1, "method": "server/discover", "params": {"_meta": META}
        })
        assert "2026-07-28" in discovery["protocolVersions"]
        catalogue = exchange(process, {
            "jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {"_meta": META}
        })
        assert tool in {item["name"] for item in catalogue["tools"]}
        result = exchange(process, {
            "jsonrpc": "2.0",
            "id": 3,
            "method": "tools/call",
            "params": {"name": tool, "arguments": arguments, "_meta": META},
        })
        serialized = json.dumps(result.get("structuredContent"), ensure_ascii=False)
        assert json.dumps(expected, ensure_ascii=False).strip('"') in serialized
        print(f"PASS {script}: {tool}")
    finally:
        process.stdin.close()
        process.wait(timeout=5)


if __name__ == "__main__":
    for case in CASES:
        verify(*case)
    print(f"Verified {len(CASES)} MCP servers.")

