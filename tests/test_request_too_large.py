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


class RateLimitError(Exception):
    pass


GROQ_TOO_LARGE = RateLimitError(
    "litellm.RateLimitError: GroqException - {\"error\":{\"message\":\"Request too large for model `openai/gpt-oss-120b` "
    "in organization `org_x` service tier `on_demand` on tokens per minute (TPM): Limit 8000, Requested 9734, "
    "please reduce your message size and try again. Need more tokens? Upgrade to Dev Tier today at "
    "https://console.groq.com/settings/billing\",\"type\":\"tokens\",\"code\":\"rate_limit_exceeded\"}}"
)


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class Compaction(unittest.TestCase):
    def test_old_long_turns_are_shortened_but_the_task_and_latest_turn_are_kept(self):
        messages = [
            {"role": "system", "content": "s" * 9000},
            {"role": "user", "content": "u" * 9000},
            {"role": "tool", "tool_call_id": "a", "content": "t" * 5000},
            {"role": "assistant", "content": "a" * 5000},
            {"role": "tool", "tool_call_id": "b", "content": "short"},
            {"role": "tool", "tool_call_id": "c", "content": "latest" * 1000},
        ]
        saved = worker_sdk._compact_messages(messages, 100)
        self.assertEqual(saved, 2 * 4900)
        self.assertEqual(len(messages[0]["content"]), 9000)
        self.assertEqual(len(messages[1]["content"]), 9000)
        self.assertTrue(messages[2]["content"].startswith("t" * 100) and "removed" in messages[2]["content"])
        self.assertEqual(messages[4]["content"], "short")
        self.assertEqual(len(messages[5]["content"]), 6000)
        self.assertEqual(worker_sdk._compact_messages(messages, 100), 0)


