"""The file tools a model works with: paged reads that say what they cut, edits that say what is
wrong, several edits at once, and find/move/delete.

Observed before: read_file returned up to 1 MiB and the worker silently cut every reply at 20,000
characters, so a 51 KB page was seen as its first 20 KB; "Expected exactly one match" gave a model
nothing to change, so it sent the same request until the run was stopped."""
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import forge_utils as tools
import worker_sdk


class Workspace(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.src = Path(self.temp.name) / "src"
        self.src.mkdir()
        previous = tools.TOOL_RESULT_CHARS
        self.addCleanup(setattr, tools, "TOOL_RESULT_CHARS", previous)

    def put(self, name, text):
        target = self.src / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding="utf-8", newline="")
        return target

    def text(self, name):
        return (self.src / name).read_text(encoding="utf-8")


class PagedReads(Workspace):
    def numbered(self, count, width=40):
        return "".join(f"line {n:05d} " + "x" * (width - 11) + "\n" for n in range(1, count + 1))

    def test_a_file_that_fits_comes_back_exactly_as_before(self):
        self.put("small.js", "const a = 1;\n")
        self.assertEqual(tools.read_file("small.js", self.temp.name), "const a = 1;\n")

    def test_a_long_file_is_paged_and_says_where_the_next_part_starts(self):
        tools.TOOL_RESULT_CHARS = 4000
        body = self.numbered(500)
        self.put("big.js", body)
        first = tools.read_file("big.js", self.temp.name)
        self.assertTrue(first.startswith(tools.READ_NOTICE))
        header, _, shown = first.partition("\n")
        self.assertIn("of 500", header)
        self.assertIn("offset=", header)
        self.assertLess(len(shown), 4000)
        self.assertTrue(body.startswith(shown))

    def test_following_the_offsets_reconstructs_the_whole_file(self):
        tools.TOOL_RESULT_CHARS = 3000
        body = self.numbered(400)
        self.put("big.js", body)
        collected, offset, calls = "", 1, 0
        while True:
            reply = tools.read_file("big.js", self.temp.name, offset=offset)
            calls += 1
            if not reply.startswith(tools.READ_NOTICE):
                collected += reply
                break
            header, _, part = reply.partition("\n")
            collected += part
            if "offset=" not in header:
                break
            offset = int(header.split("offset=")[1].split(" ")[0])
            self.assertLess(calls, 100)
        self.assertEqual(collected, body)
        self.assertGreater(calls, 3)

    def test_offset_and_limit_select_lines(self):
        self.put("a.txt", "".join(f"row {n}\n" for n in range(1, 21)))
        reply = tools.read_file("a.txt", self.temp.name, offset=5, limit=3)
        self.assertTrue(reply.startswith(tools.READ_NOTICE))
        self.assertIn("lines 5-7 of 20", reply)
        self.assertTrue(reply.endswith("row 5\nrow 6\nrow 7\n"))
        self.assertIn("offset=8", reply)

    def test_asking_past_the_end_or_for_nonsense_is_an_error_that_says_so(self):
        self.put("a.txt", "one\ntwo\n")
        self.assertIn("past the end", tools.read_file("a.txt", self.temp.name, offset=9))
        self.assertTrue(tools.read_file("a.txt", self.temp.name, offset=0).startswith("Error"))
        self.assertTrue(tools.read_file("a.txt", self.temp.name, limit="all").startswith("Error"))

    def test_one_line_longer_than_a_reply_is_cut_and_said_to_be(self):
        tools.TOOL_RESULT_CHARS = 3000
        self.put("min.js", "a" * 9000 + "\nnext\n")
        reply = tools.read_file("min.js", self.temp.name)
        self.assertIn("line 1 alone is longer than one reply", reply)
        self.assertIn("offset=2", reply)
        self.assertLess(len(reply), 3500)

    def test_the_reply_budget_scales_with_the_window_that_is_known(self):
        budget = worker_sdk._tool_result_budget
        self.assertEqual(budget({}, {}), 20000)
        self.assertEqual(budget({"llm_context_tokens": 8192}, {}), 6000)
        self.assertEqual(budget({"llm_context_tokens": 32768}, {}), 22937)
        self.assertEqual(budget({"llm_context_tokens": 1000000}, {}), 60000)
        self.assertEqual(budget({}, {"llm_num_ctx": "16384"}), 11468)
        self.assertEqual(budget({"llm_context_tokens": "unknown"}, {}), 20000)

    def test_a_cut_tool_reply_says_how_much_and_what_to_do(self):
        capped = worker_sdk._cap_tool_result("y" * 9000, 5000)
        self.assertTrue(capped.startswith("y" * 5000))
        self.assertIn("cut at 5000 of 9000 characters", capped)
        self.assertEqual(worker_sdk._cap_tool_result("short", 5000), "short")
        self.assertEqual(worker_sdk._cap_tool_result("", 5000), "(no output)")


