"""MCP server for deterministic text analysis and transformation."""

from __future__ import annotations

import collections
import re

from mcp_server import MCPServer, Tool, ToolError, require_string


def analyze_text(arguments):
    text = require_string(arguments, "text")
    words = re.findall(r"[\w'-]+", text.casefold(), flags=re.UNICODE)
    requested = arguments.get("topWords", 5)
    if not isinstance(requested, int) or isinstance(requested, bool) or not 1 <= requested <= 25:
        raise ToolError("topWords must be an integer from 1 to 25")
    counts = collections.Counter(words)
    return {
        "characters": len(text),
        "lines": 0 if not text else text.count("\n") + 1,
        "words": len(words),
        "uniqueWords": len(counts),
        "topWords": [{"word": word, "count": count} for word, count in counts.most_common(requested)],
    }


def transform_text(arguments):
    text = require_string(arguments, "text")
    operation = require_string(arguments, "operation", maximum=20)
    if operation == "uppercase":
        return text.upper()
    if operation == "lowercase":
        return text.lower()
    if operation == "title":
        return text.title()
    if operation == "slug":
        slug = re.sub(r"[^a-z0-9]+", "-", text.casefold()).strip("-")
        return slug
    raise ToolError("operation must be uppercase, lowercase, title, or slug")


SERVER = MCPServer(
    "reticle-text-tools",
    "Deterministic text statistics and transformations.",
    [
        Tool(
            "analyze_text",
            "Count characters, lines, words, unique words, and common words.",
            {
                "type": "object",
                "properties": {
                    "text": {"type": "string"},
                    "topWords": {"type": "integer", "minimum": 1, "maximum": 25, "default": 5},
                },
                "required": ["text"],
                "additionalProperties": False,
            },
            analyze_text,
        ),
        Tool(
            "transform_text",
            "Transform text using uppercase, lowercase, title, or slug mode.",
            {
                "type": "object",
                "properties": {
                    "text": {"type": "string"},
                    "operation": {"type": "string", "enum": ["uppercase", "lowercase", "title", "slug"]},
                },
                "required": ["text", "operation"],
                "additionalProperties": False,
            },
            transform_text,
        ),
    ],
)


if __name__ == "__main__":
    SERVER.run()

