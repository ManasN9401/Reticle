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
import forge_utils
import worker_sdk


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class CanonicalPaths(unittest.TestCase):
    def test_a_leading_src_is_the_workspace_folder_itself(self):
        with tempfile.TemporaryDirectory() as temp:
            root = (Path(temp) / "src").resolve()
            self.assertEqual(forge_utils.safe_path(temp, "src/js/main.js"), forge_utils.safe_path(temp, "js/main.js"))
            self.assertEqual(forge_utils.safe_path(temp, "src/js/main.js"), root / "js" / "main.js")
            self.assertEqual(forge_utils.safe_path(temp, "src"), root)
            self.assertEqual(forge_utils.safe_path(temp, "./src/a.html"), root / "a.html")
            # Only a leading src is special: a deeper src folder and similar names are real.
            self.assertEqual(forge_utils.safe_path(temp, "lib/src/a.js"), root / "lib" / "src" / "a.js")
            self.assertEqual(forge_utils.safe_path(temp, "srcs/a.js"), root / "srcs" / "a.js")

    def test_containment_checks_are_unchanged(self):
        with tempfile.TemporaryDirectory() as temp:
            for bad in ("../x", "src/../x", "/abs", "a\\b", "c:/x"):
                with self.assertRaises(ValueError, msg=bad):
                    forge_utils.safe_path(temp, bad)

    def test_literal_resolution_and_other_bases_are_left_alone(self):
        with tempfile.TemporaryDirectory() as temp:
            self.assertEqual(forge_utils.safe_path(temp, "src/a", literal=True), (Path(temp) / "src" / "src" / "a").resolve())
            self.assertEqual(forge_utils.safe_path(temp, "src/a", base=""), (Path(temp) / "src" / "a").resolve())

    def test_write_and_read_agree_whichever_spelling_is_used(self):
        with tempfile.TemporaryDirectory() as temp:
            self.assertTrue(forge_utils.write_file("src/main.js", "x", temp, {}).startswith("Successfully"))
            self.assertFalse((Path(temp) / "src" / "src").exists())
            self.assertEqual(forge_utils.read_file("main.js", temp), "x")
            self.assertEqual(forge_utils.read_file("src/main.js", temp), "x")
            self.assertEqual(forge_utils.list_dir("src", temp), "main.js")

    def test_a_session_written_before_the_change_is_still_readable(self):
        with tempfile.TemporaryDirectory() as temp:
            old = Path(temp) / "src" / "src" / "old.js"
            old.parent.mkdir(parents=True)
            old.write_text("legacy", encoding="utf-8")
            self.assertEqual(forge_utils.read_file("src/old.js", temp), "legacy")
            self.assertIn("src/old.js:1: legacy", forge_utils.search_codebase("legacy", temp))

    def test_the_sdk_tracks_one_form_for_every_spelling(self):
        for spelling in ("src/a/b.js", "./src/a/b.js", "a/b.js"):
            self.assertEqual(worker_sdk._normalized_workspace_path(spelling), "a/b.js")
        self.assertTrue(worker_sdk._output_satisfied("src/a/b.js", {"a/b.js"}))


class VerificationAndLoops(unittest.TestCase):
    def run_worker(self, script):
        script = list(script)
        told = []

        def completion(**kwargs):
            told[:] = [m["content"] for m in kwargs["messages"] if m.get("role") == "tool"]
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
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
                except BaseException as error:
                    failure = error
            summary = json.loads(output.getvalue())["artifact"]["data"] if output.getvalue() else None
            return summary, told, failure, errors.getvalue(), len(script)

    def test_reading_a_file_by_another_spelling_verifies_it(self):
        # The observed loop: write src/main.js, read main.js, and never get credit.
        script = [
            tool_response("write_file", {"path": "src/main.js", "content": "console.log(1)"}),
            tool_response("read_file", {"path": "main.js"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        summary, _, failure, _, remaining = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertEqual((summary, remaining), ("done", 0))

    def test_the_rejection_text_lists_the_unverified_files(self):
        script = [
            tool_response("write_file", {"path": "a.js", "content": "1"}),
            tool_response("write_file", {"path": "b.html", "content": "<p>"}),
            tool_response("mark_task_complete", {"summary": "premature"}),
            tool_response("read_file", {"path": "a.js"}),
            tool_response("read_file", {"path": "b.html"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        seen = []

        def completion(**kwargs):
            seen[:] = [m["content"] for m in kwargs["messages"] if m.get("role") == "tool"]
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", io.StringIO()), patch.object(sys, "stderr", io.StringIO()):
                worker_sdk.run("Fixture", kind="writing")
        rejection = next(text for text in seen if "No successful verification" in text)
        self.assertIn("a.js, b.html", rejection)
        self.assertIn("does not count", rejection)

    def test_the_same_rejection_six_times_stops_the_run_with_a_classifiable_stall(self):
        script = [tool_response("write_file", {"path": "a.js", "content": "1"})] + [tool_response("mark_task_complete", {"summary": "again"})] * 8
        _, told, failure, errors, remaining = self.run_worker(script)
        self.assertIsInstance(failure, BaseException)
        self.assertIn("Agent stalled: completion repeatedly rejected", str(failure))
        self.assertEqual(remaining, 2)  # one write and six rejected completions were consumed; two replies never used
        self.assertIn("rejection 3 of 6", "\n".join(told))
        # The file was written and nothing else, so another model may resume.
        self.assertIn("[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]", errors)

    def test_real_progress_between_rejections_resets_the_count(self):
        marks = [tool_response("mark_task_complete", {"summary": "m"})]
        script = (
            [tool_response("write_file", {"path": "a.js", "content": "1"})] + marks * 3
            + [tool_response("write_file", {"path": "b.js", "content": "2"})] + marks * 3
            + [tool_response("read_file", {"path": "a.js"}), tool_response("read_file", {"path": "b.js"}),
               tool_response("mark_task_complete", {"summary": "done"})]
        )
        summary, _, failure, _, remaining = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertEqual((summary, remaining), ("done", 0))


if __name__ == "__main__":
    unittest.main()
