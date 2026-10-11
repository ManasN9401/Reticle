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


class TurnBudget(unittest.TestCase):
    def test_the_default_grows_with_the_files_a_node_must_produce(self):
        limit = worker_sdk._iteration_limit
        self.assertEqual(limit({}, set(), 0), 30)
        self.assertEqual(limit({}, set(), 3), 30)      # 12 + 12 is below the floor
        self.assertEqual(limit({}, set(), 5), 32)      # the node that ran out two turns short
        self.assertEqual(limit({}, set(), 12), 60)
        self.assertEqual(limit({}, {"image.local"}, 5), 60)
        self.assertEqual(limit({}, {"image.local"}, 20), 92)

    def test_an_explicit_setting_still_wins_and_the_cap_holds(self):
        limit = worker_sdk._iteration_limit
        self.assertEqual(limit({"llm_max_iterations": 10}, set(), 5), 10)
        self.assertEqual(limit({}, set(), 100), 200)
        self.assertEqual(limit({"llm_max_iterations": "many"}, set(), 5), 32)


class LoopsAfterFileWrites(unittest.TestCase):
    def run_worker(self, script, instructions="Create or update exactly these workspace files: out.js.\n", unsafe=False):
        script = list(script)

        def completion(**kwargs):
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output, errors = io.StringIO(), io.StringIO()
            failure = None
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(worker_sdk.toolset, "execute_terminal_command", lambda *a, **k: "Exit code: 0\nok"), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output), patch.object(sys, "stderr", errors):
                try:
                    worker_sdk.run(instructions, kind="coding")
                except BaseException as error:
                    failure = error
            return failure, errors.getvalue(), len(script)

    def test_listing_the_same_directory_over_and_over_is_stopped_after_a_write(self):
        # Observed: a node wrote its files, then listed directories nine times in a row.
        script = [tool_response("write_file", {"path": "out.js", "content": "x"})] + [tool_response("list_dir", {"path": "."})] * 6
        failure, errors, remaining = self.run_worker(script)
        self.assertIn("Agent stalled: repeated identical tool requests", str(failure))
        self.assertEqual(remaining, 3)  # stopped on the third identical round, not the sixth
        # Only files were written, so another model may pick the node up.
        self.assertIn("[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]", errors)

    def test_the_same_stop_after_a_terminal_command_carries_no_proof(self):
        script = [tool_response("execute_terminal_command", {"command": "echo hi"})] + [tool_response("list_dir", {"path": "."})] * 5
        failure, errors, _ = self.run_worker(script)
        self.assertIn("repeated identical tool requests", str(failure))
        self.assertNotIn("RETICLE_RETRY_SAFE", errors)

    def test_progress_between_identical_calls_is_not_a_loop(self):
        script = [
            tool_response("write_file", {"path": "out.js", "content": "x"}),
            tool_response("list_dir", {"path": "."}), tool_response("list_dir", {"path": "."}),
            tool_response("read_file", {"path": "out.js"}),
            tool_response("list_dir", {"path": "."}), tool_response("list_dir", {"path": "."}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        failure, _, remaining = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertEqual(remaining, 0)

    def test_a_node_with_many_outputs_gets_the_turns_it_needs(self):
        # Five declared files: five writes, five re-reads and the completion need 11 turns plus a few
        # listings; with the larger default this finishes instead of running out.
        files = ["a.js", "b.js", "c.js", "d.js", "e.js"]
        instructions = "Create or update exactly these workspace files: " + ", ".join(files) + ".\n"
        script = [tool_response("list_dir", {"path": f"d{i}"}) for i in range(21)]
        script += [tool_response("write_file", {"path": f, "content": "x"}) for f in files]
        script += [tool_response("read_file", {"path": f}) for f in files]
        script += [tool_response("mark_task_complete", {"summary": "done"})]
        # 21 distinct listings, 5 writes, 5 reads, 1 completion: 32 turns, the limit for five outputs.
        failure, _, remaining = self.run_worker(script, instructions)
        self.assertIsNone(failure)
        self.assertEqual(remaining, 0)


if __name__ == "__main__":
    unittest.main()
