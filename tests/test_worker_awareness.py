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
import web_tools
import worker_sdk


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


BROKEN = "const a = 1\nexport { a, a }\n"


class Harness(unittest.TestCase):
    def run_worker(self, script, instructions, existing=None, resumed=False, terminal=None):
        script = list(script)
        seen = {"messages": []}

        def completion(**kwargs):
            seen["messages"] = list(kwargs["messages"])
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
            patches = [
                patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}),
                patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}),
                patch.object(comfy_tools, "ollama_model_loaded", return_value=True),
                patch.object(sys, "stdin", io.StringIO(json.dumps(request))),
                patch.object(sys, "stdout", output),
                patch.object(sys, "stderr", errors),
            ]
            if terminal is not None:
                patches.append(patch.object(worker_sdk.toolset, "execute_terminal_command", terminal))
            for item in patches:
                item.start()
                self.addCleanup(item.stop)
            try:
                worker_sdk.run(instructions, kind="coding")
            except BaseException as error:
                failure = error
            finally:
                for item in reversed(patches):
                    item.stop()
                    self.doCleanups() if False else None
            summary = json.loads(output.getvalue())["artifact"]["data"] if output.getvalue().strip().startswith("{") else None
            return failure, errors.getvalue(), len(script), seen, summary


class ResumedWebGate(Harness):
    PAGE = "<!doctype html><script type=\"module\" src=\"main.js\"></script>"

    def test_a_resumed_integrator_cannot_finish_without_checking_the_page_it_did_not_write(self):
        # Observed: the integration node completed on attempt 10 after nine failed attempts had written
        # the page; this attempt wrote nothing, so nothing required it to run the check.
        instructions = "Create or update exactly these workspace files: index.html, main.js.\n"
        script = [
            tool_response("read_file", {"path": "index.html"}),
            tool_response("read_file", {"path": "main.js"}),
            tool_response("mark_task_complete", {"summary": "premature: never checked"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "premature: the check listed problems"}),
            tool_response("replace_file_content", {"path": "main.js", "target_content": "export { a, a }", "replacement_content": "export { a }"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "verified and fixed"}),
        ]
        failure, errors, remaining, seen, summary = self.run_worker(
            script, instructions, existing={"index.html": self.PAGE, "main.js": BROKEN}, resumed=True
        )
        self.assertIsNone(failure)
        self.assertEqual(summary, "verified and fixed")
        self.assertEqual(remaining, 0)
        told = "\n".join(m["content"] for m in seen["messages"] if m.get("role") == "tool")
        self.assertIn("have not checked that the page can run", told)
        self.assertIn("Duplicate export", told)

    def test_a_node_that_declares_no_existing_web_output_is_not_held(self):
        instructions = "Create or update exactly these workspace files: notes.md.\n"
        script = [
            tool_response("write_file", {"path": "notes.md", "content": "# n"}),
            tool_response("read_file", {"path": "notes.md"}),
            tool_response("mark_task_complete", {"summary": "ok"}),
        ]
        failure, _, remaining, _, summary = self.run_worker(
            script, instructions, existing={"index.html": self.PAGE, "main.js": BROKEN}, resumed=True
        )
        self.assertIsNone(failure)
        self.assertEqual((summary, remaining), ("ok", 0))


class ImportMapRule(unittest.TestCase):
    def check(self, import_map):
        with tempfile.TemporaryDirectory() as temp:
            src = Path(temp) / "src"
            src.mkdir()
            (src / "index.html").write_text(
                '<script type="importmap">' + json.dumps({"imports": import_map}) + '</script><script type="module" src="main.js"></script>',
                encoding="utf-8",
            )
            (src / "main.js").write_text("import * as THREE from 'three'\nconsole.log(THREE)\n", encoding="utf-8")
            return web_tools.run_check(temp)

    def test_a_umd_build_in_the_import_map_is_blocking(self):
        # Observed: three -> .../three.js/r158/three.min.js, six modules importing it, every one broken.
        report, blocking = self.check({"three": "https://cdnjs.cloudflare.com/ajax/libs/three.js/r158/three.min.js"})
        self.assertEqual(blocking, 1, report)
        self.assertIn("UMD build", report)
        self.assertIn("three.module.js", report)

    def test_module_builds_are_accepted(self):
        for url in (
            "https://cdnjs.cloudflare.com/ajax/libs/three.js/r158/three.module.js",
            "https://cdn.jsdelivr.net/npm/three@0.158.0/build/three.module.js",
            "https://esm.sh/three@0.158.0",
        ):
            report, blocking = self.check({"three": url})
            self.assertEqual(blocking, 0, f"{url}: {report}")


