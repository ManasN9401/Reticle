"""Regression cases from the production-readiness audit; no live providers."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

LIB = Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"
sys.path.insert(0, str(LIB))
import forge_utils
import worker_sdk


class Hardening(unittest.TestCase):
    def test_browser_startup_failure_contains_bounded_diagnostics(self):
        import browser_tools
        with tempfile.TemporaryDirectory() as profile:
            (Path(profile)/"browser-startup.log").write_bytes(b"x"*5000+b"crash server failed")
            process = SimpleNamespace(poll=lambda: 23)
            result = browser_tools._startup_failure(profile, process)
            self.assertIn("exit 23", result)
            self.assertIn("crash server failed", result)
            self.assertLess(len(result), 4200)

    def test_container_network_is_disabled_without_a_grant(self):
        for allowed in (False, True):
            with tempfile.TemporaryDirectory() as workspace:
                (Path(workspace)/"src").mkdir()
                with patch.object(forge_utils, "_ensure_worker_image", return_value=""), \
                     patch.object(forge_utils, "detect_ml_host_environment", return_value="linux"), \
                     patch.object(forge_utils.subprocess, "run", return_value=SimpleNamespace(returncode=0)) as run:
                    forge_utils.execute_terminal_command("true", workspace, allow_network=allowed)
                argv = run.call_args_list[0].args[0]
                self.assertEqual("--network" in argv, not allowed)
                if not allowed:
                    self.assertEqual(argv[argv.index("--network")+1], "none")

    def test_command_container_respects_read_only_workspace_grant(self):
        for writable in (False, True):
            with tempfile.TemporaryDirectory() as workspace:
                (Path(workspace)/"src").mkdir()
                with patch.object(forge_utils, "_ensure_worker_image", return_value=""), \
                     patch.object(forge_utils, "detect_ml_host_environment", return_value="linux"), \
                     patch.object(forge_utils.subprocess, "run", return_value=SimpleNamespace(returncode=0)) as run:
                    forge_utils.execute_terminal_command("true", workspace, allow_workspace_write=writable)
                argv = run.call_args_list[0].args[0]
                mount = argv[argv.index("--mount")+1]
                self.assertEqual(mount.endswith(",readonly"), not writable)

    def test_unresolved_browser_hosts_fail_closed(self):
        import browser_tools
        with patch.object(browser_tools, "_addresses", return_value=[]):
            self.assertFalse(browser_tools.request_allowed("https://unresolved.example/path", "GET", 8000)[0])

    def test_version_help_and_listing_do_not_verify_work(self):
        for command in ("pytest --version", "pytest --help", "pytest --collect-only",
                        "npm test -- --help", "tsc --showConfig", "vitest --passWithNoTests"):
            with self.subTest(command=command):
                self.assertFalse(worker_sdk._meaningful_terminal_verification("backend", command.split()))
        self.assertTrue(worker_sdk._meaningful_terminal_verification("backend", ["pytest", "-v", "tests"]))

    def test_process_death_releases_edit_lock(self):
        with tempfile.TemporaryDirectory() as workspace:
            target = Path(workspace)/"src"/"a.txt"
            target.parent.mkdir()
            target.write_text("before", encoding="utf-8")
            script = "import sys,os; sys.path.insert(0,sys.argv[1]); import forge_utils; forge_utils._rewrite_locked('a.txt',sys.argv[2],lambda old:os._exit(23),'done')"
            child = subprocess.run([sys.executable, "-B", "-c", script, str(LIB), workspace], capture_output=True)
            self.assertEqual(child.returncode, 23, child.stderr)
            result = forge_utils.replace_file_content("a.txt", "before", "after", workspace)
            self.assertIn("Successfully", result)
            self.assertEqual(target.read_text(), "after")

    def test_live_lock_excludes_other_edits(self):
        with tempfile.TemporaryDirectory() as workspace:
            target = Path(workspace)/"src"/"a.txt"
            target.parent.mkdir()
            target.write_text("before")
            lock = forge_utils._acquire_edit_lock(target)
            try:
                result = forge_utils.replace_file_content("a.txt", "before", "after", workspace)
                self.assertIn("Error acquiring edit lock", result)
                self.assertEqual(target.read_text(), "before")
            finally:
                lock.close()
            self.assertIn("reserved", forge_utils.delete_path("a.txt.reticle-lock", workspace))
            self.assertNotIn("reticle-lock", forge_utils.list_dir(".", workspace))
