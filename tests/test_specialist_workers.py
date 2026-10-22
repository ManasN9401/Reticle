"""The four specialist agents through the real worker loop: which tools each is offered, how the
files their tools save are reported, whether a browse that can submit forms stays replay-safe, and
Hermes's rule that it finishes only after the code has run clean.

The model is scripted and the tools that need a network or a browser are stubs; the worker, its
tool schemas, its effect tracking and its completion rules are the real ones."""
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import forge_utils
import worker_sdk
import research_tools
import browse_tools
import quant_tools


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class Done(Exception):
    pass


class Worker(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        (Path(self.temp.name) / "src").mkdir()

    def run_worker(self, kind, script, capabilities, terminal=None):
        script = list(script)
        calls = []

        def completion(**kwargs):
            calls.append(kwargs)
            if not script:
                raise Done()
            return script.pop(0)

        request = {"id": "execution|node", "execution": "execution", "capabilities": capabilities,
                   "memory": {"workspace_dir": self.temp.name}, "parameters": {"llm_model": "ollama/fixture"}}
        out, err = io.StringIO(), io.StringIO()
        patches = [
            patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}),
            patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True),
            patch.object(sys, "stdin", io.StringIO(json.dumps(request))),
            patch.object(sys, "stdout", out), patch.object(sys, "stderr", err),
        ]
        if terminal is not None:
            patches.append(patch.object(forge_utils, "execute_terminal_command", side_effect=terminal))
        for item in patches:
            item.start()
            self.addCleanup(item.stop)
        try:
            worker_sdk.run("Fixture", kind=kind)
            response = json.loads(out.getvalue())
        except Done:
            response = None
        for item in reversed(patches):
            item.stop()
        return response, calls, err.getvalue()

    def offered(self, kind, capabilities):
        # Stop at the first model request: only the tool list is wanted.
        _, calls, _ = self.run_worker(kind, [], capabilities)
        return {tool["function"]["name"] for tool in calls[0]["tools"]}


BASE = ["workspace.read", "workspace.write"]


class ToolsOffered(Worker):
    def test_the_osint_agent_gets_search_and_fetch_only_with_network_access(self):
        self.assertTrue({"web_search", "fetch_pages"} <= self.offered("osint", BASE + ["network.public"]))
        self.assertFalse({"web_search", "fetch_pages"} & self.offered("osint", BASE))
        self.assertFalse({"web_search", "fetch_pages", "browse_web"} & self.offered("coding", BASE + ["network.public"]), "a generic worker is not a researcher")

    def test_the_browser_agent_needs_both_the_browser_and_the_network(self):
        self.assertIn("browse_web", self.offered("browser", BASE + ["network.public", "browser.run"]))
        self.assertNotIn("browse_web", self.offered("browser", BASE + ["browser.run"]))
        self.assertNotIn("browse_web", self.offered("browser", BASE + ["network.public"]))
        self.assertNotIn("browse_web", self.offered("coding", BASE + ["network.public", "browser.run"]))

    def test_the_quant_agent_gets_the_market_tools(self):
        offered = self.offered("quant", BASE + ["network.public", "process.container"])
        self.assertTrue({"market_quote", "price_history", "financial_statements", "technical_indicators", "risk_metrics", "execute_terminal_command"} <= offered)
        self.assertFalse({"market_quote", "price_history"} & self.offered("coding", BASE + ["network.public"]))

    def test_the_osint_agent_has_no_terminal_to_run_arbitrary_tools(self):
        self.assertNotIn("execute_terminal_command", self.offered("osint", BASE + ["network.public"]))

    def test_tool_arguments_are_declared_with_their_optional_parts(self):
        calls = self.run_worker("osint", [], BASE + ["network.public"])[1]
        tools = {tool["function"]["name"]: tool["function"]["parameters"] for tool in calls[0]["tools"]}
        self.assertEqual(tools["web_search"]["required"], ["query"])
        self.assertEqual(tools["fetch_pages"]["required"], ["urls"])
        self.assertEqual(tools["web_search"]["properties"]["provider"]["enum"], ["serper", "brave", "searxng", "wikipedia"])


