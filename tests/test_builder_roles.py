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


class BuilderRoles(unittest.TestCase):
    def run_worker(self, kind, script, instructions, parameters=None):
        script = list(script)

        def completion(**kwargs):
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            (Path(temp) / "src" / "spec.md").write_text("# design spec", encoding="utf-8")
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture", **(parameters or {})}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                worker_sdk.run(instructions, kind=kind)
            written = sorted(path.name for path in (Path(temp) / "src").iterdir())
            return json.loads(output.getvalue())["artifact"]["data"], script, written

    NO_OUTPUTS = "No specific output files were declared for this role.\n"

    def test_a_frontend_node_cannot_finish_by_only_reading_its_spec(self):
        # The observed failure: read the design spec, then call mark_task_complete.
        script = [
            tool_response("read_file", {"path": "spec.md"}),
            tool_response("mark_task_complete", {"summary": "premature: only read the spec"}),
            tool_response("write_file", {"path": "index.html", "content": "<h1>gallery</h1>"}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "built the site"}),
        ]
        summary, remaining, written = self.run_worker("frontend", script, self.NO_OUTPUTS)
        self.assertEqual(summary, "built the site")
        self.assertEqual(remaining, [])
        self.assertIn("index.html", written)

    def test_other_roles_keep_the_review_only_path(self):
        # The architect explicitly allows review and analysis nodes with no outputs.
        script = [
            tool_response("read_file", {"path": "spec.md"}),
            tool_response("mark_task_complete", {"summary": "reviewed"}),
        ]
        summary, remaining, written = self.run_worker("coding", script, self.NO_OUTPUTS)
        self.assertEqual(summary, "reviewed")
        self.assertEqual(remaining, [])
        self.assertEqual(written, ["spec.md"])

    def test_declared_outputs_still_govern_a_frontend_node(self):
        script = [
            tool_response("read_file", {"path": "spec.md"}),
            tool_response("mark_task_complete", {"summary": "premature"}),
            tool_response("write_file", {"path": "site.html", "content": "<p>x</p>"}),
            tool_response("read_file", {"path": "site.html"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        instructions = "Create or update exactly these workspace files: site.html.\n"
        summary, remaining, _ = self.run_worker("frontend", script, instructions)
        self.assertEqual(summary, "done")
        self.assertEqual(remaining, [])

    def test_a_registered_agent_is_held_to_outputs_declared_in_its_node_prompt(self):
        # Registered agents run a maintained worker, so the architect delivers the
        # declared outputs as parameters.system_prompt rather than in the script.
        node_prompt = (
            "You are frontend-agent.\n"
            "Read these workspace files: spec.md. Create or update exactly these workspace files: site/index.html.\n"
            "Upstream nodes are: none declared.\n"
        )
        script = [
            tool_response("read_file", {"path": "spec.md"}),
            tool_response("mark_task_complete", {"summary": "premature"}),
            tool_response("write_file", {"path": "site/index.html", "content": "<h1>x</h1>"}),
            tool_response("read_file", {"path": "site/index.html"}),
            tool_response("mark_task_complete", {"summary": "built"}),
        ]
        summary, remaining, _ = self.run_worker("coding", script, "Static agent instructions.", {"system_prompt": node_prompt})
        self.assertEqual(summary, "built")
        self.assertEqual(remaining, [])

    def test_a_leading_src_segment_does_not_hide_a_written_deliverable(self):
        self.assertTrue(worker_sdk._output_satisfied("src/index.html", {"index.html"}))
        self.assertTrue(worker_sdk._output_satisfied("index.html", {"src/index.html"}))
        self.assertTrue(worker_sdk._output_satisfied("src/public/images", {"public/images/a.png"}))
        self.assertFalse(worker_sdk._output_satisfied("src/index.html", {"other.html"}))


if __name__ == "__main__":
    unittest.main()
