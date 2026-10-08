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
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


BROKEN_SCRIPT = "import * as THREE from 'three'\nconsole.log(THREE)\n"
FIXED_SCRIPT = "// THREE comes from the CDN global\nconsole.log(THREE)\n"


class WebGate(unittest.TestCase):
    def run_worker(self, kind, script):
        script = list(script)
        results = []

        def completion(**kwargs):
            # Everything the model has been told by its tools so far.
            results[:] = [m["content"] for m in kwargs["messages"] if m.get("role") == "tool"]
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                worker_sdk.run("Fixture", kind=kind)
            return json.loads(output.getvalue())["artifact"]["data"], results, len(script)

    def build_broken_page(self):
        return [
            tool_response("write_file", {"path": "index.html", "content": '<script src="script.js"></script>'}),
            tool_response("write_file", {"path": "script.js", "content": BROKEN_SCRIPT}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("read_file", {"path": "script.js"}),
        ]

    def test_a_frontend_node_cannot_finish_with_an_unchecked_or_broken_page(self):
        script = self.build_broken_page() + [
            tool_response("mark_task_complete", {"summary": "premature: never checked"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "premature: check reported problems"}),
            tool_response("replace_file_content", {"path": "script.js", "target_content": BROKEN_SCRIPT.splitlines()[0] + "\n", "replacement_content": FIXED_SCRIPT.splitlines()[0] + "\n"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "shipped a page that runs"}),
        ]
        summary, results, remaining = self.run_worker("frontend", script)
        self.assertEqual(summary, "shipped a page that runs")
        self.assertEqual(remaining, 0)
        joined = "\n".join(results)
        self.assertIn("have not checked that the page can run", joined)
        self.assertIn("Cannot use import statement outside a module", joined)
        self.assertIn("No blocking problems found", joined)

    def test_editing_after_a_clean_check_requires_checking_again(self):
        script = [
            tool_response("write_file", {"path": "index.html", "content": "<p>hi</p>"}),
            tool_response("check_web_page", {}),
            tool_response("write_file", {"path": "style.css", "content": "p{color:red}"}),
            tool_response("read_file", {"path": "style.css"}),
            tool_response("mark_task_complete", {"summary": "premature: edited after the check"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        summary, results, remaining = self.run_worker("frontend", script)
        self.assertEqual(summary, "done")
        self.assertEqual(remaining, 0)
        self.assertIn("have not checked that the page can run", "\n".join(results))

    def test_a_frontend_node_that_wrote_no_web_files_is_not_held_up(self):
        script = [
            tool_response("write_file", {"path": "notes.md", "content": "# notes"}),
            tool_response("read_file", {"path": "notes.md"}),
            tool_response("mark_task_complete", {"summary": "documented"}),
        ]
        summary, _, _ = self.run_worker("frontend", script)
        self.assertEqual(summary, "documented")

    def test_other_roles_are_advised_not_gated(self):
        script = self.build_broken_page() + [tool_response("mark_task_complete", {"summary": "wrote a fragment"})]
        summary, _, remaining = self.run_worker("coding", script)
        self.assertEqual(summary, "wrote a fragment")
        self.assertEqual(remaining, 0)

    def test_the_check_is_available_to_every_role_as_a_read_only_tool(self):
        descriptor = worker_sdk._local_tool_descriptor("check_web_page", "d", {"path": {"type": "string", "optional": True}}, "workspace.read")
        self.assertEqual(descriptor["effect"], "no_effect")
        self.assertEqual(descriptor["schema"]["required"], [])


if __name__ == "__main__":
    unittest.main()
