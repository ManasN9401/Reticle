import io
import json
from contextlib import nullcontext
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


class GeneratedAssetOutputs(unittest.TestCase):
    def test_generated_images_satisfy_declared_output_directories(self):
        script = [
            tool_response("generate_local_asset", {"prompt": "art", "output_path": "public/images/art/artwork-1.jpg"}),
            tool_response("mark_task_complete", {"summary": "too early: thumbs are missing"}),
            tool_response("generate_local_asset", {"prompt": "thumb", "checkpoint": "fixture.safetensors", "width": 256, "height": 256, "output_path": "public/images/thumbs/artwork-1.jpg"}),
            tool_response("mark_task_complete", {"summary": "generated"}),
        ]
        instructions = "Create or update exactly these workspace files: public/images/art/, public/images/thumbs/.\n"
        def completion(**kwargs):
            return script.pop(0)
        def generate(workspace_dir, **args):
            return "Successfully generated " + args["output_path"]
        with tempfile.TemporaryDirectory() as temp:
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(comfy_tools, "generate_local_asset", generate), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                worker_sdk.run(instructions, kind="writing")
            self.assertEqual(json.loads(output.getvalue())["artifact"]["data"], "generated")


class BatchGeneration(unittest.TestCase):
    def batch(self, items, fail=()):
        calls = []
        def generate(prompt, output_path, workspace_dir, **options):
            calls.append((output_path, options))
            if output_path in fail:
                raise ValueError("boom " + output_path)
            return "ok"
        with patch.object(comfy_tools, "_generate_local_asset", generate), \
                patch.object(comfy_tools, "local_gpu_session", lambda name: nullcontext()), \
                patch.object(comfy_tools, "release_comfy_models") as release:
            try:
                return comfy_tools.generate_local_assets(items, "workspace"), calls, release
            except ValueError as error:
                return error, calls, release

    def test_items_run_in_one_session_and_options_are_forwarded(self):
        items = [{"prompt": "a", "output_path": "a.png", "width": 512}, {"prompt": "b", "output_path": "b.png"}]
        result, calls, release = self.batch(items)
        self.assertEqual(json.loads(result), {"generated": ["a.png", "b.png"], "failed": []})
        self.assertEqual(calls, [("a.png", {"width": 512}), ("b.png", {})])
        release.assert_called_once()

    def test_one_failure_does_not_stop_the_rest(self):
        items = [{"prompt": "a", "output_path": "a.png"}, {"prompt": "b", "output_path": "b.png"}]
        result, _, _ = self.batch(items, fail={"a.png"})
        report = json.loads(result)
        self.assertEqual(report["generated"], ["b.png"])
        self.assertEqual(report["failed"], [{"output_path": "a.png", "error": "boom a.png"}])

    def test_total_failure_is_an_error_result(self):
        result, _, _ = self.batch([{"prompt": "a", "output_path": "a.png"}], fail={"a.png"})
        self.assertTrue(result.startswith("Error"))

    def test_invalid_batches_are_rejected_before_any_generation(self):
        too_many = [{"prompt": "p", "output_path": f"{i}.png"} for i in range(comfy_tools.MAX_BATCH_ASSETS + 1)]
        for items in ([], "x", too_many, [{"prompt": "p"}], [{"prompt": "p", "output_path": "a"}, {"prompt": "q", "output_path": "a"}]):
            result, calls, _ = self.batch(items)
            self.assertIsInstance(result, ValueError)
            self.assertEqual(calls, [])

    def test_worker_counts_only_generated_paths_toward_declared_outputs(self):
        script = [
            tool_response("generate_local_assets", {"items": [{"prompt": "a", "output_path": "public/images/art/a.png"}, {"prompt": "t", "output_path": "public/images/thumbs/t.png"}]}),
            tool_response("mark_task_complete", {"summary": "batch"}),
        ]
        instructions = "Create or update exactly these workspace files: public/images/art/, public/images/thumbs/.\n"
        def completion(**kwargs):
            return script.pop(0)
        report = json.dumps({"generated": ["public/images/art/a.png", "public/images/thumbs/t.png"], "failed": []})
        with tempfile.TemporaryDirectory() as temp:
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(comfy_tools, "generate_local_assets", lambda workspace_dir, **args: report), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                worker_sdk.run(instructions, kind="writing")
            self.assertEqual(json.loads(output.getvalue())["artifact"]["data"], "batch")


if __name__ == "__main__":
    unittest.main()
