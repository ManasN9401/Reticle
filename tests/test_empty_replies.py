import copy
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


def empty_reply(content=None):
    """What a runaway reasoning turn ends as: no text and no tool call."""
    message = SimpleNamespace(tool_calls=[], content=content, model_dump=lambda **kwargs: {"role": "assistant"})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


def invalid_messages(messages):
    """Messages a strict provider (Cohere, for one) rejects with a 400."""
    bad = []
    for index, message in enumerate(messages):
        if message["role"] == "assistant" and not message.get("tool_calls") and not str(message.get("content") or "").strip():
            bad.append((index, "empty assistant"))
        if message["role"] in ("user", "tool", "system") and not str(message.get("content") or "").strip():
            bad.append((index, "empty " + message["role"]))
    return bad


class EmptyReplies(unittest.TestCase):
    def run_worker(self, script, files=None):
        script = list(script)
        histories = []

        def completion(**kwargs):
            histories.append(copy.deepcopy(kwargs["messages"]))
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            for name, content in (files or {}).items():
                (Path(temp) / "src" / name).write_text(content, encoding="utf-8")
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output, errors = io.StringIO(), io.StringIO()
            failure = None
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output), patch.object(sys, "stderr", errors):
                try:
                    worker_sdk.run("Fixture", kind="writing")
                except Exception as error:
                    failure = error
            summary = json.loads(output.getvalue())["artifact"]["data"] if output.getvalue() else None
            return summary, histories, failure, errors.getvalue()

    def test_an_empty_reply_never_enters_the_history_and_the_run_recovers(self):
        script = [
            tool_response("write_file", {"path": "report.md", "content": "work"}),
            empty_reply(),
            empty_reply("   \n"),
            tool_response("read_file", {"path": "report.md"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        summary, histories, failure, _ = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertEqual(summary, "done")
        for history in histories:
            self.assertEqual(invalid_messages(history), [])
        # The model is told why it is being asked again.
        self.assertIn("no text and no tool call", histories[2][-1]["content"])

    def test_repeated_empty_replies_stop_with_a_classifiable_error(self):
        _, histories, failure, errors = self.run_worker([empty_reply(), empty_reply(), empty_reply(), empty_reply()])
        self.assertIsInstance(failure, RuntimeError)
        self.assertIn("Agent stalled: model returned empty responses", str(failure))
        self.assertEqual(len(histories), 3)
        # Nothing was changed, so the dispatcher may reroute to another model.
        self.assertIn("[RETICLE_RETRY_SAFE: NO_EFFECTS]", errors)

    def test_a_good_reply_resets_the_empty_reply_count(self):
        script = [
            empty_reply(), empty_reply(),
            tool_response("write_file", {"path": "report.md", "content": "x"}),
            empty_reply(), empty_reply(),
            tool_response("read_file", {"path": "report.md"}),
            tool_response("mark_task_complete", {"summary": "ok"}),
        ]
        summary, _, failure, _ = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertEqual(summary, "ok")

    def test_an_empty_tool_result_is_sent_with_placeholder_text(self):
        script = [
            tool_response("write_file", {"path": "notes.md", "content": "x"}),
            tool_response("read_file", {"path": "empty.txt"}),
            tool_response("read_file", {"path": "notes.md"}),
            tool_response("mark_task_complete", {"summary": "ok"}),
        ]
        summary, histories, failure, _ = self.run_worker(script, files={"empty.txt": ""})
        self.assertIsNone(failure)
        for history in histories:
            self.assertEqual(invalid_messages(history), [])
        self.assertEqual(histories[2][-1]["content"], "(no output)")


if __name__ == "__main__":
    unittest.main()
