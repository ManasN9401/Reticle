"""Overwriting a file you have read, and refusing an edit that nests a whole page in a page.

Observed: a resumed node could not replace the page an earlier attempt wrote (write_file refuses
an existing file), so the model used replace_file_content to insert a whole new page at one unique
string. Each such edit matched once and succeeded; one run ended with nine documents in one file."""
import io
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import forge_utils as tools
import worker_sdk

PAGE = "<!DOCTYPE html>\n<html>\n<head><title>One</title></head>\n<body><p>first</p></body>\n</html>\n"
REWRITTEN = "<!DOCTYPE html>\n<html>\n<head><title>Two</title></head>\n<body><p>second</p></body>\n</html>\n"


def digest_of(text):
    return tools.text_digest(text)


class WriteFileOverwrite(unittest.TestCase):
    def test_a_concurrent_change_during_temp_write_is_preserved(self):
        original = tools.tempfile.NamedTemporaryFile
        def temporary(*args, **kwargs):
            stream = original(*args, **kwargs)
            self.page.write_text("concurrent edit", encoding="utf-8")
            return stream
        with patch.object(tools.tempfile, "NamedTemporaryFile", side_effect=temporary):
            result = self.write(REWRITTEN, overwrite=True, expected_digest=digest_of(PAGE))
        self.assertIn("changed during overwrite", result)
        self.assertEqual(self.page.read_text(encoding="utf-8"), "concurrent edit")
        self.assertEqual(self.modified, {})

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        (Path(self.temp.name) / "src").mkdir()
        self.page = Path(self.temp.name) / "src" / "index.html"
        self.page.write_text(PAGE, encoding="utf-8")
        self.modified = {}

    def write(self, content, **options):
        return tools.write_file("index.html", content, self.temp.name, self.modified, **options)

    def leftovers(self):
        return sorted(p.name for p in self.page.parent.iterdir() if p.name not in {"index.html", "index.html.reticle-lock"})

    def test_an_existing_file_is_still_refused_and_the_refusal_names_the_way_out(self):
        result = self.write(REWRITTEN)
        self.assertTrue(result.startswith("Error: File exists"))
        self.assertIn("overwrite=true", result)
        self.assertEqual(self.page.read_text(encoding="utf-8"), PAGE)

    def test_overwrite_without_having_read_the_file_is_refused(self):
        result = self.write(REWRITTEN, overwrite=True)
        self.assertTrue(result.startswith("Error"), result)
        self.assertIn("read_file", result)
        self.assertEqual(self.page.read_text(encoding="utf-8"), PAGE)

    def test_overwrite_replaces_the_version_that_was_read_and_leaves_nothing_behind(self):
        result = self.write(REWRITTEN, overwrite=True, expected_digest=digest_of(PAGE))
        self.assertEqual(result, "Successfully overwrote index.html")
        self.assertEqual(self.page.read_text(encoding="utf-8"), REWRITTEN)
        self.assertEqual(self.modified["index.html"], REWRITTEN)
        self.assertEqual(self.leftovers(), [], "no temporary file may remain; the OS lock inode stays stable")

    def test_overwrite_is_refused_when_the_file_changed_after_it_was_read(self):
        stale = digest_of(PAGE)
        self.page.write_text(PAGE.replace("first", "someone else"), encoding="utf-8")
        result = self.write(REWRITTEN, overwrite=True, expected_digest=stale)
        self.assertIn("changed since you read it", result)
        self.assertIn("someone else", self.page.read_text(encoding="utf-8"))
        self.assertEqual(self.leftovers(), [])

    def test_overwrite_of_a_file_that_does_not_exist_just_creates_it(self):
        result = tools.write_file("new.html", "<p>x</p>", self.temp.name, {}, overwrite=True)
        self.assertEqual(result, "Successfully wrote to new.html")

    def test_the_flag_is_optional_in_the_tool_definition(self):
        description, properties = tools._definitions["write_file"]
        self.assertIn("overwrite", description)
        self.assertTrue(properties["overwrite"]["optional"])
        descriptor = worker_sdk._local_tool_descriptor("write_file", description, properties, "workspace.write")
        self.assertEqual(descriptor["schema"]["required"], ["path", "content"])