class InformativeEdits(Workspace):
    SOURCE = "function go() {\n    let speed = 5;\n    return speed;\n}\nlet speed = 5;\n"

    def replace(self, target, replacement, **options):
        return tools.replace_file_content("app.js", target, replacement, self.temp.name, **options)

    def setUp(self):
        super().setUp()
        self.put("app.js", self.SOURCE)

    def test_a_target_that_differs_only_in_whitespace_is_told_so_with_the_line(self):
        reply = self.replace("let  speed = 5;\nreturn speed;", "let speed = 6;")
        self.assertTrue(reply.startswith("Error"))
        self.assertIn("whitespace", reply)
        self.assertIn("near line", reply)
        self.assertEqual(self.text("app.js"), self.SOURCE)

    def test_a_target_that_is_not_there_gets_the_closest_lines(self):
        reply = self.replace("function start() {", "function begin() {")
        self.assertIn("closest lines", reply)
        self.assertIn("function go()", reply)

    def test_several_matches_are_listed_and_a_way_out_is_given(self):
        reply = self.replace("let speed = 5;", "let speed = 9;")
        self.assertTrue(reply.startswith("Error"))
        self.assertIn("matches 2 places", reply)
        self.assertIn("lines 2, 5", reply)
        self.assertIn("occurrence", reply)

    def test_occurrence_chooses_which_match(self):
        self.assertTrue(self.replace("let speed = 5;", "let speed = 9;", occurrence=2).startswith("Successfully"))
        self.assertEqual(self.text("app.js"), self.SOURCE[:self.SOURCE.rindex("let speed = 5;")] + "let speed = 9;\n")
        self.assertIn("matches only 1 time", self.replace("let speed = 9;", "x", occurrence=2))

    def test_an_edit_that_inserts_the_start_of_the_file_again_is_refused_in_any_language(self):
        big = self.SOURCE + ("// pad\n" * 80)
        head = big.lstrip()[:160]
        self.put("app.js", big)
        reply = self.replace("let speed = 5;\n// pad", head + "\nlet speed = 5;\n// pad")
        self.assertTrue(reply.startswith("Error"), reply)
        self.assertIn("second copy", reply)
        self.assertEqual(self.text("app.js"), big)

    def test_a_normal_edit_still_works(self):
        self.assertEqual(self.replace("return speed;", "return speed * 2;"), "Successfully replaced content in app.js")
        self.assertIn("return speed * 2;", self.text("app.js"))


