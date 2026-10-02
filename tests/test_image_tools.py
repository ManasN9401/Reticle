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
import image_tools
import worker_sdk

try:
    from PIL import Image
except ImportError:
    Image = None


def make_image(workspace, relative, size=(400, 200), mode="RGB"):
    target = Path(workspace, "src", relative)
    target.parent.mkdir(parents=True, exist_ok=True)
    Image.new(mode, size, (200, 40, 40) if mode == "RGB" else (200, 40, 40, 128)).save(target)
    return target


def size_of(workspace, relative):
    with Image.open(Path(workspace, "src", relative)) as image:
        return image.size


@unittest.skipIf(Image is None, "Pillow is not installed")
class ImageTools(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.ws = self.temp.name
        make_image(self.ws, "art/a.png")

    def test_fit_with_only_a_width_keeps_the_aspect_ratio(self):
        image_tools.resize_image("art/a.png", "out/a.png", 200, self.ws)
        self.assertEqual(size_of(self.ws, "out/a.png"), (200, 100))

    def test_fit_inside_a_box_and_cover_filling_it(self):
        image_tools.resize_image("art/a.png", "out/fit.png", 100, self.ws, height=100)
        self.assertEqual(size_of(self.ws, "out/fit.png"), (100, 50))
        image_tools.resize_image("art/a.png", "out/cover.png", 100, self.ws, height=100, mode="cover")
        self.assertEqual(size_of(self.ws, "out/cover.png"), (100, 100))

    def test_invalid_resize_requests_are_rejected(self):
        for kwargs in ({"width": 0}, {"width": 5000}, {"width": True}, {"width": 50, "mode": "stretch"}, {"width": 50, "mode": "cover"}):
            with self.assertRaises(ValueError, msg=kwargs):
                image_tools.resize_image("art/a.png", "out/x.png", workspace_dir=self.ws, **kwargs)

    def test_convert_to_jpeg_flattens_transparency(self):
        make_image(self.ws, "art/alpha.png", mode="RGBA")
        image_tools.convert_image("art/alpha.png", "out/alpha.jpg", self.ws, quality=70)
        with Image.open(Path(self.ws, "src", "out/alpha.jpg")) as image:
            self.assertEqual((image.format, image.mode), ("JPEG", "RGB"))
        with self.assertRaises(ValueError):
            image_tools.convert_image("art/a.png", "out/x.jpg", self.ws, quality=0)

    def test_crop_validates_the_box(self):
        image_tools.crop_image("art/a.png", "out/c.png", 10, 20, 110, 70, self.ws)
        self.assertEqual(size_of(self.ws, "out/c.png"), (100, 50))
        for box in ((0, 0, 401, 10), (50, 0, 50, 10), (0, 0, 10, 201), (-1, 0, 10, 10), (True, 0, 10, 10)):
            with self.assertRaises(ValueError, msg=box):
                image_tools.crop_image("art/a.png", "out/c.png", *box, self.ws)

    def test_thumbnails_cover_every_image_and_report_bad_files(self):
        make_image(self.ws, "art/b.jpg", size=(100, 300))
        Path(self.ws, "src", "art", "broken.png").write_bytes(b"not an image")
        Path(self.ws, "src", "art", "notes.txt").write_text("ignored", encoding="utf-8")
        report = json.loads(image_tools.make_thumbnails("art", "thumbs", self.ws, max_size=64))
        self.assertEqual(report["created"], ["thumbs/a.png", "thumbs/b.jpg"])
        self.assertEqual([item["source"] for item in report["failed"]], ["art/broken.png"])
        self.assertEqual(size_of(self.ws, "thumbs/a.png"), (64, 32))
        self.assertEqual(size_of(self.ws, "thumbs/b.jpg"), (21, 64))

    def test_thumbnail_requests_are_validated(self):
        with self.assertRaises(ValueError):
            image_tools.make_thumbnails("art", "art", self.ws)
        with self.assertRaises(ValueError):
            image_tools.make_thumbnails("missing", "thumbs", self.ws)
        Path(self.ws, "src", "empty").mkdir()
        with self.assertRaises(ValueError):
            image_tools.make_thumbnails("empty", "thumbs", self.ws)
        with patch.object(image_tools, "MAX_THUMBNAILS", 0), self.assertRaises(ValueError):
            image_tools.make_thumbnails("art", "thumbs", self.ws)

    def test_total_thumbnail_failure_is_an_error_result(self):
        Path(self.ws, "src", "bad").mkdir()
        Path(self.ws, "src", "bad", "x.png").write_bytes(b"nope")
        self.assertTrue(image_tools.make_thumbnails("bad", "thumbs", self.ws).startswith("Error"))

    def test_paths_cannot_escape_or_use_other_formats(self):
        for source, output in (("../a.png", "out/a.png"), ("art/a.png", "../out.png"), ("art/a.png", "out/a.gif"), ("art/missing.png", "out/a.png")):
            with self.assertRaises(ValueError, msg=(source, output)):
                image_tools.convert_image(source, output, self.ws)

    def test_oversized_sources_are_refused(self):
        with patch.object(image_tools, "MAX_INPUT_PIXELS", 1000), self.assertRaises(ValueError):
            image_tools.convert_image("art/a.png", "out/a.png", self.ws)
        with patch.object(image_tools, "MAX_INPUT_BYTES", 10), self.assertRaises(ValueError):
            image_tools.convert_image("art/a.png", "out/a.png", self.ws)


class MissingPillow(unittest.TestCase):
    def test_a_clear_error_is_reported(self):
        with patch.dict(sys.modules, {"PIL": None}):
            with self.assertRaisesRegex(ValueError, "Pillow is not installed"):
                image_tools.convert_image("a.png", "b.png", "workspace")


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


@unittest.skipIf(Image is None, "Pillow is not installed")
class WorkerIntegration(unittest.TestCase):
    def test_thumbnails_and_resizes_satisfy_declared_output_directories(self):
        script = [
            tool_response("make_thumbnails", {"source_dir": "public/images/art", "output_dir": "public/images/thumbs", "max_size": 32}),
            tool_response("mark_task_complete", {"summary": "too early: hero is missing"}),
            tool_response("resize_image", {"source_path": "public/images/art/a.png", "output_path": "public/images/hero/a.png", "width": 100}),
            tool_response("mark_task_complete", {"summary": "edited"}),
        ]
        instructions = "Create or update exactly these workspace files: public/images/thumbs/, public/images/hero/.\n"

        def completion(**kwargs):
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            make_image(temp, "public/images/art/a.png")
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            output = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", output):
                worker_sdk.run(instructions, kind="writing")
            self.assertEqual(json.loads(output.getvalue())["artifact"]["data"], "edited")
            self.assertTrue(Path(temp, "src", "public/images/thumbs/a.png").is_file())


if __name__ == "__main__":
    unittest.main()