class Effects(Worker):
    def test_public_fetch_is_not_assumed_replay_safe(self):
        with patch.object(research_tools, "fetch_pages", return_value="Fetched fixture"):
            _, _, errors = self.run_worker("osint", [tool_response("fetch_pages", {"urls": ["https://example.test/"]})], BASE + ["network.public"])
        self.assertIn("[RETICLE_EFFECTS: UNSAFE]", errors)
        self.assertNotIn("[RETICLE_RETRY_SAFE: FILE_EFFECTS_ONLY]", errors)

    def stubbed_browse(self, **arguments):
        reply = "Browsed https://shop.example/\nSaved: browse/shop-example-1.md"
        with patch.object(browse_tools, "browse_web", return_value=reply):
            return self.run_worker("browser", [tool_response("browse_web", dict({"url": "https://shop.example/"}, **arguments))],
                                   BASE + ["network.public", "browser.run"])

    def test_a_public_browse_is_not_assumed_replay_safe(self):
        _, _, err = self.stubbed_browse()
        self.assertIn("[RETICLE_EFFECTS: UNSAFE]", err)

    def test_a_browse_that_may_submit_forms_is_not_replay_safe(self):
        _, _, err = self.stubbed_browse(allow_submit=True)
        self.assertIn("[RETICLE_EFFECTS: UNSAFE]", err)

    def test_the_files_the_tools_save_are_reported_as_outputs_of_the_node(self):
        fetched = ("Fetched 1 of 1 page(s). Cite sources by id (S#); the index is research/sources.json.\n"
                   "S1 https://a.test/\n  title: A\n  saved: research/sources/S1-a-test-a.md\n")
        script = [
            tool_response("fetch_pages", {"urls": ["https://a.test/"]}),
            tool_response("write_file", {"path": "report.md", "content": "# Report [S1]"}),
            tool_response("read_file", {"path": "report.md"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        with patch.object(research_tools, "fetch_pages", return_value=fetched):
            response, _, _ = self.run_worker("osint", script, BASE + ["network.public"])
        self.assertEqual(response["files"], ["report.md", "research/sources.json", "research/sources/S1-a-test-a.md"])

    def test_a_market_tool_result_that_failed_reports_no_files(self):
        script = [tool_response("price_history", {"symbol": "ZZZ"}), tool_response("mark_task_complete", {"summary": "x"})]
        with patch.object(quant_tools, "price_history", return_value="Error: no price data"):
            response, calls, _ = self.run_worker("quant", script, BASE + ["network.public"])
        self.assertIsNone(response, "nothing was written or checked, so completion was refused and the script ran dry")


class Hermes(Worker):
    CAPS = BASE + ["process.container"]

    def terminal(self, outputs):
        outputs = list(outputs)
        commands = []

        def run(command, workspace, allow_native=False, allow_network=False, allow_workspace_write=True):
            commands.append(command)
            return outputs.pop(0)
        run.commands = commands
        return run

    def tool_texts(self, calls):
        # The worker appends to one message list, so the last request holds the whole conversation.
        return [m["content"] for m in calls[-1]["messages"] if m.get("role") == "tool"]

    last_tool_messages = tool_texts

    def test_it_cannot_finish_without_running_anything(self):
        script = [
            tool_response("write_file", {"path": "calc.py", "content": "def add(a, b):\n    return a + b\n"}),
            tool_response("read_file", {"path": "calc.py"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        response, calls, _ = self.run_worker("hermes", script, self.CAPS, terminal=self.terminal([]))
        self.assertIsNone(response)
        self.assertIn("Hermes finishes only when the code has been run and passed", self.last_tool_messages(calls)[-1])
        self.assertIn("You have not run anything yet", self.last_tool_messages(calls)[-1])

    def test_the_whole_loop_fails_reads_fixes_and_passes(self):
        run = self.terminal([
            "Exit code: 1\nFAIL: test_add (test_calc.TestAdd)\nAssertionError: 4 != 5\n",
            "Exit code: 0\nRan 1 test in 0.001s\n\nOK\n",
        ])
        script = [
            tool_response("write_file", {"path": "test_calc.py", "content": "import unittest\nfrom calc import add\nclass TestAdd(unittest.TestCase):\n    def test_add(self):\n        self.assertEqual(add(2, 3), 5)\n"}),
            tool_response("write_file", {"path": "calc.py", "content": "def add(a, b):\n    return a * b\n"}),
            tool_response("execute_terminal_command", {"command": "python -m unittest -v"}),
            tool_response("replace_file_content", {"path": "calc.py", "target_content": "a * b", "replacement_content": "a + b"}),
            tool_response("execute_terminal_command", {"command": "python -m unittest -v"}),
            tool_response("mark_task_complete", {"summary": "add works; 1 test passes"}),
        ]
        response, calls, _ = self.run_worker("hermes", script, self.CAPS, terminal=run)
        self.assertIsNotNone(response, "the node finished once the run was clean")
        self.assertEqual(sorted(response["files"]), ["calc.py", "test_calc.py"])
        self.assertEqual(run.commands, ["python -m unittest -v", "python -m unittest -v"])
        failed_run = next(text for text in self.tool_texts(calls) if text.startswith("Exit code: 1"))
        self.assertIn("[hermes] Run failed (exit 1), failure 1.", failed_run)
        self.assertIn("Read the error from the bottom up", failed_run)
        self.assertTrue(any(text.startswith("Exit code: 0") and "[hermes]" not in text for text in self.tool_texts(calls)), "a clean run gets no nudge")

    def test_a_change_after_a_clean_run_has_to_be_run_again(self):
        run = self.terminal(["Exit code: 0\nOK\n"])
        script = [
            tool_response("write_file", {"path": "calc.py", "content": "x = 1\n"}),
            tool_response("execute_terminal_command", {"command": "python calc.py"}),
            tool_response("replace_file_content", {"path": "calc.py", "target_content": "x = 1", "replacement_content": "x = 2"}),
            tool_response("read_file", {"path": "calc.py"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        response, calls, _ = self.run_worker("hermes", script, self.CAPS, terminal=run)
        self.assertIsNone(response)
        self.assertIn("Nothing has run clean since your last change", self.last_tool_messages(calls)[-1])

    def test_the_same_error_three_times_is_called_out(self):
        same = "Exit code: 1\nTraceback (most recent call last):\n  File \"calc.py\", line 3, in add\nTypeError: unsupported operand type(s) for +: 'int' and 'NoneType'\n"
        run = self.terminal([same.replace("line 3", "line 3"), same.replace("line 3", "line 4"), same.replace("line 3", "line 9")])
        script = [tool_response("write_file", {"path": "calc.py", "content": "def add(a, b):\n    return a + b\n"})]
        script += [tool_response("execute_terminal_command", {"command": command}) for command in ("python calc.py", "python calc.py 1", "python calc.py 2")]
        response, calls, _ = self.run_worker("hermes", script, self.CAPS, terminal=run)
        messages = self.tool_texts(calls)
        self.assertIn("failure 1", messages[1])
        self.assertNotIn("appeared", messages[1])
        self.assertIn("failure 2", messages[2])
        self.assertNotIn("appeared", messages[2])
        self.assertIn("failure 3", messages[3])
        self.assertIn("appeared 3 times", messages[3], "line numbers differ between runs, the error is the same")
        self.assertIn("Stop patching that spot", messages[3])

    def test_an_unavailable_terminal_does_not_trap_the_node_but_the_summary_must_say_so(self):
        run = self.terminal(["Error executing command: Docker is not installed or not on PATH; commands need a container unless native execution is enabled"])
        script = [
            tool_response("write_file", {"path": "calc.py", "content": "x = 1\n"}),
            tool_response("execute_terminal_command", {"command": "python calc.py"}),
            tool_response("read_file", {"path": "calc.py"}),
            tool_response("mark_task_complete", {"summary": "written but not run"}),
        ]
        response, calls, _ = self.run_worker("hermes", script, self.CAPS, terminal=run)
        self.assertIsNotNone(response, "completion is allowed when nothing can be run")
        broken = next(text for text in self.tool_texts(calls) if text.startswith("Error executing command"))
        self.assertIn("nothing can be verified by running", broken)
        self.assertIn("the code was not run", broken)

    def test_an_inspection_command_is_not_a_run(self):
        run = self.terminal(["Exit code: 0\ncalc.py\n"])
        script = [
            tool_response("write_file", {"path": "calc.py", "content": "x = 1\n"}),
            tool_response("execute_terminal_command", {"command": "ls"}),
            tool_response("read_file", {"path": "calc.py"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]
        response, calls, _ = self.run_worker("hermes", script, self.CAPS, terminal=run)
        self.assertIsNone(response)
        self.assertIn("Hermes finishes only", self.last_tool_messages(calls)[-1])


if __name__ == "__main__":
    unittest.main()
