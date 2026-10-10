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


class QuotaError(Exception):
    pass


FAIL = QuotaError("Rate limit exceeded: free-models-per-day")


class RetryProof(unittest.TestCase):
    def run_worker(self, script, memory=None):
        script = list(script)
        seen = []

        def completion(**kwargs):
            seen.append(kwargs["messages"])
            item = script.pop(0)
            if isinstance(item, Exception):
                raise item
            return item

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp, **(memory or {})}, "parameters": {"llm_model": "ollama/fixture"}}
            output, errors = io.StringIO(), io.StringIO()
            failure = None
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(comfy_tools, "generate_local_asset", lambda workspace_dir, **a: "Successfully generated " + a["output_path"]), \
                    patch.object(worker_sdk.toolset, "execute_terminal_command", lambda *a, **k: "Exit code: 0\nok"), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output), patch.object(sys, "stderr", errors):
                try:
                    worker_sdk.run("Fixture", kind="writing")
                except Exception as error:
                    failure = error
            return seen, failure, errors.getvalue()

    def marker(self, text):
        return [name for name in ("NO_EFFECTS", "FILE_EFFECTS_ONLY") if f"[RETICLE_RETRY_SAFE: {name}]" in text]

    def test_no_effects_gives_the_original_proof(self):
        _, failure, errors = self.run_worker([FAIL])
        self.assertIs(failure, FAIL)
        self.assertEqual(self.marker(errors), ["NO_EFFECTS"])

    def test_file_writes_and_images_give_the_file_only_proof(self):
        for tool in (("write_file", {"path": "a.txt", "content": "x"}),
                     ("generate_local_asset", {"prompt": "p", "output_path": "a.png"}),
                     ("generate_local_assets", {"items": [{"prompt": "p", "output_path": "a.png"}]})):
            _, failure, errors = self.run_worker([tool_response(*tool), FAIL])
            self.assertIs(failure, FAIL, tool[0])
            self.assertEqual(self.marker(errors), ["FILE_EFFECTS_ONLY"], tool[0])

    def test_a_terminal_command_is_never_replayable(self):
        _, failure, errors = self.run_worker([
            tool_response("write_file", {"path": "a.txt", "content": "x"}),
            tool_response("execute_terminal_command", {"command": "echo hi"}),
            FAIL,
        ])
        self.assertIs(failure, FAIL)
        self.assertEqual(self.marker(errors), [])

    def test_one_unsafe_effect_keeps_the_proof_withdrawn_even_after_more_file_writes(self):
        _, failure, errors = self.run_worker([
            tool_response("execute_terminal_command", {"command": "echo hi"}),
            tool_response("write_file", {"path": "a.txt", "content": "x"}),
            FAIL,
        ])
        self.assertEqual(self.marker(errors), [])

    def test_a_resumed_attempt_is_told_to_build_on_the_existing_files(self):
        seen, _, _ = self.run_worker([FAIL], memory={"resumed_after_files": True})
        system = seen[0][0]["content"]
        self.assertIn("This task is being resumed", system)
        self.assertIn("list and read the existing files first", system)

    def test_a_first_attempt_gets_no_resume_note_and_the_flag_is_not_shown_as_a_fact(self):
        seen, _, _ = self.run_worker([FAIL])
        self.assertNotIn("being resumed", seen[0][0]["content"])
        seen, _, _ = self.run_worker([FAIL], memory={"resumed_after_files": True})
        self.assertNotIn("resumed_after_files", seen[0][1]["content"])


if __name__ == "__main__":
    unittest.main()
