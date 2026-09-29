"""MCP server for deterministic planning artefacts."""

from __future__ import annotations

from mcp_server import MCPServer, Tool, ToolError, require_string


def create_checklist(arguments):
    objective = require_string(arguments, "objective", maximum=500)
    steps = arguments.get("steps")
    if not isinstance(steps, list) or not 1 <= len(steps) <= 50:
        raise ToolError("steps must contain between 1 and 50 strings")
    cleaned = []
    for step in steps:
        if not isinstance(step, str) or not step.strip() or len(step) > 500:
            raise ToolError("each step must be a non-empty string of at most 500 characters")
        cleaned.append(step.strip())
    verify = arguments.get("includeVerification", True)
    if not isinstance(verify, bool):
        raise ToolError("includeVerification must be a boolean")
    lines = [f"# {objective.strip()}", ""]
    lines.extend(f"- [ ] {step}" for step in cleaned)
    if verify:
        lines.extend(["", "## Verification", "", "- [ ] Confirm every step is complete and record evidence."])
    return {"objective": objective.strip(), "stepCount": len(cleaned), "markdown": "\n".join(lines)}


def assess_risks(arguments):
    risks = arguments.get("risks")
    if not isinstance(risks, list) or not 1 <= len(risks) <= 50:
        raise ToolError("risks must contain between 1 and 50 entries")
    assessed = []
    for item in risks:
        if not isinstance(item, dict):
            raise ToolError("each risk must be an object")
        name = item.get("name")
        likelihood = item.get("likelihood")
        impact = item.get("impact")
        if not isinstance(name, str) or not name.strip() or len(name) > 300:
            raise ToolError("each risk needs a non-empty name")
        if any(isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= 5 for value in (likelihood, impact)):
            raise ToolError("likelihood and impact must be integers from 1 to 5")
        score = likelihood * impact
        rating = "high" if score >= 15 else "medium" if score >= 6 else "low"
        assessed.append({
            "name": name.strip(),
            "likelihood": likelihood,
            "impact": impact,
            "score": score,
            "rating": rating,
        })
    assessed.sort(key=lambda item: (-item["score"], item["name"].casefold()))
    return {"risks": assessed}


SERVER = MCPServer(
    "reticle-planning-tools",
    "Create checklists and consistently score simple risk registers.",
    [
        Tool(
            "create_checklist",
            "Create a Markdown checklist from an objective and ordered steps.",
            {
                "type": "object",
                "properties": {
                    "objective": {"type": "string", "maxLength": 500},
                    "steps": {
                        "type": "array",
                        "items": {"type": "string", "maxLength": 500},
                        "minItems": 1,
                        "maxItems": 50,
                    },
                    "includeVerification": {"type": "boolean", "default": True},
                },
                "required": ["objective", "steps"],
                "additionalProperties": False,
            },
            create_checklist,
        ),
        Tool(
            "assess_risks",
            "Score and order risks using 1-5 likelihood and impact values.",
            {
                "type": "object",
                "properties": {
                    "risks": {
                        "type": "array",
                        "minItems": 1,
                        "maxItems": 50,
                        "items": {
                            "type": "object",
                            "properties": {
                                "name": {"type": "string"},
                                "likelihood": {"type": "integer", "minimum": 1, "maximum": 5},
                                "impact": {"type": "integer", "minimum": 1, "maximum": 5},
                            },
                            "required": ["name", "likelihood", "impact"],
                            "additionalProperties": False,
                        },
                    }
                },
                "required": ["risks"],
                "additionalProperties": False,
            },
            assess_risks,
        ),
    ],
)


if __name__ == "__main__":
    SERVER.run()

