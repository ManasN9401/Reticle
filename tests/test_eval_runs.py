import asyncio
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("eval_runs", ROOT / "tools" / "eval_runs.py")
eval_runs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(eval_runs)
import browser_tools  # importable once eval_runs has put the compiler lib on the path

BROWSER = browser_tools.choose_backend(browser_tools.find_chromium())
FAKE_PNG = b"\x89PNG\r\n\x1a\n" + b"\0" * 2048


def web_case(**expect):
    return {"id": "fixture-web", "kind": "web", "depth": 3, "timeout_minutes": 1,
            "prompt": "Build a fixture page for the evaluation tests with a menu and an about section.",
            "expect": {"files": ["**/*.html", "**/*.css"], "mentions": ["Ember & Oak", "menu"], **expect}}

def python_case(**expect):
    return {"id": "fixture-py", "kind": "python", "depth": 1, "timeout_minutes": 1,
            "prompt": "Build a fixture Python project for the evaluation tests, with unittest tests.",
            "expect": {"files": ["**/*.py", "**/test*.py"], **expect}}

def write(root, files):
    for rel, content in files.items():
        path = Path(root) / "src" / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        (path.write_bytes if isinstance(content, bytes) else lambda c: path.write_text(c, encoding="utf-8"))(content)


class CaseDefinitions(unittest.TestCase):
    def test_the_shipped_cases_are_valid(self):
        cases = eval_runs.load_cases()
        self.assertGreaterEqual(len(cases), 5)
        self.assertEqual(len({case["id"] for case in cases}), len(cases))

    def test_invalid_cases_are_rejected(self):
        bad = [
            [web_case(), web_case()],                                   # duplicate id
            [{**python_case(), "expect": {"files": ["*.py"], "images": 2}}],  # images on a python case
            [{**web_case(), "depth": 2}],                               # not a Studio depth
            [{**web_case(), "expect": {"files": []}}],                  # nothing to deliver
            [{**web_case(), "extra": True}],                            # unknown key
        ]
        for cases in bad:
            with tempfile.TemporaryDirectory() as tmp, self.subTest(cases=cases):
                path = Path(tmp) / "cases.json"
                path.write_text(json.dumps(cases), encoding="utf-8")
                with self.assertRaises(ValueError):
                    eval_runs.load_cases(path)

    def test_a_case_edit_changes_its_digest(self):
        self.assertNotEqual(eval_runs.case_digest(web_case()), eval_runs.case_digest({**web_case(), "depth": 5}))