class SeveralEdits(Workspace):
    def setUp(self):
        super().setUp()
        self.put("page.html", "<h1>Title</h1>\n<p>one</p>\n<p>two</p>\n<footer>old</footer>\n")

    def test_edits_apply_in_order_in_one_swap(self):
        reply = tools.apply_edits("page.html", [
            {"target_content": "<h1>Title</h1>", "replacement_content": "<h1>New</h1>"},
            {"target_content": "<p>one</p>", "replacement_content": "<p>1</p>"},
            {"target_content": "<h1>New</h1>", "replacement_content": "<h1>Newer</h1>"},
        ], self.temp.name)
        self.assertEqual(reply, "Successfully applied 3 edit(s) to page.html")
        self.assertEqual(self.text("page.html"), "<h1>Newer</h1>\n<p>1</p>\n<p>two</p>\n<footer>old</footer>\n")

    def test_one_failure_changes_nothing_and_names_the_edit(self):
        before = self.text("page.html")
        reply = tools.apply_edits("page.html", [
            {"target_content": "<p>one</p>", "replacement_content": "<p>1</p>"},
            {"target_content": "<p>three</p>", "replacement_content": "<p>3</p>"},
        ], self.temp.name)
        self.assertTrue(reply.startswith("Error"))
        self.assertIn("edit 2 of 2 failed and nothing was changed", reply)
        self.assertEqual(self.text("page.html"), before)
        self.assertEqual([p.name for p in self.src.iterdir()], ["page.html"], "no lock or temporary file may remain")

    def test_bad_arguments_are_explained(self):
        self.assertIn("non-empty list", tools.apply_edits("page.html", [], self.temp.name))
        self.assertIn("at most", tools.apply_edits("page.html", [{"target_content": "a", "replacement_content": "b"}] * 21, self.temp.name))
        self.assertIn("must be an object", tools.apply_edits("page.html", ["nope"], self.temp.name))

    def test_the_whole_page_rule_applies_to_every_edit(self):
        self.put("full.html", "<!DOCTYPE html>\n<html><body><footer>old</footer></body></html>\n")
        before = self.text("full.html")
        reply = tools.apply_edits("full.html", [
            {"target_content": "<footer>old</footer>", "replacement_content": "<!DOCTYPE html><html></html>"},
        ], self.temp.name)
        self.assertTrue(reply.startswith("Error"))
        self.assertEqual(self.text("full.html"), before)


