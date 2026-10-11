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
import forge_utils
import worker_sdk


def tool_response(name, args):
    call = SimpleNamespace(id="call", function=SimpleNamespace(name=name, arguments=json.dumps(args)))
    message = SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role": "assistant", "tool_calls": [{"id": "call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
    return SimpleNamespace(choices=[SimpleNamespace(message=message)])


class LoadSkillInstructions(unittest.TestCase):
    def test_applied_skills_are_appended_and_sized_and_missing_ones_are_named(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "alpha").mkdir()
            (root / "alpha" / "SKILL.md").write_text("# Alpha\nbe careful", encoding="utf-8")
            report = forge_utils.load_skill_instructions(root, ["alpha", "deps-only"])
        self.assertEqual(report["text"], "\n\n# Alpha\nbe careful")
        self.assertEqual(report["applied"], [["alpha", len("# Alpha\nbe careful")]])
        self.assertEqual(report["missing"], ["deps-only"])

    def test_an_id_that_could_leave_the_skills_folder_is_refused(self):
        with tempfile.TemporaryDirectory() as temp:
            for bad in ("../secret", "a/b", "", "..", "x y"):
                with self.assertRaises(ValueError):
                    forge_utils.load_skill_instructions(temp, [bad])

    def test_every_repository_skill_with_a_manifest_loads(self):
        root = Path(__file__).resolve().parents[1] / "skills"
        report = forge_utils.load_skill_instructions(root, ["frontend-uiux", "verification-loop", "clickhouse-io"])
        self.assertEqual(report["missing"], [])
        self.assertEqual([name for name, _ in report["applied"]], ["frontend-uiux", "verification-loop", "clickhouse-io"])


class SkillStatusInTheLLMPanel(unittest.TestCase):
    def run_worker(self, **options):
        script = [
            tool_response("write_file", {"path": "a.js", "content": "1"}),
            tool_response("read_file", {"path": "a.js"}),
            tool_response("mark_task_complete", {"summary": "done"}),
        ]

        def completion(**kwargs):
            return script.pop(0)

        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "src").mkdir()
            request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp}, "parameters": {"llm_model": "ollama/fixture"}}
            errors = io.StringIO()
            with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                    patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false"}, clear=True), \
                    patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                    patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                    patch.object(sys, "stdout", io.StringIO()), patch.object(sys, "stderr", errors):
                worker_sdk.run("Fixture", kind="writing", **options)
        statuses = []
        for line in errors.getvalue().splitlines():
            if line.startswith("[LLM_STREAM] "):
                event = json.loads(line[len("[LLM_STREAM] "):])
                if event["kind"] == "status":
                    statuses.append(event["text"])
        return statuses

    def test_the_applied_skills_and_their_sizes_are_shown(self):
        statuses = self.run_worker(skills={"applied": [["frontend-uiux", 4660], ["verification-loop", 1500]], "missing": ["llm-worker"]})
        self.assertIn("Skills applied: frontend-uiux (4.7 KB), verification-loop (1.5 KB)", statuses)
        self.assertTrue(any(text.startswith("Skills with no instruction file") and "llm-worker" in text for text in statuses))

    def test_an_agent_with_no_skills_says_so(self):
        statuses = self.run_worker(skills={"applied": [], "missing": []})
        self.assertIn("Skills applied: none", statuses)

    def test_a_worker_that_does_not_report_skills_stays_quiet_and_a_bad_report_is_harmless(self):
        self.assertFalse([text for text in self.run_worker() if text.startswith("Skills")])
        self.assertFalse([text for text in self.run_worker(skills={"applied": "oops", "missing": None}) if text.startswith("Skills applied")])


if __name__ == "__main__":
    unittest.main()