class ReplaceFileContentGuard(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        (Path(self.temp.name) / "src").mkdir()

    def file(self, name, text):
        path = Path(self.temp.name) / "src" / name
        path.write_text(text, encoding="utf-8")
        return path

    def test_a_whole_page_inserted_into_a_page_is_refused_and_the_file_is_untouched(self):
        page = self.file("index.html", PAGE)
        result = tools.replace_file_content("index.html", "<title>One</title>", REWRITTEN, self.temp.name)
        self.assertTrue(result.startswith("Error"), result)
        self.assertIn("overwrite=true", result)
        self.assertEqual(page.read_text(encoding="utf-8"), PAGE)

    def test_the_nesting_run_cannot_repeat(self):
        # The observed failure: each insertion matched exactly once. The first one must be stopped.
        page = self.file("index.html", PAGE)
        for _ in range(3):
            tools.replace_file_content("index.html", "</html>", "</html>\n" + REWRITTEN, self.temp.name)
        self.assertEqual(page.read_text(encoding="utf-8").lower().count("<!doctype"), 1)

    def test_an_ordinary_edit_inside_the_page_is_allowed(self):
        page = self.file("index.html", PAGE)
        result = tools.replace_file_content("index.html", "<p>first</p>", "<p>first</p><canvas id=c></canvas>", self.temp.name)
        self.assertEqual(result, "Successfully replaced content in index.html")
        self.assertIn("<canvas", page.read_text(encoding="utf-8"))

    def test_a_script_change_that_mentions_html_in_a_string_is_allowed_outside_html_files(self):
        script = self.file("page.js", "const shell = 'x';\n")
        result = tools.replace_file_content("page.js", "'x'", "'<html></html>'", self.temp.name)
        self.assertTrue(result.startswith("Successfully"), result)
        self.assertIn("<html>", script.read_text(encoding="utf-8"))

    def test_a_fragment_file_without_a_root_can_gain_one(self):
        fragment = self.file("part.html", "<p>x</p>\n")
        result = tools.replace_file_content("part.html", "<p>x</p>", "<html><p>x</p></html>", self.temp.name)
        self.assertTrue(result.startswith("Successfully"), result)
        self.assertIn("<html>", fragment.read_text(encoding="utf-8"))


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class ThroughTheWorker(unittest.TestCase):
    def run_worker(self, script, existing=None, resumed=False):
        script = list(script)
        calls = []

        def completion(**kwargs):
            calls.append(kwargs)
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            if existing is not None:
                (Path(temp) / "src" / "index.html").write_text(existing, encoding="utf-8")
            memory = {"workspace_dir": temp}
            if resumed:
                memory["resumed_after_files"] = True
            request = {"id": "execution|node", "execution": "execution", "memory": memory, "parameters": {"llm_model": "ollama/fixture"}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                worker_sdk.run("Fixture", kind="writing")
            text = (Path(temp) / "src" / "index.html").read_text(encoding="utf-8")
            return json.loads(output.getvalue()), text, calls

    def test_a_resumed_node_can_replace_the_page_an_earlier_attempt_left(self):
        response, text, calls = self.run_worker([
            tool_response("read_file", {"path": "index.html"}),
            tool_response("write_file", {"path": "index.html", "content": REWRITTEN, "overwrite": True}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "rewritten"}),
        ], existing=PAGE, resumed=True)
        self.assertEqual(text, REWRITTEN)
        self.assertEqual(response["files"], ["index.html"])
        prompt = calls[0]["messages"][0]["content"]
        self.assertIn("overwrite=true", prompt)

    def test_overwrite_before_reading_is_refused_and_the_model_recovers(self):
        response, text, calls = self.run_worker([
            tool_response("write_file", {"path": "index.html", "content": REWRITTEN, "overwrite": True}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("write_file", {"path": "index.html", "content": REWRITTEN, "overwrite": True}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ], existing=PAGE)
        self.assertEqual(text, REWRITTEN)
        first_refusal = [m for m in calls[1]["messages"] if m.get("role") == "tool"][0]["content"]
        self.assertTrue(first_refusal.startswith("Error"), first_refusal)

    def test_a_second_rewrite_does_not_need_another_read(self):
        # The worker knows what it just wrote; asking for a read after every write would loop weak models.
        third = REWRITTEN.replace("second", "third")
        response, text, calls = self.run_worker([
            tool_response("read_file", {"path": "index.html"}),
            tool_response("write_file", {"path": "index.html", "content": REWRITTEN, "overwrite": True}),
            tool_response("write_file", {"path": "index.html", "content": third, "overwrite": True}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ], existing=PAGE)
        self.assertEqual(text, third)

    def test_the_nesting_edit_is_refused_through_the_worker(self):
        response, text, calls = self.run_worker([
            tool_response("read_file", {"path": "index.html"}),
            tool_response("replace_file_content", {"path": "index.html", "target_content": "</html>", "replacement_content": "</html>\n" + REWRITTEN}),
            tool_response("read_file", {"path": "index.html"}),
            tool_response("check_web_page", {}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ], existing=PAGE)
        self.assertEqual(text, PAGE)
        refusal = [m for m in calls[2]["messages"] if m.get("role") == "tool"][1]["content"]
        self.assertTrue(refusal.startswith("Error"), refusal)


if __name__ == "__main__":
    unittest.main()
