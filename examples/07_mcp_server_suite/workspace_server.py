"""Read-only MCP server restricted to its configured working directory."""

from __future__ import annotations

import os
from pathlib import Path

from mcp_server import MCPServer, Tool, ToolError, require_string


ROOT = Path.cwd().resolve()
MAX_FILES = 500


def confined(relative_path: str) -> Path:
    candidate = (ROOT / relative_path).resolve()
    try:
        candidate.relative_to(ROOT)
    except ValueError as exc:
        raise ToolError("path must remain inside the configured working directory") from exc
    return candidate


def list_files(arguments):
    relative = arguments.get("path", ".")
    if not isinstance(relative, str) or len(relative) > 1_000:
        raise ToolError("path must be a string of at most 1000 characters")
    maximum_depth = arguments.get("maxDepth", 2)
    include_hidden = arguments.get("includeHidden", False)
    if not isinstance(maximum_depth, int) or isinstance(maximum_depth, bool) or not 0 <= maximum_depth <= 5:
        raise ToolError("maxDepth must be an integer from 0 to 5")
    if not isinstance(include_hidden, bool):
        raise ToolError("includeHidden must be a boolean")
    target = confined(relative)
    if not target.is_dir():
        raise ToolError("path is not a directory")

    entries = []
    base_depth = len(target.parts)
    for current, directories, files in os.walk(target):
        current_path = Path(current)
        depth = len(current_path.parts) - base_depth
        directories[:] = sorted(
            name for name in directories
            if depth < maximum_depth and (include_hidden or not name.startswith("."))
        )
        for name in sorted(files):
            if include_hidden or not name.startswith("."):
                file_path = current_path / name
                entries.append(file_path.relative_to(ROOT).as_posix())
                if len(entries) >= MAX_FILES:
                    return {"root": str(ROOT), "files": entries, "truncated": True}
    return {"root": str(ROOT), "files": entries, "truncated": False}


def read_text(arguments):
    relative = require_string(arguments, "path", maximum=1_000)
    maximum = arguments.get("maxCharacters", 20_000)
    if not isinstance(maximum, int) or isinstance(maximum, bool) or not 1 <= maximum <= 100_000:
        raise ToolError("maxCharacters must be an integer from 1 to 100000")
    target = confined(relative)
    if not target.is_file():
        raise ToolError("path is not a file")
    try:
        text = target.read_text(encoding="utf-8")
    except UnicodeDecodeError as exc:
        raise ToolError("file is not UTF-8 text") from exc
    return {
        "path": target.relative_to(ROOT).as_posix(),
        "text": text[:maximum],
        "truncated": len(text) > maximum,
        "characters": len(text),
    }


SERVER = MCPServer(
    "reticle-workspace-reader",
    "Read-only, bounded access to files beneath the server working directory.",
    [
        Tool(
            "list_files",
            "List up to 500 files below a directory within the configured workspace.",
            {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "default": "."},
                    "maxDepth": {"type": "integer", "minimum": 0, "maximum": 5, "default": 2},
                    "includeHidden": {"type": "boolean", "default": False},
                },
                "additionalProperties": False,
            },
            list_files,
        ),
        Tool(
            "read_text",
            "Read a bounded UTF-8 text file within the configured workspace.",
            {
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "maxCharacters": {"type": "integer", "minimum": 1, "maximum": 100000, "default": 20000},
                },
                "required": ["path"],
                "additionalProperties": False,
            },
            read_text,
        ),
    ],
)


if __name__ == "__main__":
    SERVER.run()

