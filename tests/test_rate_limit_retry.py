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
    def __init__(self, message="429 too many requests", retry_after=None):
        super().__init__(message)
        self.response = SimpleNamespace(headers={"retry-after": retry_after} if retry_after else {})


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class RateLimitRetry(unittest.TestCase):
    def run_worker(self, script, instructions="Create or update exactly these workspace files: report.md.\n"):
        script = list(script)
        def completion(**kwargs):
            item = script.pop(0)
            if isinstance(item, Exception):
                raise item
            return item
        sleeps = []
        with tempfile.TemporaryDirectory() as temp:
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(worker_sdk.time, "sleep", side_effect=sleeps.append), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                try:
                    worker_sdk.run(instructions, kind="writing")
                    return json.loads(output.getvalue()), sleeps, None
                except Exception as error:
                    return None, sleeps, error

    def test_rate_limit_after_a_write_waits_and_continues_without_replaying_it(self):
        writes = [tool_response("write_file", {"path": "report.md", "content": "work"})]
        script = writes + [RateLimitError(), tool_response("read_file", {"path": "report.md"}), tool_response("mark_task_complete", {"summary": "done"})]
        result, sleeps, error = self.run_worker(script)
        self.assertIsNone(error)
        self.assertEqual(sleeps, [10])
        self.assertEqual(result["artifact"]["data"], "done")

    def test_retry_after_header_sets_the_wait(self):
        script = [RateLimitError(retry_after="7"), tool_response("write_file", {"path": "report.md", "content": "x"}), tool_response("read_file", {"path": "report.md"}), tool_response("mark_task_complete", {"summary": "ok"})]
        _, sleeps, error = self.run_worker(script)
        self.assertIsNone(error)
        self.assertEqual(sleeps, [7.0])

    def test_waits_are_bounded_then_the_original_error_surfaces(self):
        _, sleeps, error = self.run_worker([RateLimitError() for _ in range(4)])
        self.assertIsInstance(error, RateLimitError)
        self.assertEqual(sleeps, [10, 20, 40])

    def test_quota_and_other_errors_are_not_waited_on(self):
        for failure in (RateLimitError("RateLimitError: openrouter_free_tier_daily"), RateLimitError("insufficient balance"), RateLimitError(retry_after="3600"), ValueError("429 too many requests")):
            _, sleeps, error = self.run_worker([failure])
            self.assertIs(error, failure)
            self.assertEqual(sleeps, [])

    GROQ_TPM = (
        "GroqException - {\"error\":{\"message\":\"Rate limit reached for model `openai/gpt-oss-20b` in organization "
        "`org_x` service tier `on_demand` on tokens per minute (TPM): Limit 8000, Used 4314, Requested 4803. "
        "Please try again in 8.377499999s. Need more tokens? Upgrade to Dev Tier today at "
        "https://console.groq.com/settings/billing\",\"type\":\"tokens\",\"code\":\"rate_limit_exceeded\"}}"
    )

    def test_a_per_minute_limit_whose_message_links_to_billing_is_still_waited_on(self):
        # The upgrade link once matched a "billing" marker and the worker gave up.
        error = RateLimitError(self.GROQ_TPM)
        self.assertAlmostEqual(worker_sdk._rate_limit_wait(error, 0), 9.3775, places=3)
        script = [error, tool_response("write_file", {"path": "report.md", "content": "x"}), tool_response("read_file", {"path": "report.md"}), tool_response("mark_task_complete", {"summary": "ok"})]
        result, sleeps, failure = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertEqual(len(sleeps), 1)
        self.assertAlmostEqual(sleeps[0], 9.3775, places=3)

    def test_the_providers_own_wait_is_parsed_in_every_unit(self):
        for text, expected in (("try again in 250ms", 2.0), ("try again in 1m2.5s", 63.5), ("Try again in 30s.", 31.0)):
            self.assertAlmostEqual(worker_sdk._rate_limit_wait(RateLimitError(text), 0), expected, places=3, msg=text)

    def test_daily_limits_reported_in_minutes_or_hours_are_not_waited_on(self):
        for text in ("tokens per day (TPD) ... Please try again in 12m5s.", "try again in 2h3m"):
            self.assertIsNone(worker_sdk._rate_limit_wait(RateLimitError(text), 0), text)


if __name__ == "__main__":
    unittest.main()
