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


class ReportedFiles(unittest.TestCase):
    def run_worker(self, script):
        script = list(script)

        def completion(**kwargs):
            return script.pop(0)

        def generate(workspace_dir, **args):
            return "Successfully generated " + args["output_path"]

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            (Path(temp) / "src" / "spec.md").write_text("# spec", encoding="utf-8")
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(comfy_tools, "generate_local_asset", generate), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                worker_sdk.run("Fixture", kind="writing")
            return json.loads(output.getvalue())

    def test_the_final_response_lists_every_file_the_worker_wrote(self):
        response = self.run_worker([
            tool_response("write_file", {"path": "index.html", "content": "<h1>x</h1>"}),
            tool_response("write_file", {"path": "js/main.js", "content": "console.log(1)"}),
            tool_response("generate_local_asset", {"prompt": "art", "output_path": "assets/gallery-01.png"}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("read_file", {"path": "js/main.js"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ])
        self.assertEqual(response["files"], ["assets/gallery-01.png", "index.html", "js/main.js"])

    def test_paths_are_reported_as_written_so_they_match_the_disk(self):
        # write_file("src/x.html") lands at src/src/x.html, so the prefix must be kept.
        response = self.run_worker([
            tool_response("write_file", {"path": "src/page.html", "content": "<p>x</p>"}),
            tool_response("read_file", {"path": "src/page.html"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ])
        self.assertEqual(response["files"], ["src/page.html"])

    def test_a_worker_that_wrote_nothing_reports_an_empty_list(self):
        response = self.run_worker([
            tool_response("read_file", {"path": "spec.md"}),
            tool_response("mark_task_complete", {"summary": "reviewed"}),
        ])
        self.assertEqual(response["files"], [])


if __name__ == "__main__":
    unittest.main()
