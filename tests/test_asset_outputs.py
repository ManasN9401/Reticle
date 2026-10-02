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


if __name__ == "__main__":
    unittest.main()
