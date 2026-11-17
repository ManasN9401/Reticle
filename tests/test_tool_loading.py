"""A node starts with the tools it is likely to need and loads the rest by name (list_tools), so a
small model or a small per-minute route is not sent every tool's full definition on every turn."""
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import comfy_tools
import worker_sdk


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [
        {"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class Done(Exception):
    pass


def run_worker(script, instructions="Fixture", environment=None, files=None):
    """Run the tool loop on scripted replies; returns (tool names sent per request, tool results)."""
    sent, results = [], []

    def completion(**kwargs):
        sent.append([tool["function"]["name"] for tool in kwargs["tools"]])
        results.extend(m["content"] for m in kwargs["messages"] if m.get("role") == "tool" and m["content"] not in results)
        if not script:
            raise Done()
        return script.pop(0)

    with tempfile.TemporaryDirectory() as temp:
        for rel, text in (files or {}).items():
            path = Path(temp) / "src" / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text, encoding="utf-8")
        (Path(temp) / "src").mkdir(exist_ok=True)
        request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp},
                   "parameters": {"llm_model": "ollama/fixture"}}
        with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false", **(environment or {})}, clear=True), \
                patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                patch.object(sys, "stdout", io.StringIO()), patch.object(sys, "stderr", io.StringIO()):
            try:
                worker_sdk.run(instructions, kind="writing")
            except Done:
                pass
            moved = (Path(temp) / "src" / "b.txt").exists()
    return sent, results, moved


class ToolLoading(unittest.TestCase):
    def test_rarely_needed_tools_are_named_but_not_sent(self):
        sent, _, _ = run_worker([])
        first = sent[0]
        self.assertIn("list_tools", first)
        self.assertIn("write_file", first)
        self.assertNotIn("move_path", first)
        everything, _, _ = run_worker([], environment={"RETICLE_TOOL_LOADING": "all"})
        self.assertIn("move_path", everything[0])
        self.assertNotIn("list_tools", everything[0], "with every tool sent there is nothing to load")

    def test_list_tools_summarises_then_loads_by_name(self):
        sent, results, _ = run_worker([tool_response("list_tools", {}), tool_response("list_tools", {"names": ["move_path"]})])
        self.assertIn("- move_path: ", results[0])
        self.assertIn("move_path(source_path (string), destination_path (string))", results[1])
        self.assertNotIn("move_path", sent[1], "a summary does not load anything")
        self.assertIn("move_path", sent[2])

    def test_a_deferred_tool_called_directly_still_runs_and_stays_loaded(self):
        sent, _, moved = run_worker([tool_response("move_path", {"source_path": "a.txt", "destination_path": "b.txt"})],
                                    files={"a.txt": "fixture"})
        self.assertTrue(moved)
        self.assertIn("move_path", sent[1])

    def test_a_tool_the_instructions_name_starts_loaded(self):
        sent, _, _ = run_worker([], instructions="Tidy the assets, then use `move_path` to rename them.")
        self.assertIn("move_path", sent[0])
        self.assertNotIn("delete_path", sent[0])

    def test_an_unknown_name_is_refused_with_the_loadable_tools(self):
        _, results, _ = run_worker([tool_response("list_tools", {"names": ["teleport"]})])
        self.assertIn("No such tool: teleport", results[0])
        self.assertIn("move_path", results[0])


if __name__ == "__main__":
    unittest.main()
