"""MCP server for bounded deterministic numerical tasks."""

from __future__ import annotations

import math
import statistics

from mcp_server import MCPServer, Tool, ToolError


def require_numbers(arguments):
    values = arguments.get("numbers")
    if not isinstance(values, list) or not 1 <= len(values) <= 1_000:
        raise ToolError("numbers must contain between 1 and 1000 values")
    result = []
    for value in values:
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
            raise ToolError("every value must be a finite number")
        result.append(float(value))
    return result


def descriptive_statistics(arguments):
    values = require_numbers(arguments)
    return {
        "count": len(values),
        "sum": math.fsum(values),
        "minimum": min(values),
        "maximum": max(values),
        "mean": statistics.fmean(values),
        "median": statistics.median(values),
        "populationStandardDeviation": statistics.pstdev(values),
    }


def fibonacci(arguments):
    count = arguments.get("count")
    if not isinstance(count, int) or isinstance(count, bool) or not 1 <= count <= 100:
        raise ToolError("count must be an integer from 1 to 100")
    sequence = []
    left, right = 0, 1
    for _ in range(count):
        sequence.append(left)
        left, right = right, left + right
    return {"count": count, "sequence": sequence}


SERVER = MCPServer(
    "reticle-math-tools",
    "Bounded numerical summaries and sequence generation.",
    [
        Tool(
            "descriptive_statistics",
            "Calculate bounded descriptive statistics for finite numbers.",
            {
                "type": "object",
                "properties": {
                    "numbers": {
                        "type": "array",
                        "items": {"type": "number"},
                        "minItems": 1,
                        "maxItems": 1000,
                    }
                },
                "required": ["numbers"],
                "additionalProperties": False,
            },
            descriptive_statistics,
        ),
        Tool(
            "fibonacci",
            "Generate the first 1 to 100 Fibonacci numbers.",
            {
                "type": "object",
                "properties": {"count": {"type": "integer", "minimum": 1, "maximum": 100}},
                "required": ["count"],
                "additionalProperties": False,
            },
            fibonacci,
        ),
    ],
)


if __name__ == "__main__":
    SERVER.run()

