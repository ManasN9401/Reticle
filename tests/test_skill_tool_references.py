"""A skill that tells a worker to call a tool it was never offered wastes turns and teaches it to
invent tools. The set of tools is taken from what a worker is really offered, not from a list kept
here, so renaming or removing a tool fails this test until the skills that mention it are updated."""
import io
import json
import re
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "cmd/forge/compiler/lib"))
import worker_sdk

ALL_CAPABILITIES = [
    "workspace.read", "workspace.write", "network.public", "process.container", "process.native",
    "memory.execution", "graph.delegate", "image.local", "browser.run", "rag.local",
]
# The generic worker and every specialist kind: a skill written for the research, browser, market-data
# or REPL agent names that agent's tools.
KINDS = ("writing", "rag", "osint", "browser", "quant", "hermes")

# A backticked word that starts with a verb these tools use and has an underscore: read_file,
# check_web_page, generate_local_asset(s). Words such as output_path do not start with one.
TOOL_LIKE = re.compile(
    r"`((?:read|write|list|search|replace|apply|find|move|delete|execute|check|run|generate|resize|make|"
    r"convert|crop|mark|index|query|remove|remember|delegate|fetch|browse|web|market|price|financial|technical|risk)_[a-z_]+)(\(s\))?`"
)


def offered_tool_names(kind):
    class Done(Exception):
        pass

    def completion(**kwargs):
        captured.append([tool["function"]["name"] for tool in kwargs["tools"]])
        raise Done()

    captured = []
    with tempfile.TemporaryDirectory() as temp:
        (Path(temp) / "src").mkdir()
        request = {
            "id": "execution|node", "execution": "execution", "capabilities": ALL_CAPABILITIES,
            "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"},
        }
        with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                patch.object(sys, "stdout", io.StringIO()), patch.object(sys, "stderr", io.StringIO()):
            try:
                worker_sdk.run("Fixture", kind=kind)
            except Done:
                pass
    return set(captured[0]) if captured else set()


class SkillsNameRealTools(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tools = set().union(*(offered_tool_names(kind) for kind in KINDS))

    def test_the_offered_set_is_the_real_one(self):
        # If this fails the harness above broke and the other test would pass vacuously.
        for name in ("read_file", "write_file", "replace_file_content", "apply_edits", "find_files",
                     "execute_terminal_command", "check_web_page", "run_page", "mark_task_complete",
                     "web_search", "fetch_pages", "browse_web", "market_quote", "price_history",
                     "financial_statements", "technical_indicators", "risk_metrics"):
            self.assertIn(name, self.tools)

    def test_every_tool_a_skill_names_exists(self):
        skills = ROOT / "skills"
        checked, problems = 0, []
        for manifest in sorted(skills.glob("*.yaml")):
            skill = skills / manifest.stem / "SKILL.md"
            if not skill.is_file():
                continue
            text = skill.read_text(encoding="utf-8")
            for match in TOOL_LIKE.finditer(text):
                checked += 1
                name = match.group(1)
                names = {name, name + "s"} if match.group(2) else {name}
                if not names & self.tools:
                    problems.append(f"{manifest.stem}: `{name}{match.group(2) or ''}`")
        self.assertGreater(checked, 5, "the pattern found almost no tool names; it has probably stopped matching")
        self.assertEqual(problems, [], "skills name tools no worker is offered")


if __name__ == "__main__":
    unittest.main()
