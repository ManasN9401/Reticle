import base64
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/"cmd/forge/compiler/lib"))
import vision_tools
import worker_sdk
import project_tools
from test_worker_awareness import Harness, tool_response

try:
    from PIL import Image
except ImportError:
    Image = None


@unittest.skipIf(Image is None, "Pillow is needed for image decoding tests")
class Images(unittest.TestCase):
    def test_downsamples_bounds_bytes_and_preserves_original(self):
        with tempfile.TemporaryDirectory() as workspace:
            source = Path(workspace)/"src"/"image.png"
            source.parent.mkdir()
            image = Image.effect_noise((3000, 2000), 100).convert("RGB")
            image.save(source)
            original = source.read_bytes()
            metadata, part = vision_tools.view_image(workspace, "image.png")
            self.assertLessEqual(max(metadata["width"], metadata["height"]), 1280)
            self.assertLessEqual(metadata["encoded_bytes"], 1024*1024)
            rendered = Image.open(io.BytesIO(base64.b64decode(part["image_url"]["url"].split(",",1)[1])))
            self.assertEqual(rendered.size, (metadata["width"], metadata["height"]))
            self.assertEqual(source.read_bytes(), original)

    def test_path_format_and_input_budgets(self):
        with tempfile.TemporaryDirectory() as workspace:
            source = Path(workspace)/"src"/"image.png"
            source.parent.mkdir()
            source.write_bytes(b"x"*100)
            with patch.object(vision_tools, "MAX_INPUT_BYTES", 99):
                with self.assertRaisesRegex(ValueError, "input budget"):
                    vision_tools.view_image(workspace, "image.png")
            for path in ("../outside.png", "https://example.org/picture.png", "image.svg"):
                with self.assertRaises(ValueError):
                    vision_tools.view_image(workspace, path)
            Image.new("RGB", (100,100)).save(source)
            with patch.object(vision_tools, "MAX_PIXELS", 9999):
                with self.assertRaisesRegex(ValueError, "pixels"):
                    vision_tools.view_image(workspace, "image.png")
            with self.assertRaises(ValueError):
                vision_tools.view_image(workspace, "image.png", max_side=True)