class Scoring(unittest.TestCase):
    def test_glob_matches_top_level_and_nested_files(self):
        self.assertTrue(eval_runs.glob_matches("index.html", "**/*.html"))
        self.assertTrue(eval_runs.glob_matches("site/pages/index.html", "**/*.html"))
        self.assertTrue(eval_runs.glob_matches("tests/test_cli.py", "**/test*.py"))
        self.assertFalse(eval_runs.glob_matches("cli.py", "**/test*.py"))

    def test_a_complete_page_passes_its_static_checks(self):
        with tempfile.TemporaryDirectory() as tmp:
            write(tmp, {
                "index.html": '<!doctype html><html><head><title>Ember &amp; Oak</title><link rel="stylesheet" href="style.css"></head>'
                              '<body><h1>Ember & Oak</h1><nav>menu</nav><img src="assets/hero.png" alt="hero"></body></html>',
                "style.css": "body { margin: 0; }",
                "assets/hero.png": FAKE_PNG,
            })
            result = eval_runs.score_workspace(web_case(images=1), tmp, browser=False)
        checks = result["checks"]
        for name in ("finished", "files_present", "no_placeholders", "mentions", "static_web", "images"):
            self.assertIs(checks[name], True, f"{name}: {result['details']}")
        self.assertIsNone(checks["page_loads"], "browser checks are not applicable when skipped")
        self.assertEqual(result["score"], 1.0)

    def test_missing_files_mentions_and_images_fail(self):
        with tempfile.TemporaryDirectory() as tmp:
            write(tmp, {"index.html": "<html><body>Hello</body></html>", "unused.png": FAKE_PNG})
            result = eval_runs.score_workspace(web_case(images=2), tmp, browser=False)
        self.assertFalse(result["checks"]["files_present"])
        self.assertEqual(result["details"]["missing_files"], ["**/*.css"])
        self.assertEqual(result["details"]["missing_mentions"], ["Ember & Oak", "menu"])
        self.assertFalse(result["checks"]["images"])
        self.assertEqual(result["details"]["images"]["unreferenced"], ["unused.png"], "an image no page uses does not count")
        self.assertLess(result["score"], 0.6)

    def test_images_are_not_applicable_when_comfyui_was_down(self):
        with tempfile.TemporaryDirectory() as tmp:
            write(tmp, {"index.html": "<html>Ember & Oak menu</html>", "style.css": ""})
            result = eval_runs.score_workspace(web_case(images=4), tmp, browser=False, image_generation=False)
        self.assertIsNone(result["checks"]["images"])
        self.assertIn("ComfyUI", result["details"]["images"]["skipped"])

    def test_a_run_with_no_output_scores_zero(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = eval_runs.score_workspace(web_case(), tmp, finished=False)
        self.assertEqual(result["score"], 0.0)
        self.assertFalse(result["checks"]["page_loads"])

    def test_placeholders_are_found_without_flagging_a_todo_app(self):
        hits = eval_runs.find_placeholders({
            "app.js": "const todoList = [];\n// TODO: implement filtering\nfunction addTodo() {}\n",
            "index.html": '<img src="https://picsum.photos/200">\n<p>Lorem ipsum dolor</p>\n',
        })
        self.assertEqual(hits, ["app.js:2: TODO/FIXME left in", "index.html:1: remote placeholder image", "index.html:2: lorem ipsum filler"])

    def test_python_syntax_errors_are_reported(self):
        with tempfile.TemporaryDirectory() as tmp:
            write(tmp, {"cli.py": "def main(:\n    pass\n", "test_cli.py": "import unittest\n"})
            result = eval_runs.score_workspace(python_case(), tmp)
        self.assertFalse(result["checks"]["python_compiles"])
        self.assertEqual(result["details"]["syntax_errors"], ["cli.py:1: invalid syntax"])

    def test_generated_tests_run_only_with_execute(self):
        passing = "import unittest\nclass T(unittest.TestCase):\n    def test_ok(self):\n        self.assertTrue(True)\n"
        failing = passing.replace("assertTrue(True)", "assertTrue(False)")
        command = {"test_command": ["-m", "unittest", "discover", "-v"]}
        for source, expected in ((passing, True), (failing, False)):
            with tempfile.TemporaryDirectory() as tmp, self.subTest(expected=expected):
                write(tmp, {"cli.py": "print('hi')\n", "test_cli.py": source, "README.md": "# cli\n"})
                self.assertIsNone(eval_runs.score_workspace(python_case(**command), tmp)["checks"]["tests_pass"])
                self.assertIs(eval_runs.score_workspace(python_case(**command), tmp, execute=True)["checks"]["tests_pass"], expected)

    def test_a_declared_but_uninstalled_dependency_is_not_a_failure(self):
        test = "import unittest\nimport not_installed_fixture_pkg\n"
        command = {"test_command": ["-m", "unittest", "discover"]}
        with tempfile.TemporaryDirectory() as tmp:
            write(tmp, {"app.py": "", "test_app.py": test, "requirements.txt": "not_installed_fixture_pkg==1.0\n"})
            declared = eval_runs.score_workspace(python_case(**command), tmp, execute=True)
        self.assertIsNone(declared["checks"]["tests_pass"])
        self.assertIn("not installed", declared["details"]["tests"]["skipped"])
        with tempfile.TemporaryDirectory() as tmp:
            write(tmp, {"app.py": "", "test_app.py": test, "requirements.txt": "flask\n"})
            undeclared = eval_runs.score_workspace(python_case(**command), tmp, execute=True)
        self.assertIs(undeclared["checks"]["tests_pass"], False, "importing a module nobody declared is the run's bug")

    @unittest.skipUnless(BROWSER, "no headless browser")
    def test_a_canvas_page_is_loaded_and_seen_animating(self):
        page = ("<!doctype html><html><head><title>Ember & Oak</title><link rel='stylesheet' href='style.css'></head><body>"
                "<p>menu</p><canvas id='c' width='300' height='200'></canvas><script>"
                "const g = document.getElementById('c').getContext('2d'); let t = 0;"
                "function draw() { t++; g.fillStyle = `hsl(${t % 360},80%,50%)`; g.fillRect(0, 0, 300, 200);"
                "g.fillStyle = '#000'; g.fillRect(t % 300, 50, 40, 40); requestAnimationFrame(draw); } draw();"
                "</script></body></html>")
        with tempfile.TemporaryDirectory() as tmp:
            write(tmp, {"index.html": page, "style.css": "body { background: #fff; }"})
            out = Path(tmp) / "out"
            out.mkdir()
            result = eval_runs.score_workspace(web_case(canvas=True), tmp, out_dir=out)
            self.assertTrue((out / "screenshot.png").is_file())
        checks = result["checks"]
        self.assertTrue(checks["page_loads"], result["details"].get("browser"))
        self.assertTrue(checks["no_page_errors"], result["details"].get("browser"))
        self.assertTrue(checks["animates"], result["details"].get("browser"))


class RuntimeRecords(unittest.TestCase):
    def approval(self, directory, task, name):
        content = f"# Approval required\n\n## Exact request\n\n```json\n{json.dumps({'task': task, 'inputs': []})}\n```\n".encode()
        (directory / name).write_bytes(content)
        return content

    def test_only_this_runs_checkpoints_are_approved_with_the_bound_hash(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp) / ".reticle" / "approvals"
            directory.mkdir(parents=True)
            mine = self.approval(directory, "exec-abc|pre-deployment-checkpoint", "approval_req_1.md")
            self.approval(directory, "compile-exec-abc|plan-review", "approval_req_2.md")
            self.approval(directory, "exec-abcd|someone-else", "approval_req_3.md")
            approved = {}
            eval_runs.approve_pending(tmp, "exec-abc", approved)
            self.assertEqual(sorted(approved.values()), ["compile-exec-abc|plan-review", "exec-abc|pre-deployment-checkpoint"])
            decision = json.loads((directory / "approval_req_1.md.decision.json").read_text(encoding="utf-8"))
            self.assertEqual(decision["hash"], hashlib.sha256(mine).hexdigest())
            self.assertEqual(decision["decision"], "APPROVED")
            self.assertFalse((directory / "approval_req_3.md.decision.json").exists(), "a run whose id merely starts the same is not ours")

    def test_run_metrics_count_nodes_attempts_and_failures(self):
        state = {"executions": {
            "exec-x": {"node_states": {"a": "done", "b": "failed"}, "attempts": {
                "a": [{"model": "ollama/qwen", "state": "succeeded"}],
                "b": [{"model": "ollama/qwen", "state": "failed", "reason": "exit_non_zero"},
                      {"model": "ollama/big", "state": "failed", "reason": "timeout"}]}},
            "compile-exec-x": {"node_states": {"arch": "done"}, "attempts": {"arch": [{"model": "ollama/qwen", "state": "succeeded"}]}},
        }}
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / ".reticle" / "executions"
            path.mkdir(parents=True)
            (path / "state.json").write_text(json.dumps(state), encoding="utf-8")
            metrics = eval_runs.run_metrics(tmp, "exec-x")
        self.assertEqual((metrics["nodes"], metrics["nodes_done"], metrics["attempts"], metrics["failed_attempts"]), (2, 1, 4, 2))
        self.assertEqual(metrics["models"], {"ollama/qwen": 3, "ollama/big": 1})
        self.assertEqual(metrics["failure_reasons"], {"exit_non_zero": 1, "timeout": 1})


class Reporting(unittest.TestCase):
    def document(self, run_id, scores, models):
        results = [{"case": case, "case_digest": "d", "score": score, "seconds": 600,
                    "checks": {"finished": score > 0, "page_loads": score >= 0.5}} for case, score in scores]
        return {"run_id": run_id, "environment": {"models": {"enabled": models}, "git": {"commit": "abc"}},
                "results": results, "summary": eval_runs.summarise(results)}

    def test_repeats_are_averaged(self):
        summary = eval_runs.summarise(self.document("a", [("x", 1.0), ("x", 0.5), ("x", 0.0)], [])["results"])
        self.assertEqual(summary["x"]["mean_score"], 0.5)
        self.assertEqual(summary["x"]["finished"], 2)
        self.assertEqual(summary["x"]["checks"]["page_loads"], [2, 3])

    def test_compare_reports_changes_and_warns_about_different_models(self):
        a = self.document("old", [("landing", 1.0), ("game", 0.4)], ["ollama/qwen2.5-coder:7b"])
        b = self.document("new", [("landing", 0.4), ("game", 0.4)], ["ollama/qwen2.5-coder:32b"])
        report = eval_runs.compare(a, b)
        self.assertIn("the enabled models differ", report)
        self.assertIn("| landing | 1.00 | 0.40 | -0.60 |", report)
        self.assertIn("page_loads 100%→0%", report)
        self.assertIn("treat a single-run difference", report)
        self.assertIn("Overall mean score: 0.40", eval_runs.markdown_summary({**b, "environment": {**b["environment"], "image_generation": False}}))


class LiveProtocol(unittest.TestCase):
    """drive_case against a stand-in Forge that speaks the runtime's websocket protocol."""

    def test_a_case_is_submitted_followed_and_its_checkpoint_approved(self):
        import websockets
        received, case = [], {**web_case(), "timeout_minutes": 1}
        with tempfile.TemporaryDirectory() as tmp:
            approvals = Path(tmp) / ".reticle" / "approvals"
            approvals.mkdir(parents=True)

            async def forge(ws):
                async for raw in ws:
                    message = json.loads(raw)
                    received.append(message)
                    item = {"id": "exec-live", "group": message["group"], "status": "RUNNING"}
                    await ws.send(json.dumps({"Type": "WaitlistUpdated", "Payload": {"items": [item]}}))
                    content = f"```json\n{json.dumps({'task': 'exec-live|checkpoint'})}\n```\n"
                    (approvals / "approval_req_live.md").write_text(content, encoding="utf-8")
                    while not (approvals / "approval_req_live.md.decision.json").exists():
                        await asyncio.sleep(0.05)
                    await ws.send(json.dumps({"Type": "WaitlistUpdated", "Payload": {"items": [{**item, "status": "COMPLETED"}]}}))

            async def scenario():
                async with websockets.serve(forge, "127.0.0.1", 0) as server:
                    port = server.sockets[0].getsockname()[1]
                    async with asyncio.timeout(20):  # a protocol mismatch fails here instead of hanging the suite
                        return await eval_runs.drive_case(case, port=port, token="t", reticle_root=tmp, group="eval-g",
                                                          auto_approve=True, log=lambda _line: None)

            exec_id, status, _seconds, approved = asyncio.run(scenario())
        self.assertEqual((exec_id, status), ("exec-live", "COMPLETED"))
        self.assertEqual(list(approved.values()), ["exec-live|checkpoint"])
        self.assertEqual(received[0]["action"], "enqueue")
        self.assertEqual((received[0]["group"], received[0]["agent_complexity"]), ("eval-g", 3))


if __name__ == "__main__":
    unittest.main()
