"""Small dependency-free MCP stdio server used by Reticle examples.

Stdout is reserved for JSON-RPC messages. Diagnostics must go to stderr.
"""

from __future__ import annotations

import json
import sys
from dataclasses import dataclass
from typing import Any, Callable


MODERN_PROTOCOL_VERSION = "2026-07-28"


class ToolError(ValueError):
    """An input or execution error safe to return as an MCP tool result."""


@dataclass(frozen=True)
class Tool:
    name: str
    description: str
    input_schema: dict[str, Any]
    handler: Callable[[dict[str, Any]], Any]


class MCPServer:
    def __init__(self, name: str, description: str, tools: list[Tool]) -> None:
        self.name = name
        self.description = description
        self.tools = {tool.name: tool for tool in tools}

    @staticmethod
    def _write(payload: dict[str, Any]) -> None:
        sys.stdout.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n")
        sys.stdout.flush()

    def _result(self, request_id: Any, result: Any) -> None:
        self._write({"jsonrpc": "2.0", "id": request_id, "result": result})

    def _error(self, request_id: Any, code: int, message: str) -> None:
        self._write({
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {"code": code, "message": message[:1024]},
        })

    def _tool_catalogue(self) -> list[dict[str, Any]]:
        return [
            {
                "name": tool.name,
                "description": tool.description,
                "inputSchema": tool.input_schema,
            }
            for tool in sorted(self.tools.values(), key=lambda item: item.name)
        ]

    @staticmethod
    def _content(value: Any) -> dict[str, Any]:
        if isinstance(value, str):
            text = value
            structured = {"value": value}
        else:
            structured = value
            text = json.dumps(value, ensure_ascii=False, indent=2)
        return {
            "content": [{"type": "text", "text": text}],
            "structuredContent": structured,
            "isError": False,
        }

    def _dispatch(self, message: dict[str, Any]) -> None:
        method = message.get("method")
        request_id = message.get("id")

        # Notifications do not receive responses.
        if request_id is None:
            return

        if method == "server/discover":
            self._result(request_id, {
                "protocolVersions": [MODERN_PROTOCOL_VERSION],
                "capabilities": {"tools": {}},
                "serverInfo": {
                    "name": self.name,
                    "version": "1.0.0",
                    "description": self.description,
                },
            })
            return

        if method == "ping":
            self._result(request_id, {})
            return

        if method == "tools/list":
            self._result(request_id, {"tools": self._tool_catalogue()})
            return

        if method == "tools/call":
            params = message.get("params") or {}
            tool_name = params.get("name")
            arguments = params.get("arguments") or {}
            tool = self.tools.get(tool_name)
            if tool is None:
                self._error(request_id, -32602, f"Unknown tool: {tool_name}")
                return
            if not isinstance(arguments, dict):
                self._error(request_id, -32602, "Tool arguments must be an object")
                return
            try:
                self._result(request_id, self._content(tool.handler(arguments)))
            except ToolError as exc:
                self._result(request_id, {
                    "content": [{"type": "text", "text": str(exc)}],
                    "isError": True,
                })
            except Exception as exc:  # Keep unexpected details off stdout.
                print(f"{self.name}: {type(exc).__name__}: {exc}", file=sys.stderr, flush=True)
                self._error(request_id, -32603, "Tool execution failed")
            return

        self._error(request_id, -32601, f"Method not found: {method}")

    def run(self) -> None:
        for line in sys.stdin:
            try:
                message = json.loads(line)
                if not isinstance(message, dict) or message.get("jsonrpc") != "2.0":
                    raise ValueError("invalid JSON-RPC envelope")
                self._dispatch(message)
            except json.JSONDecodeError:
                self._error(None, -32700, "Parse error")
            except ValueError as exc:
                self._error(None, -32600, str(exc))


def require_string(arguments: dict[str, Any], name: str, *, maximum: int = 50_000) -> str:
    value = arguments.get(name)
    if not isinstance(value, str):
        raise ToolError(f"{name} must be a string")
    if len(value) > maximum:
        raise ToolError(f"{name} exceeds {maximum} characters")
    return value

