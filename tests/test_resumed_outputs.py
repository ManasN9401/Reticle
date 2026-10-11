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


def text_response(text):
    message = SimpleNamespace(tool_calls=None, content=text, model_dump=lambda **kwargs: {"role": "assistant", "content": text})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class Harness(unittest.TestCase):
    def run_worker(self, script, instructions, existing=None, resumed=True):
        script = list(script)
        self.told = []

        def completion(**kwargs):
            self.told = [m["content"] for m in kwargs["messages"] if m.get("role") == "tool"]
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            src = Path(temp) / "src"
            src.mkdir()
            for name, body in (existing or {}).items():
                (src / name).parent.mkdir(parents=True, exist_ok=True)
                (src / name).write_text(body, encoding="utf-8")
            memory = {"workspace_dir": temp}
            if resumed:
                memory["resumed_after_files"] = True
            request = {"id": "execution|node", "execution": "execution", "memory": memory, "parameters": {"llm_model": "ollama/fixture"}}
            output, errors = io.StringIO(), io.StringIO()
            failure = None
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output), patch.object(sys, "stderr", errors):
                try:
                    worker_sdk.run(instructions, kind="coding")
                except BaseException as error:
                    failure = error
            return failure, errors.getvalue(), len(script)


class ResumedOutputs(Harness):
    INSTRUCTIONS = "Create or update exactly these workspace files: core/Scene.js.\n"

    def test_an_existing_output_that_is_read_back_completes_the_node(self):
        # Observed on a resumed node: write_file said "File exists, read it, then call
        # mark_task_complete", and mark_task_complete said the file was not created.
        script = [
            tool_response("write_file", {"path": "core/Scene.js", "content": "new"}),
            tool_response("read_file", {"path": "core/Scene.js"}),
            tool_response("mark_task_complete", {"summary": "verified the earlier work"}),
        ]
        failure, _, remaining = self.run_worker(script, self.INSTRUCTIONS, existing={"core/Scene.js": "old"})
        self.assertIsNone(failure)
        self.assertEqual(remaining, 0)

    def test_an_existing_output_that_was_never_read_is_still_refused(self):
        script = [tool_response("mark_task_complete", {"summary": "done"})] * 8
        failure, _, _ = self.run_worker(script, self.INSTRUCTIONS, existing={"core/Scene.js": "old"})
        self.assertIn("completion repeatedly rejected", str(failure))
        # The rejection says how to satisfy it, not only to write a file that cannot be written.
        self.assertTrue(any("already exists, read it" in text for text in self.told))

    def test_reading_an_unrelated_file_does_not_count(self):
        script = [tool_response("read_file", {"path": "other.js"})] + [tool_response("mark_task_complete", {"summary": "done"})] * 8
        failure, _, _ = self.run_worker(script, self.INSTRUCTIONS, existing={"core/Scene.js": "old", "other.js": "x"})
        self.assertIn("completion repeatedly rejected", str(failure))

    def test_a_directory_output_is_satisfied_by_reading_a_file_inside_it(self):
        script = [
            tool_response("read_file", {"path": "core/Scene.js"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        failure, _, remaining = self.run_worker(script, "Create or update exactly these workspace files: core.\n", existing={"core/Scene.js": "old"})
        self.assertIsNone(failure)
        self.assertEqual(remaining, 0)

    def test_the_resume_note_tells_the_model_that_reading_counts(self):
        seen = []

        def completion(**kwargs):
            seen.append(kwargs["messages"][0]["content"])
            return tool_response("read_file", {"path": "core/Scene.js"})

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src" / "core").mkdir(parents=True)
            (Path(temp) / "src" / "core" / "Scene.js").write_text("x", encoding="utf-8")
            request = {"id": "e|n", "execution": "e", "memory": {"workspace_dir": temp, "resumed_after_files": True}, "parameters": {"llm_model": "ollama/fixture"}}
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", io.StringIO()), patch.object(sys, "stderr", io.StringIO()):
                try:
                    worker_sdk.run(self.INSTRUCTIONS, kind="coding")
                except BaseException:
                    pass
        self.assertIn("counts as created once you have read it back", seen[0])


class TextOnlyReplies(Harness):
    INSTRUCTIONS = "Create or update exactly these workspace files: a.js.\n"

    def test_a_model_that_only_prints_code_is_stopped_after_four_replies(self):
        script = [text_response("```js\nconsole.log(1)\n```")] * 8
        failure, errors, remaining = self.run_worker(script, self.INSTRUCTIONS, resumed=False)
        self.assertIn("Agent stalled: model replied with text but no tool call", str(failure))
        self.assertEqual(remaining, 4)

    def test_a_tool_call_resets_the_count(self):
        script = [
            text_response("thinking"), text_response("thinking"), text_response("thinking"),
            tool_response("write_file", {"path": "a.js", "content": "1"}),
            text_response("thinking"), text_response("thinking"), text_response("thinking"),
            tool_response("read_file", {"path": "a.js"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        failure, _, remaining = self.run_worker(script, self.INSTRUCTIONS, resumed=False)
        self.assertIsNone(failure)
        self.assertEqual(remaining, 0)

    def test_the_stop_after_a_file_write_carries_the_resumable_marker(self):
        script = [tool_response("write_file", {"path": "a.js", "content": "1"})] + [text_response("here is more code")] * 6
        failure, errors, _ = self.run_worker(script, self.INSTRUCTIONS, resumed=False)
        self.assertIn("model replied with text but no tool call", str(failure))
        self.assertIn("[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]", errors)


class EffectProgress(Harness):
    """A worker that is killed never prints its exit marker; its progress lines are all the
    dispatcher has to decide whether the node may be resumed on another route."""
    INSTRUCTIONS = "Create or update exactly these workspace files: a.js.\n"

    def progress(self, errors):
        return [line for line in errors.splitlines() if line.startswith("[RETICLE_EFFECTS:")]

    def test_state_is_announced_at_start_and_when_files_change(self):
        script = [
            tool_response("list_dir", {"path": "."}),
            tool_response("write_file", {"path": "a.js", "content": "1"}),
            tool_response("write_file", {"path": "b.js", "content": "2"}),
            tool_response("read_file", {"path": "a.js"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        _, errors, _ = self.run_worker(script, self.INSTRUCTIONS, resumed=False)
        # Once per change, not once per tool call.
        self.assertEqual(self.progress(errors), ["[RETICLE_EFFECTS: NONE]", "[RETICLE_EFFECTS: FILES]"])

    def test_a_command_that_cannot_be_replayed_is_announced_as_unsafe(self):
        script = [
            tool_response("write_file", {"path": "a.js", "content": "1"}),
            tool_response("execute_terminal_command", {"command": "echo hi"}),
            tool_response("read_file", {"path": "a.js"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        with patch.object(worker_sdk.toolset, "execute_terminal_command", return_value="Exit code: 0\nhi"):
            _, errors, _ = self.run_worker(script, self.INSTRUCTIONS, resumed=False)
        self.assertEqual(self.progress(errors), ["[RETICLE_EFFECTS: NONE]", "[RETICLE_EFFECTS: FILES]", "[RETICLE_EFFECTS: UNSAFE]"])

    def test_a_node_that_changes_nothing_announces_only_the_start(self):
        script = [tool_response("list_dir", {"path": "."})] + [text_response("no tools")] * 6
        _, errors, _ = self.run_worker(script, self.INSTRUCTIONS, resumed=False)
        self.assertEqual(self.progress(errors), ["[RETICLE_EFFECTS: NONE]"])


if __name__ == "__main__":
    unittest.main()
