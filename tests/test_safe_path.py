import ctypes
import os
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import forge_utils


@unittest.skipUnless(os.name == "nt", "8.3 short names exist only on Windows")
class ShortNameWorkspace(unittest.TestCase):
    def test_short_name_workspace_is_accepted(self):
        with tempfile.TemporaryDirectory() as temp:
            buffer = ctypes.create_unicode_buffer(32768)
            if not ctypes.windll.kernel32.GetShortPathNameW(temp, buffer, len(buffer)):
                self.skipTest("8.3 names are unavailable on this volume")
            self.assertTrue(forge_utils.write_file("a.txt", "x", buffer.value, {}).startswith("Successfully"))

    def test_symlinked_workspace_is_still_denied(self):
        with tempfile.TemporaryDirectory() as temp:
            real, link = Path(temp, "real"), Path(temp, "link")
            real.mkdir()
            try:
                link.symlink_to(real, target_is_directory=True)
            except OSError:
                self.skipTest("symlinks need privileges")
            with self.assertRaises(ValueError):
                forge_utils.safe_path(link, "a.txt", base="")


if __name__ == "__main__":
    unittest.main()