class InspectionCommands(unittest.TestCase):
    def test_commands_that_only_read_are_inspection(self):
        for command in (
            "ls -la src", "cat src/main.js", "head -n 20 index.html", "grep -rn THREE src | head -5",
            "find . -name '*.js'", "node --check src/main.js", "tsc --noEmit", "git status", "git diff --stat",
            "wc -l src/*.js",
        ):
            self.assertTrue(worker_sdk._is_inspection_command(command), command)

    def test_anything_that_could_change_the_workspace_is_not(self):
        for command in (
            "", "rm -rf src", "echo hi", "npm install", "python validate.py", "node main.js", "ls > out.txt",
            "cat a && rm a", "ls; rm a", "find . -delete", "find . -exec rm {} +", "grep x f | tee out",
            "git checkout main", "git diff --output=patch.diff", "cat $(whoami)", "ls `pwd`", "node --check a.js || true",
        ):
            self.assertFalse(worker_sdk._is_inspection_command(command), command)
        self.assertFalse(worker_sdk._is_inspection_command(None))

    def test_a_node_that_only_inspects_stays_replayable(self):
        script = [
            tool_response("execute_terminal_command", {"command": "ls -la"}),
            tool_response("execute_terminal_command", {"command": "node --check main.js"}),
        ] + [tool_response("list_dir", {"path": "."})] * 4
        harness = Harness()
        failure, errors, _, _, _ = harness.run_worker(
            script, "Create or update exactly these workspace files: report.md.\n",
            terminal=lambda *a, **k: "Exit code: 0\nok",
        )
        self.assertIn("repeated identical tool requests", str(failure))
        # Stalled after nothing but inspection: still carries the no-effects proof.
        self.assertIn("[RETICLE_RETRY_SAFE: NO_EFFECTS]", errors)
        self.assertNotIn("[RETICLE_EFFECTS: UNSAFE]", errors)

    def test_a_command_that_runs_code_makes_the_node_unreplayable(self):
        script = [tool_response("execute_terminal_command", {"command": "python validate.py"})] + [tool_response("list_dir", {"path": "."})] * 4
        harness = Harness()
        failure, errors, _, _, _ = harness.run_worker(
            script, "Create or update exactly these workspace files: report.md.\n",
            terminal=lambda *a, **k: "Exit code: 0\nok",
        )
        self.assertIn("[RETICLE_EFFECTS: UNSAFE]", errors)
        self.assertNotIn("RETICLE_RETRY_SAFE", errors)


class TurnAwareness(Harness):
    def test_declared_inputs_are_read_from_the_node_prompt(self):
        prompt = (
            "Your workflow nodes are: qa. Read these workspace files: src/index.html, main.js. "
            "Create or update exactly these workspace files: qa/report.json.\nUpstream nodes are: x.\n"
        )
        self.assertEqual(worker_sdk._declared_input_paths(prompt), {"index.html", "main.js"})
        self.assertEqual(worker_sdk._declared_input_paths("Create or update exactly these workspace files: a.\n"), set())

    def test_reading_inputs_raises_the_budget_with_a_cap(self):
        limit = worker_sdk._iteration_limit
        self.assertEqual(limit({}, set(), 3, 0), 30)
        self.assertEqual(limit({}, set(), 5, 0), 32)
        self.assertEqual(limit({}, set(), 5, 6), 38)
        self.assertEqual(limit({}, set(), 5, 90), 52)  # inputs add at most 20
        self.assertEqual(limit({"llm_max_iterations": 10}, set(), 5, 6), 10)

    def test_the_model_is_told_what_is_missing_while_there_is_still_time(self):
        # 20 turns: a nudge at turn 15 (75 %) and another with 3 left, both naming the file still missing.
        instructions = "Create or update exactly these workspace files: report.md.\n"
        script = [tool_response("list_dir", {"path": f"d{i}"}) for i in range(20)]
        harness = self
        collected = []

        def completion(**kwargs):
            collected.append([m["content"] for m in kwargs["messages"] if m.get("role") == "user"])
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            request = {"id": "e|n", "execution": "e", "memory": {"workspace_dir": temp, "llm_max_iterations": 20}, "parameters": {"llm_model": "ollama/fixture"}}
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", io.StringIO()), patch.object(sys, "stderr", io.StringIO()):
                try:
                    worker_sdk.run(instructions, kind="coding")
                except BaseException:
                    pass
        nudges = [text for text in collected[-1] if text.startswith("Turn budget:")]
        self.assertEqual(len(nudges), 2, nudges)
        self.assertTrue(nudges[0].startswith("Turn budget: 5 of 20"), nudges[0])
        self.assertTrue(nudges[1].startswith("Turn budget: 3 of 20"), nudges[1])
        self.assertTrue(all("report.md" in text for text in nudges))

    def test_tool_calls_describe_what_they_did_without_secrets(self):
        self.assertEqual(worker_sdk._tool_detail("execute_terminal_command", {"command": "node --check main.js"}), "node --check main.js")
        self.assertEqual(worker_sdk._tool_detail("read_file", {"path": "core/Scene.js"}), "core/Scene.js")
        self.assertEqual(worker_sdk._tool_detail("generate_local_assets", {"items": [{}, {}, {}]}), "3 images")
        self.assertEqual(worker_sdk._tool_detail("write_file", {"content": "x"}), "")
        long = worker_sdk._tool_detail("execute_terminal_command", {"command": "echo " + "x" * 400})
        self.assertLessEqual(len(long), 160)
        redacted = worker_sdk._tool_detail("execute_terminal_command", {"command": "curl -H 'Authorization: Bearer abc123' --api_key=sk-secret https://x"})
        self.assertNotIn("sk-secret", redacted)
        self.assertNotIn("abc123", redacted)


if __name__ == "__main__":
    unittest.main()
