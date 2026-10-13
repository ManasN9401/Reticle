import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import browser_tools
import comfy_tools
import worker_sdk


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


def run_worker(script, capabilities, files=None, fake_report=None):
    script = list(script)
    offered, told = [], []

    def completion(**kwargs):
        offered[:] = [tool["function"]["name"] for tool in kwargs.get("tools", [])]
        told[:] = [m["content"] for m in kwargs["messages"] if m.get("role") == "tool"]
        return script.pop(0)

    with tempfile.TemporaryDirectory() as temp:
        src = Path(temp) / "src"
        src.mkdir()
        for name, content in (files or {}).items():
            (src / name).parent.mkdir(parents=True, exist_ok=True)
            (src / name).write_text(content, encoding="utf-8")
        request = {"id": "execution|node", "execution": "execution", "capabilities": capabilities,
                   "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
        output, errors = io.StringIO(), io.StringIO()
        patches = [
            patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}),
            patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}),
            patch.object(comfy_tools, "ollama_model_loaded", return_value=True),
            patch.object(sys, "stdin", io.StringIO(json.dumps(request))),
            patch.object(sys, "stdout", output), patch.object(sys, "stderr", errors),
        ]
        if fake_report is not None:
            patches.append(patch.object(browser_tools, "run_page", lambda workspace_dir, **args: fake_report))
        failure = None
        for item in patches:
            item.start()
        try:
            worker_sdk.run("Fixture instructions.\n", kind="coding")
        except BaseException as error:
            failure = error
        finally:
            for item in reversed(patches):
                item.stop()
        response = json.loads(output.getvalue()) if output.getvalue().strip().startswith("{") else None
        shot_exists = [(src / "run_page").is_dir() and sorted(p.name for p in (src / "run_page").iterdir())][0]
        return failure, errors.getvalue(), offered, told, response, shot_exists


CAPABILITIES = ["workspace.read", "workspace.write", "browser.run"]
FAKE = "Ran index.html in a headless browser (fake).\nScreenshot: run_page/index-1.png\nResult: NO PROBLEMS FOUND."


class Registration(unittest.TestCase):
    def test_the_tool_is_offered_only_with_the_capability(self):
        script = [tool_response("list_dir", {"path": "."})] * 4
        _, _, offered, _, _, _ = run_worker(script, CAPABILITIES)
        self.assertIn("run_page", offered)
        _, _, offered, _, _, _ = run_worker(script, ["workspace.read", "workspace.write"])
        self.assertNotIn("run_page", offered)
        self.assertIn("check_web_page", offered)

    def test_the_architect_catalogue_describes_the_same_tool_name(self):
        text = (Path(__file__).resolve().parents[1] / "cmd/forge/architect_catalog.go").read_text(encoding="utf-8")
        self.assertIn('ID: "run_page", Capability: "browser.run"', text)


class Effects(unittest.TestCase):
    def test_the_screenshot_is_reported_as_a_file_and_the_node_stays_replayable(self):
        script = [tool_response("run_page", {})] + [tool_response("list_dir", {"path": "."})] * 5
        failure, errors, _, told, _, _ = run_worker(script, CAPABILITIES, fake_report=FAKE)
        self.assertIn("repeated identical tool requests", str(failure))
        # A new screenshot is a replay-safe file effect, like a generated image: resumable.
        self.assertIn("[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]", errors)
        self.assertNotIn("[RETICLE_EFFECTS: UNSAFE]", errors)
        self.assertTrue(any("NO PROBLEMS FOUND" in text for text in told))

    def test_arguments_are_validated_before_the_browser_starts(self):
        script = [tool_response("run_page", {"wait_ms": "soon"}), tool_response("run_page", {"colour": "red"})] + [tool_response("list_dir", {"path": "."})] * 4
        _, _, _, told, _, _ = run_worker(script, CAPABILITIES, fake_report=FAKE)
        joined = "\n".join(told)
        self.assertIn("'wait_ms' must be integer", joined)
        self.assertIn("unknown argument 'colour'", joined)


@unittest.skipUnless(browser_tools.find_chromium(), "no Chrome or Edge installed")
class ThroughTheWorker(unittest.TestCase):
    def test_a_node_runs_its_page_for_real_and_the_screenshot_is_a_reported_file(self):
        page = "<!doctype html><title>T</title><body style='margin:0'><div style='width:100vw;height:50vh;background:#c33'></div>"
        script = [
            tool_response("read_file", {"path": "index.html"}),
            tool_response("run_page", {"wait_ms": 300}),
            tool_response("mark_task_complete", {"summary": "ran it"}),
        ]
        failure, _, _, told, response, shots = run_worker(script, CAPABILITIES, files={"index.html": page})
        self.assertIsNone(failure)
        report = next(text for text in told if text.startswith("Ran index.html"))
        self.assertIn("Result: NO PROBLEMS FOUND.", report)
        self.assertEqual(shots, ["index-1.png"])
        self.assertIn("run_page/index-1.png", response["files"])


if __name__ == "__main__":
    unittest.main()
