import io
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import comfy_tools

try:
    from PIL import Image
except ImportError:  # the Pillow-dependent checks are skipped without it
    Image = None


def png_bytes(mode="RGBA", size=(8, 8)):
    out = io.BytesIO()
    Image.new(mode, size, (200, 40, 40, 128) if mode == "RGBA" else (200, 40, 40)).save(out, format="PNG")
    return out.getvalue()


class RequestedFormat(unittest.TestCase):
    def test_raster_extensions_pick_their_format(self):
        for name, expected in (("a.png", "PNG"), ("dir/b.JPG", "JPEG"), ("c.jpeg", "JPEG"), ("d.WebP", "WEBP")):
            self.assertEqual(comfy_tools._output_format(name), expected, name)

    def test_a_model_or_unknown_name_is_refused_with_a_way_forward(self):
        # Observed: assets/models/frame-ornate.glb was written, and held PNG data.
        for name in ("models/frame-ornate.glb", "scene.gltf", "noextension", "x.tga"):
            with self.assertRaises(ValueError) as caught:
                comfy_tools._output_format(name)
            self.assertIn("raster images", str(caught.exception))
        with self.assertRaisesRegex(ValueError, "3D models such as .glb.*write_file"):
            comfy_tools._output_format("models/frame.glb")

    def test_the_refusal_happens_before_any_request_is_made(self):
        with tempfile.TemporaryDirectory() as workspace, \
                patch.object(comfy_tools.urllib.request, "urlopen", side_effect=AssertionError("no request may be made")):
            with self.assertRaisesRegex(ValueError, "3D models"):
                comfy_tools._generate_local_asset("a frame", "models/frame.glb", workspace)

    def test_one_bad_name_in_a_batch_is_reported_and_the_rest_are_unaffected(self):
        calls = []

        def generate(prompt, output_path, workspace_dir, **options):
            calls.append(output_path)
            comfy_tools._output_format(output_path)

        with patch.object(comfy_tools, "_generate_local_asset", generate), \
                patch.object(comfy_tools, "local_gpu_session"), patch.object(comfy_tools, "release_comfy_models"):
            report = comfy_tools.generate_local_assets(
                [{"prompt": "a", "output_path": "a.png"}, {"prompt": "m", "output_path": "m.glb"}, {"prompt": "b", "output_path": "b.jpg"}],
                "workspace",
            )
        self.assertEqual(calls, ["a.png", "m.glb", "b.jpg"])
        self.assertIn('"generated": ["a.png", "b.jpg"]', report)
        self.assertIn("m.glb", report)


@unittest.skipIf(Image is None, "Pillow is not installed")
class EncodedFormat(unittest.TestCase):
    def test_png_is_passed_through_untouched(self):
        data = png_bytes()
        self.assertIs(comfy_tools._encode_image(data, "PNG"), data)

    def test_a_jpg_name_gets_jpeg_data_even_for_a_transparent_image(self):
        jpeg = comfy_tools._encode_image(png_bytes("RGBA"), "JPEG")
        self.assertTrue(jpeg.startswith(b"\xff\xd8\xff"))
        self.assertEqual(Image.open(io.BytesIO(jpeg)).format, "JPEG")

    def test_a_webp_name_gets_webp_data(self):
        webp = comfy_tools._encode_image(png_bytes("RGB"), "WEBP")
        self.assertEqual(webp[:4], b"RIFF")
        self.assertEqual(webp[8:12], b"WEBP")


class MissingPillow(unittest.TestCase):
    def test_a_non_png_name_without_pillow_is_an_error_not_a_mislabelled_file(self):
        real_import = __import__

        def no_pillow(name, *args, **kwargs):
            if name == "PIL" or name.startswith("PIL."):
                raise ImportError("no Pillow here")
            return real_import(name, *args, **kwargs)

        with patch("builtins.__import__", no_pillow):
            with self.assertRaisesRegex(ValueError, "only .png output is possible"):
                comfy_tools._encode_image(b"\x89PNG....", "JPEG")


if __name__ == "__main__":
    unittest.main()