class FindMoveDelete(Workspace):
    def setUp(self):
        super().setUp()
        self.put("index.html", "<p>x</p>")
        self.put("js/main.js", "1")
        self.put("js/lib/util.js", "22")
        self.put("css/site.css", "a{}")

    def test_find_files_matches_globs_and_bare_names_in_every_folder(self):
        listing = tools.find_files("*.js", self.temp.name).splitlines()
        self.assertEqual(listing, ["js/lib/util.js (2 bytes)", "js/main.js (1 bytes)"])
        self.assertEqual(tools.find_files("js/*.js", self.temp.name), "js/main.js (1 bytes)")
        self.assertEqual(tools.find_files("**/*.css", self.temp.name), "css/site.css (3 bytes)")
        self.assertEqual(tools.find_files("*.php", self.temp.name), "No files match.")

    def test_find_files_refuses_to_leave_the_workspace(self):
        for pattern in ("../*", "/etc/*", "C:/x/*", ""):
            self.assertTrue(tools.find_files(pattern, self.temp.name).startswith("Error"), pattern)

    def test_move_renames_a_file_and_a_folder(self):
        self.assertTrue(tools.move_path("index.html", "home.html", self.temp.name).startswith("Successfully"))
        self.assertFalse((self.src / "index.html").exists())
        self.assertTrue(tools.move_path("js", "scripts/js", self.temp.name).startswith("Successfully"))
        self.assertTrue((self.src / "scripts" / "js" / "lib" / "util.js").is_file())

    def test_move_will_not_overwrite_or_move_into_itself_or_out_of_the_workspace(self):
        self.assertIn("already exists", tools.move_path("index.html", "css/site.css", self.temp.name))
        self.assertIn("into itself", tools.move_path("js", "js/lib/js", self.temp.name))
        self.assertIn("does not exist", tools.move_path("missing.txt", "x.txt", self.temp.name))
        self.assertTrue(tools.move_path("index.html", "../escape.html", self.temp.name).startswith("Error"))
        self.assertTrue((self.src / "index.html").is_file())

    def test_delete_removes_a_file_but_a_full_folder_needs_recursive(self):
        self.assertTrue(tools.delete_path("css/site.css", self.temp.name).startswith("Successfully"))
        self.assertTrue(tools.delete_path("css", self.temp.name).startswith("Successfully"), "an empty folder goes")
        refused = tools.delete_path("js", self.temp.name)
        self.assertIn("recursive=true", refused)
        self.assertTrue((self.src / "js" / "main.js").is_file())
        self.assertTrue(tools.delete_path("js", self.temp.name, recursive=True).startswith("Successfully"))
        self.assertFalse((self.src / "js").exists())

    def test_the_workspace_itself_and_outside_paths_cannot_be_deleted(self):
        self.assertIn("workspace itself", tools.delete_path(".", self.temp.name, recursive=True))
        self.assertTrue(tools.delete_path("../outside", self.temp.name, recursive=True).startswith("Error"))
        self.assertTrue((self.src / "index.html").is_file())


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class ThroughTheWorker(Workspace):
    def run_worker(self, script, request_extra=None, capabilities=None, files=None, env=None):
        script = list(script)
        calls = []

        class Done(Exception):
            pass

        def completion(**kwargs):
            calls.append(kwargs)
            if not script:
                raise Done()
            return script.pop(0)

        for name, text in (files or {}).items():
            self.put(name, text)
        request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": self.temp.name}, "parameters": {"llm_model": "ollama/fixture"}}
        if capabilities is not None:
            request["capabilities"] = capabilities
        request.update(request_extra or {})
        output = io.StringIO()
        environment = {"RETICLE_LOCAL_GPU_COORDINATION": "false"}
        environment.update(env or {})
        with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                patch.dict("os.environ", environment, clear=True), \
                patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                patch.object(sys, "stdout", output):
            try:
                worker_sdk.run("Fixture", kind="writing")
            except Done:
                return None, calls
        return json.loads(output.getvalue()), calls

    def offered(self, calls):
        return {tool["function"]["name"]: tool["function"] for tool in calls[0]["tools"]}

    def test_every_file_tool_is_offered_by_default_and_the_terminal_says_what_it_is(self):
        _, calls = self.run_worker([])
        offered = self.offered(calls)
        for name in ("read_file", "write_file", "replace_file_content", "apply_edits", "find_files",
                     "move_path", "delete_path", "list_dir", "search_codebase", "execute_terminal_command"):
            self.assertIn(name, offered)
        description = offered["execute_terminal_command"]["description"]
        self.assertIn("no node, npm or git", description)
        self.assertIn("Time limit 120 seconds", description)
        self.assertNotIn("optional", json.dumps(offered["read_file"]["parameters"]["required"]))
        self.assertEqual(offered["read_file"]["parameters"]["required"], ["path"])
        self.assertEqual(offered["apply_edits"]["parameters"]["required"], ["path", "edits"])

    def test_a_read_only_agent_is_not_offered_anything_that_writes_or_runs(self):
        _, calls = self.run_worker([], capabilities=["workspace.read"])
        offered = self.offered(calls)
        self.assertIn("find_files", offered)
        for name in ("write_file", "replace_file_content", "apply_edits", "move_path", "delete_path", "execute_terminal_command"):
            self.assertNotIn(name, offered)

    def test_native_execution_changes_what_the_terminal_description_promises(self):
        request = {"memory": {"workspace_dir": self.temp.name, "allow_native_execution": "true"}}
        _, calls = self.run_worker([], request_extra=request)
        self.assertIn("directly on the user's machine", self.offered(calls)["execute_terminal_command"]["description"])

    def test_the_window_sets_how_much_a_reply_may_carry(self):
        long_file = "".join(f"row {n:05d} {'z' * 30}\n" for n in range(1, 1500))
        request = {"parameters": {"llm_model": "ollama/fixture", "llm_context_tokens": 8192}}
        _, calls = self.run_worker([
            tool_response("read_file", {"path": "big.txt"}),
            tool_response("mark_task_complete", {"summary": "read"}),
        ], request_extra=request, files={"big.txt": long_file})
        reply = [m for m in calls[1]["messages"] if m.get("role") == "tool"][0]["content"]
        self.assertLessEqual(len(reply), 6000)
        self.assertTrue(reply.startswith(tools.READ_NOTICE))
        self.assertIn("offset=", reply.splitlines()[0])

    def test_a_partial_read_is_not_enough_to_overwrite_a_file(self):
        original = "".join(f"row {n}\n" for n in range(1, 30))
        response, calls = self.run_worker([
            tool_response("read_file", {"path": "data.txt", "offset": 1, "limit": 5}),
            tool_response("write_file", {"path": "data.txt", "content": "new\n", "overwrite": True}),
            tool_response("read_file", {"path": "data.txt"}),
            tool_response("write_file", {"path": "data.txt", "content": "new\n", "overwrite": True}),
            tool_response("read_file", {"path": "data.txt"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ], files={"data.txt": original})
        refused = [m for m in calls[2]["messages"] if m.get("role") == "tool"][1]["content"]
        self.assertTrue(refused.startswith("Error"), refused)
        self.assertEqual(self.text("data.txt"), "new\n")

    def test_move_and_delete_keep_the_reported_files_honest(self):
        response, _ = self.run_worker([
            tool_response("write_file", {"path": "draft.txt", "content": "text"}),
            tool_response("write_file", {"path": "scratch.txt", "content": "tmp"}),
            tool_response("move_path", {"source_path": "draft.txt", "destination_path": "final.txt"}),
            tool_response("delete_path", {"path": "scratch.txt"}),
            tool_response("read_file", {"path": "final.txt"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ])
        self.assertEqual(response["files"], ["final.txt"])
        self.assertEqual(self.text("final.txt"), "text")

    def test_apply_edits_through_the_worker(self):
        response, _ = self.run_worker([
            tool_response("read_file", {"path": "cfg.txt"}),
            tool_response("apply_edits", {"path": "cfg.txt", "edits": [
                {"target_content": "a=1", "replacement_content": "a=2"},
                {"target_content": "b=1", "replacement_content": "b=2"},
            ]}),
            tool_response("read_file", {"path": "cfg.txt"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ], files={"cfg.txt": "a=1\nb=1\n"})
        self.assertEqual(self.text("cfg.txt"), "a=2\nb=2\n")
        self.assertEqual(response["files"], ["cfg.txt"])


class TerminalImage(unittest.TestCase):
    def test_a_missing_docker_is_reported_instead_of_a_stack_trace_in_the_output(self):
        with patch.object(tools.subprocess, "run", side_effect=FileNotFoundError()):
            problem = tools._ensure_worker_image("example/image")
        self.assertIn("Docker is not installed", problem)

    def test_a_ready_image_is_not_pulled_again(self):
        calls = []

        def run(argv, **kwargs):
            calls.append(argv[:3])
            return SimpleNamespace(returncode=0, stderr=b"")
        with patch.object(tools.subprocess, "run", side_effect=run):
            self.assertEqual(tools._ensure_worker_image("example/image"), "")
        self.assertEqual(calls, [["docker", "image", "inspect"]])

    def test_a_failed_pull_says_why_and_the_command_is_not_attempted(self):
        def run(argv, **kwargs):
            return SimpleNamespace(returncode=1 if argv[1] == "image" else 1, stderr=b"pull access denied")
        with tempfile.TemporaryDirectory() as workspace, patch.object(tools.subprocess, "run", side_effect=run):
            reply = tools.execute_terminal_command("echo hi", workspace, False)
        self.assertTrue(reply.startswith("Error executing command: could not pull"), reply)
        self.assertIn("pull access denied", reply)


if __name__ == "__main__":
    unittest.main()
