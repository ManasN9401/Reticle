import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

LIB = Path(__file__).resolve().parents[1]/"cmd/forge/compiler/lib"
sys.path.insert(0, str(LIB))
import diagnostics_tools as diagnostics
import forge_utils


class Diagnostics(unittest.TestCase):
    def project(self, workspace, fails=False):
        root = Path(workspace)/"src"
        (root/"tests").mkdir(parents=True)
        test = root/"tests"/"test_smoke.py"
        test.write_text("import unittest\nclass Smoke(unittest.TestCase):\n    def test_truth(self):\n        self.assertTrue(" + str(not fails) + ")\n", encoding="utf-8")
        return root, test

    def test_real_unittest_run_produces_hash_bound_receipt(self):
        with tempfile.TemporaryDirectory() as workspace:
            root, test = self.project(workspace)
            result = diagnostics.run_diagnostics(workspace, "test", tracked_paths=["tests/test_smoke.py"],
                        allow_native=True, attempt_id="attempt", task_id="task")
            self.assertTrue(result["ok"], result)
            self.assertEqual(result["checks"][0]["test_count"], 1)
            receipt = json.loads((Path(workspace)/result["receipt"]).read_text(encoding="utf-8"))
            self.assertEqual(receipt["attempt_id"], "attempt")
            self.assertEqual(receipt["checked_files"]["tests/test_smoke.py"], hashlib.sha256(test.read_bytes()).hexdigest())
            self.assertTrue(receipt["artifacts_unchanged"])
            captured = json.loads(diagnostics.read_diagnostics(workspace, result["receipt"], task_id="task"))
            self.assertIn("Ran 1 test", captured["output"])
            with self.assertRaises(PermissionError):
                diagnostics.read_diagnostics(workspace, result["receipt"], task_id="different-task")
            with self.assertRaises(ValueError):
                diagnostics.read_diagnostics(workspace, "../outside.json", task_id="task")
            self.assertTrue(diagnostics.receipt_matches(workspace, result))
            test.write_text("changed after validation", encoding="utf-8")
            self.assertFalse(diagnostics.receipt_matches(workspace, result))
            self.assertNotIn("__pycache__", forge_utils.find_files("**/*", workspace))

    def test_real_failure_returns_location_and_cannot_verify(self):
        with tempfile.TemporaryDirectory() as workspace:
            self.project(workspace, fails=True)
            result = diagnostics.run_diagnostics(workspace, "test", allow_native=True)
            self.assertFalse(result["ok"])
            self.assertEqual(result["checks"][0]["exit_code"], 1)
            self.assertTrue(any(error["line"] == 4 and error["file"].endswith("test_smoke.py") for error in result["errors"]), result)

    def test_empty_suite_and_mutations_during_checks_are_rejected(self):
        for output, mutate in (("Exit code: 0\nRan 0 tests in 0.00s\nOK", False),
                               ("Exit code: 0\nRan 1 test in 0.01s\nOK", True)):
            with tempfile.TemporaryDirectory() as workspace:
                _, test = self.project(workspace)
                def run(*args, **kwargs):
                    if mutate:
                        test.write_text("changed", encoding="utf-8")
                    return output
                with patch.object(forge_utils, "execute_terminal_command", side_effect=run):
                    result = diagnostics.run_diagnostics(workspace, "test", tracked_paths=["tests/test_smoke.py"])
                self.assertFalse(result["ok"], result)

    def test_unknown_project_does_not_guess_a_command(self):
        with tempfile.TemporaryDirectory() as workspace, patch.object(forge_utils, "execute_terminal_command") as run:
            (Path(workspace)/"src").mkdir()
            result = diagnostics.run_diagnostics(workspace)
            self.assertIn("Nothing to run", result["summary"])
            self.assertIsNone(result["receipt"])
            run.assert_not_called()

    def test_detected_npm_invocation_uses_argv_and_keeps_project_path(self):
        with tempfile.TemporaryDirectory() as workspace:
            project=Path(workspace)/"src"/"project with spaces"
            project.mkdir(parents=True)
            (project/"package.json").write_text(json.dumps({"scripts":{"test":"vitest run", "typecheck":"tsc --noEmit"}}))
            plans, _=diagnostics.detect_diagnostics(workspace, path="project with spaces")
            self.assertEqual(plans[0]["argv"], ["npm", "--prefix", "project with spaces", "run", "test"])
            with self.assertRaises(ValueError):
                diagnostics.detect_diagnostics(workspace, path="../outside")

    def test_parsers_preserve_failure_locations_and_observed_test_counts(self):
        fixtures = (("src/app.ts:8:3: error TS123: broken", "src/app.ts", 8, 3),
                    ("src/app.ts(8,3): error TS123: broken", "src/app.ts", 8, 3),
                    ("src/app.py:12: invalid syntax", "src/app.py", 12, 0))
        for text, file, line, column in fixtures:
            parsed=diagnostics.parse_diagnostic_output(text,"typecheck")
            self.assertEqual((parsed["errors"][0]["file"],parsed["errors"][0]["line"],parsed["errors"][0]["column"]),(file,line,column))
        for text in ("3 passed in 0.1s", "Tests: 2 passed, 2 total", "Tests 4 passed (4)"):
            self.assertTrue(diagnostics.parse_diagnostic_output(text, "test")["tests_observed"])
        self.assertFalse(diagnostics.parse_diagnostic_output("pytest 8.0.0", "test")["tests_observed"])

    def test_package_manager_and_project_working_directory_are_preserved(self):
        for manager, lock in (("pnpm", "pnpm-lock.yaml"), ("yarn", "yarn.lock"), ("bun", "bun.lock")):
            with self.subTest(manager=manager), tempfile.TemporaryDirectory() as workspace:
                project = Path(workspace)/"src"/"project with spaces"
                project.mkdir(parents=True)
                (project/"package.json").write_text(json.dumps({"scripts": {"test": "fixture"}}))
                (project/lock).write_text("")
                plans, _ = diagnostics.detect_diagnostics(workspace, "test", "project with spaces")
                self.assertEqual(plans[0]["tool"], manager)
                self.assertEqual(plans[0]["cwd"], "project with spaces")
                with patch.object(forge_utils, "execute_terminal_command", return_value="Exit code: 0\n1 passed") as run:
                    result = diagnostics.run_diagnostics(workspace, "test", "project with spaces")
                self.assertTrue(result["ok"])
                self.assertEqual(run.call_args.kwargs["command_cwd"], "project with spaces")

    def test_python_config_enables_lint_typecheck_and_build_without_installing(self):
        with tempfile.TemporaryDirectory() as workspace:
            root = Path(workspace)/"src"; root.mkdir()
            (root/"pyproject.toml").write_text('[tool.ruff]\n[tool.mypy]\n[build-system]\nrequires = ["setuptools"]\n')
            plans, _ = diagnostics.detect_diagnostics(workspace)
            self.assertEqual([plan["tool"] for plan in plans], ["pytest", "mypy", "ruff", "build"])
            self.assertIn("--no-isolation", plans[-1]["argv"])
            (root/"package.json").write_text(json.dumps({"scripts": {"test": "fixture"}}))
            (root/"pnpm-lock.yaml").write_text("")
            (root/"yarn.lock").write_text("")
            with self.assertRaisesRegex(ValueError, "Multiple"):
                diagnostics.detect_diagnostics(workspace)

    def test_large_diagnostic_reply_remains_valid_json_with_receipt_reference(self):
        report = {"ok": False, "summary": "Checks failed", "receipt": "diagnostics/"+"a"*32+".json",
                  "checked_files": {f"file-{i}": "x"*64 for i in range(500)},
                  "errors": [{"file": "a.py", "line": i, "column": 1, "message": "error"*200} for i in range(100)],
                  "checks": [{"ok": False, "tool": "pytest", "output_excerpt": "x"*4000}] * 4}
        encoded = diagnostics.model_report(report, budget=2000)
        self.assertLessEqual(len(encoded), 2000)
        parsed = json.loads(encoded)
        self.assertEqual(parsed["checked_file_count"], 500)
        self.assertEqual(parsed["receipt"], report["receipt"])
        self.assertTrue(parsed["details_truncated"])

    def test_native_npm_diagnostic_runs_a_real_script_in_a_subproject(self):
        import shutil
        if not shutil.which("npm") or not shutil.which("node"):
            self.skipTest("npm and node are required for this adapter integration test")
        with tempfile.TemporaryDirectory() as workspace:
            project = Path(workspace)/"src"/"project with spaces"; project.mkdir(parents=True)
            (project/"package.json").write_text(json.dumps({"scripts": {"typecheck": "node --check app.js"}}))
            (project/"app.js").write_text("const invalid = ;")
            result = diagnostics.run_diagnostics(workspace, "typecheck", "project with spaces", allow_native=True)
            self.assertFalse(result["ok"], result)
            self.assertEqual(result["checks"][0]["exit_code"], 1)
            self.assertIn("SyntaxError", result["checks"][0]["output_excerpt"])
            self.assertIn("app.js", result["checks"][0]["output_excerpt"])
