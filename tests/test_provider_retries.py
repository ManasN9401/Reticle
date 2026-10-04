import io
import json
from pathlib import Path
import sys
import tempfile
import threading
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import comfy_tools
import worker_sdk


class Timeout(Exception):
    """litellm.Timeout, as raised for a gateway 504."""


class InternalServerError(Exception):
    pass


class APIConnectionError(Exception):
    pass


class BadRequestError(Exception):
    pass


GATEWAY_TIMEOUT = Timeout("litellm.Timeout: Timeout Error: OpenAIException - Error code: 504")


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class TransientClassification(unittest.TestCase):
    def test_gateway_and_connection_failures_are_transient_on_a_bounded_schedule(self):
        for error in (GATEWAY_TIMEOUT, APIConnectionError("connection reset"), TimeoutError("did not start"),
                      InternalServerError("Error code: 504"), InternalServerError("503 Service Unavailable"),
                      InternalServerError("502 Bad Gateway")):
            self.assertEqual(worker_sdk._transient_wait(error, 0), 5, repr(error))
            self.assertEqual(worker_sdk._transient_wait(error, 1), 15, repr(error))
            self.assertIsNone(worker_sdk._transient_wait(error, 2), repr(error))

    def test_other_failures_are_not_treated_as_transient(self):
        for error in (BadRequestError("invalid message provided at index 504"), InternalServerError("Error code: 500"),
                      ValueError("Error code: 404"), BadRequestError("Error code: 400")):
            self.assertIsNone(worker_sdk._transient_wait(error, 0), repr(error))


class StartTimeout(unittest.TestCase):
    def test_a_provider_that_never_starts_is_given_up_on(self):
        release = threading.Event()
        self.addCleanup(release.set)
        with self.assertRaises(TimeoutError) as caught:
            worker_sdk._start_within(lambda **request: release.wait(5), 0.05)
        # The wording must match the dispatcher's timeout markers so it can reroute.
        self.assertIn("did not start responding", str(caught.exception))
        self.assertIn("timed out", str(caught.exception))

    def test_results_and_errors_pass_through(self):
        self.assertEqual(worker_sdk._start_within(lambda **request: request["x"], 1, x=7), 7)
        boom = BadRequestError("nope")
        with self.assertRaises(BadRequestError) as caught:
            worker_sdk._start_within(lambda **request: (_ for _ in ()).throw(boom), 1)
        self.assertIs(caught.exception, boom)


class WorkerPolicy(unittest.TestCase):
    def run_worker(self, script):
        script = list(script)

        def completion(**kwargs):
            item = script.pop(0)
            if isinstance(item, Exception):
                raise item
            return item

        sleeps = []
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
            summary = json.loads(output.getvalue())["artifact"]["data"] if output.getvalue() else None
            return summary, sleeps, failure, errors.getvalue(), script

    def finish(self):
        return [tool_response("read_file", {"path": "report.md"}), tool_response("mark_task_complete", {"summary": "done"})]

    def test_after_files_were_written_a_gateway_timeout_is_waited_out(self):
        script = [tool_response("write_file", {"path": "report.md", "content": "x"}), GATEWAY_TIMEOUT] + self.finish()
        summary, sleeps, failure, errors, _ = self.run_worker(script)
        self.assertIsNone(failure)
        self.assertEqual(summary, "done")
        self.assertEqual(sleeps, [5])
        self.assertIn("Provider unavailable (Timeout)", errors)

    def test_a_persistent_outage_after_writes_stops_after_the_bounded_retries(self):
        script = [tool_response("write_file", {"path": "report.md", "content": "x"}), GATEWAY_TIMEOUT, GATEWAY_TIMEOUT, GATEWAY_TIMEOUT]
        _, sleeps, failure, errors, remaining = self.run_worker(script)
        self.assertIs(failure, GATEWAY_TIMEOUT)
        self.assertEqual(sleeps, [5, 15])
        self.assertEqual(remaining, [])
        # Files were written, so the dispatcher must not replay this worker.
        self.assertNotIn("[RETICLE_RETRY_SAFE: NO_EFFECTS]", errors)

    def test_before_any_change_the_error_goes_to_the_dispatcher_to_reroute(self):
        _, sleeps, failure, errors, remaining = self.run_worker([GATEWAY_TIMEOUT] + self.finish())
        self.assertIs(failure, GATEWAY_TIMEOUT)
        self.assertEqual(sleeps, [])
        self.assertEqual(len(remaining), 2)
        self.assertIn("[RETICLE_RETRY_SAFE: NO_EFFECTS]", errors)

    def test_a_request_the_provider_rejects_is_never_repeated(self):
        script = [tool_response("write_file", {"path": "report.md", "content": "x"}), BadRequestError("Error code: 400")]
        _, sleeps, failure, _, _ = self.run_worker(script)
        self.assertIsInstance(failure, BadRequestError)
        self.assertEqual(sleeps, [])


if __name__ == "__main__":
    unittest.main()