class VisualWorker(Harness):
    def script(self):
        return [tool_response("view_image", {"path":"picture.png"}),
                tool_response("write_file", {"path":"notes.md", "content":"Observed image"}),
                tool_response("read_file", {"path":"notes.md"}),
                tool_response("mark_task_complete", {"summary":"done"})]

    def test_vision_fixture_gets_content_after_all_tool_results(self):
        metadata = {"path":"picture.png", "source_sha256":"fixture"}
        part = {"type":"image_url", "image_url":{"url":"data:image/jpeg;base64,Zml4dHVyZQ=="}}
        with patch.object(vision_tools, "view_image", return_value=(metadata,part)):
            failure, errors, remaining, seen, summary = self.run_worker(self.script(), "Create or update exactly these workspace files: notes.md.\n", parameters={"llm_supports_vision":True})
        self.assertIsNone(failure, errors)
        self.assertEqual(remaining, 0)
        images = [m for m in seen["messages"] if isinstance(m.get("content"),list)]
        self.assertEqual(images[0]["content"][1], part)
        index = seen["messages"].index(images[0])
        self.assertEqual(seen["messages"][index-1]["role"], "tool")
        self.assertEqual(summary,"done")

    def test_server_screenshot_is_attached_as_image_content(self):
        import browser_tools
        script = [tool_response("start_server", {"command":["node","server.js"], "port":3000}),
                  tool_response("run_page", {"server_id":"owned", "path":"/dashboard", "view":True}),
                  tool_response("write_file", {"path":"notes.md", "content":"Observed app"}),
                  tool_response("read_file", {"path":"notes.md"}),
                  tool_response("mark_task_complete", {"summary":"done"})]
        part = {"type":"image_url", "image_url":{"url":"data:image/jpeg;base64,Zml4dHVyZQ=="}}
        with patch.object(project_tools.ManagedServers,"start",return_value={"id":"owned"}), \
             patch.object(project_tools.ManagedServers,"site",return_value=3000), \
             patch.object(browser_tools,"run_page",return_value="Screenshot: run_page/fixture.png") as browser, \
             patch.object(vision_tools,"view_image",return_value=({"path":"run_page/fixture.png"},part)) as view:
            failure, errors, _, seen, _ = self.run_worker(script,"Create or update exactly these workspace files: notes.md.\n",
                parameters={"llm_supports_vision":True}, capabilities=["workspace.read","workspace.write","process.container","network.public","browser.run"])
        self.assertIsNone(failure,errors)
        self.assertEqual(browser.call_args.kwargs["managed_port"],3000)
        self.assertEqual(browser.call_args.kwargs["path"],"/dashboard")
        self.assertNotIn("view",browser.call_args.kwargs)
        self.assertEqual(view.call_args.args[1],"run_page/fixture.png")
        self.assertTrue(any(isinstance(m.get("content"),list) and part in m["content"] for m in seen["messages"]))

    def test_text_only_route_never_offers_or_executes_image_tool(self):
        with patch.object(vision_tools, "view_image") as view:
            failure, errors, _, seen, _ = self.run_worker(self.script(), "Create or update exactly these workspace files: notes.md.\n")
        self.assertIsNone(failure,errors)
        view.assert_not_called()
        self.assertTrue(any("not a real tool" in m.get("content","") for m in seen["messages"] if m["role"]=="tool"))

    def test_compaction_discards_images_before_text(self):
        messages=[{"role":"system","content":"keep"},{"role":"user","content":"task"},
                  {"role":"tool","content":"small receipt"},
                  {"role":"user","content":[{"type":"text","text":"source digest"},{"type":"image_url","image_url":{"url":"data:"+"x"*10000}}]}]
        self.assertGreater(worker_sdk._compact_messages(messages,100),10000)
        self.assertEqual(messages[2]["content"],"small receipt")
        self.assertEqual(messages[-1]["content"][0]["text"],"source digest")
        self.assertTrue(all(p["type"]!="image_url" for p in messages[-1]["content"]))

    def test_server_cleanup_on_success_and_provider_failure(self):
        script=[tool_response("start_server",{"command":["node","server.js"],"port":3000}),
                tool_response("write_file",{"path":"notes.md","content":"server ready"}),
                tool_response("read_file",{"path":"notes.md"}), tool_response("mark_task_complete",{"summary":"done"})]
        with patch.object(project_tools.ManagedServers,"start",return_value={"id":"fixture","url":"http://127.0.0.1:3000"}) as start, \
             patch.object(project_tools.ManagedServers,"close") as close:
            failure, errors, _, _, _=self.run_worker(script,"Create or update exactly these workspace files: notes.md.\n")
        self.assertIsNone(failure,errors)
        start.assert_called_once()
        close.assert_called_once()
        self.assertIn("[RETICLE_EFFECTS: UNSAFE]",errors)
        with patch.object(project_tools.ManagedServers,"close") as close:
            failure, _, _, _, _=self.run_worker([],"inspect")
        self.assertIsNotNone(failure)
        close.assert_called_once()

    def test_process_tools_not_offered_without_process_grants(self):
        script=[tool_response("start_server",{"command":["node","server.js"],"port":3000}),
                tool_response("prepare_environment",{}),
                tool_response("write_file",{"path":"notes.md","content":"no processes"}),
                tool_response("read_file",{"path":"notes.md"}),tool_response("mark_task_complete",{"summary":"done"})]
        with patch.object(project_tools.ManagedServers,"start") as start, patch.object(project_tools.ProjectEnvironments,"prepare") as prepare:
            failure, errors, _, _, _=self.run_worker(script,"Create or update exactly these workspace files: notes.md.\n",capabilities=["workspace.read","workspace.write"])
        self.assertIsNone(failure,errors)
        start.assert_not_called()
        prepare.assert_not_called()


if __name__ == "__main__":
    unittest.main()