class TooLargeRequests(unittest.TestCase):
    def test_a_request_over_the_token_cap_is_not_waited_on(self):
        self.assertIsNone(worker_sdk._rate_limit_wait(GROQ_TOO_LARGE, 0))
        self.assertTrue(worker_sdk._is_request_too_large(GROQ_TOO_LARGE))

    def run_worker(self, script, big_history=True):
        script = list(script)
        histories, sleeps = [], []

        def completion(**kwargs):
            histories.append(copy.deepcopy(kwargs["messages"]))
            item = script.pop(0)
            if isinstance(item, Exception):
                raise item
            return item

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output, errors = io.StringIO(), io.StringIO()
            failure = None
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(worker_sdk.time, "sleep", side_effect=sleeps.append), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output), patch.object(sys, "stderr", errors):
                try:
                    worker_sdk.run("Fixture", kind="writing")
                except Exception as error:
                    failure = error
            return histories, sleeps, failure, errors.getvalue(), bool(output.getvalue())

    def test_after_files_were_written_the_history_is_shortened_and_the_run_continues(self):
        big = "x" * 6000
        script = [
            tool_response("write_file", {"path": "report.md", "content": big}),
            tool_response("read_file", {"path": "report.md"}),
            GROQ_TOO_LARGE,
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        histories, sleeps, failure, errors, finished = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertTrue(finished)
        self.assertEqual(sleeps, [])
        # The retry is the same request with the long read_file result cut down.
        rejected, retried = histories[2], histories[3]
        self.assertEqual(len(rejected), len(retried))
        self.assertGreater(sum(len(str(m.get("content"))) for m in rejected), sum(len(str(m.get("content"))) for m in retried))
        self.assertIn("Request too large for the model", errors)

    def test_before_any_change_the_error_goes_to_the_dispatcher_to_reroute(self):
        histories, sleeps, failure, errors, finished = self.run_worker([GROQ_TOO_LARGE])
        self.assertIs(failure, GROQ_TOO_LARGE)
        self.assertEqual(sleeps, [])
        self.assertEqual(len(histories), 1)
        self.assertIn("[RETICLE_RETRY_SAFE: NO_EFFECTS]", errors)

    def test_when_shortening_cannot_help_the_error_surfaces(self):
        script = [tool_response("write_file", {"path": "report.md", "content": "x"}), GROQ_TOO_LARGE, GROQ_TOO_LARGE, GROQ_TOO_LARGE]
        histories, sleeps, failure, _, finished = self.run_worker(script)
        self.assertIs(failure, GROQ_TOO_LARGE)
        self.assertEqual(sleeps, [])
        self.assertFalse(finished)


OVER_BY_498 = RateLimitError(str(GROQ_TOO_LARGE).replace("9734", "8498"))


class ReplyBudget(unittest.TestCase):
    def test_max_tokens_is_lowered_by_the_reported_overshoot(self):
        request, options = {"max_tokens": 4096}, {"max_tokens": 4096}
        change = worker_sdk._clamp_max_tokens(request, OVER_BY_498, options)
        self.assertEqual(request["max_tokens"], 4096 - 498 - 128)
        self.assertEqual(options["max_tokens"], 4096 - 498 - 128)
        self.assertIn("4096 to 3470", change)

    def test_nothing_is_lowered_when_it_cannot_help(self):
        self.assertEqual(worker_sdk._clamp_max_tokens({}, OVER_BY_498), "")
        self.assertEqual(worker_sdk._clamp_max_tokens({"max_tokens": 4096}, RateLimitError("Request too large")), "")
        # The prompt alone is over the limit: there is no reply budget left to give up.
        self.assertEqual(worker_sdk._clamp_max_tokens({"max_tokens": 300}, OVER_BY_498), "")

    def test_old_turns_are_dropped_only_at_turn_boundaries(self):
        def turn(index):
            call = {"id": f"c{index}", "type": "function", "function": {"name": "write_file", "arguments": "{}"}}
            return [{"role": "assistant", "tool_calls": [call]}, {"role": "tool", "tool_call_id": f"c{index}", "content": "ok"}]
        messages = [{"role": "system", "content": "s"}, {"role": "user", "content": "u"}]
        for index in range(5):
            messages += turn(index)
        dropped = worker_sdk._drop_old_turns(messages, "Files you have written so far: a.html.")
        self.assertIn("dropped 6 earlier messages", dropped)
        self.assertEqual([m["role"] for m in messages], ["system", "user", "user", "assistant", "tool", "assistant", "tool"])
        self.assertIn("a.html", messages[2]["content"])
        # Every tool message still follows the assistant message that called it.
        for position, message in enumerate(messages):
            if message["role"] == "tool":
                self.assertEqual(messages[position - 1]["role"], "assistant")
        self.assertEqual(worker_sdk._drop_old_turns(messages, ""), "")

    def run_worker(self, script, max_tokens):
        script = list(script)
        seen = []

        def completion(**kwargs):
            seen.append(kwargs.get("max_tokens"))
            item = script.pop(0)
            if isinstance(item, Exception):
                raise item
            return item

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            memory = {"workspace_dir": temp, "llm_max_tokens": max_tokens}
            request = {"id": "execution|node", "execution": "execution", "memory": memory, "parameters": {"llm_model": "ollama/fixture"}}
            output, errors = io.StringIO(), io.StringIO()
            failure = None
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}),                     patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True),                     patch.object(comfy_tools, "ollama_model_loaded", return_value=True),                     patch.object(sys, "stdin", io.StringIO(json.dumps(request))),                     patch.object(sys, "stdout", output), patch.object(sys, "stderr", errors):
                try:
                    worker_sdk.run("Fixture", kind="writing")
                except Exception as error:
                    failure = error
            return seen, failure, errors.getvalue(), bool(output.getvalue())

    def test_after_files_were_written_the_reply_budget_is_lowered_and_stays_lowered(self):
        script = [
            tool_response("write_file", {"path": "report.md", "content": "x"}),
            OVER_BY_498,
            tool_response("read_file", {"path": "report.md"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        seen, failure, errors, finished = self.run_worker(script, 4096)
        self.assertIsNone(failure)
        self.assertTrue(finished)
        # 4096, then the rejected call repeated at 3470, and the later turns keep 3470.
        self.assertEqual(seen, [4096, 4096, 3470, 3470])
        self.assertIn("lowered the reply budget from 4096 to 3470", errors)

    def test_before_any_change_the_dispatcher_decides_so_nothing_is_lowered(self):
        # Docs: a request rejection excludes that route and the dispatcher picks another, so
        # the worker repairs a request only once it can no longer be replayed.
        seen, failure, errors, finished = self.run_worker([OVER_BY_498, tool_response("read_file", {"path": "x"})], 4096)
        self.assertIs(failure, OVER_BY_498)
        self.assertFalse(finished)
        self.assertEqual(seen, [4096])
        self.assertIn("[RETICLE_RETRY_SAFE: NO_EFFECTS]", errors)


if __name__ == "__main__":
    unittest.main()
