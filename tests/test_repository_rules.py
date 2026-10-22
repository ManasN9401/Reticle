import json
from pathlib import Path
import sys
import tempfile
import unittest

LIB = Path(__file__).resolve().parents[1]/"cmd/forge/compiler/lib"
sys.path.insert(0,str(LIB))
import repository_rules


class RepositoryRules(unittest.TestCase):
    def test_precedence_and_scope_do_not_load_sibling_guidance(self):
        with tempfile.TemporaryDirectory() as workspace:
            root=Path(workspace)/"src"
            (root/"backend").mkdir(parents=True)
            (root/"frontend").mkdir()
            for path,text in (("AGENTS.md","Root conventions"),("RETICLE.md","Root-specific overrides"),
                              ("backend/AGENTS.md","Backend guidance"),("frontend/AGENTS.md","Frontend guidance")):
                (root/path).write_text(text,encoding="utf-8")
            report=repository_rules.resolve_rules(workspace,["backend/new.py"])
            self.assertEqual([s["path"] for s in report["sources"]],["AGENTS.md","RETICLE.md","backend/AGENTS.md"])
            self.assertEqual(report["sources"][-1]["scope"],"backend")
            self.assertNotIn("Frontend guidance",str(report))
            self.assertIn("cannot expand permissions",report["precedence"])
            saved=repository_rules.save_provenance(workspace,report)
            metadata=json.loads((Path(workspace)/saved).read_text())
            self.assertNotIn("content",metadata[0])
            self.assertEqual(len(metadata[0]["sha256"]),64)
            self.assertEqual(repository_rules.save_provenance(workspace,report),saved)
            (root/"AGENTS.md").write_text("Changed conventions",encoding="utf-8")
            changed=repository_rules.resolve_rules(workspace,["backend/new.py"])
            self.assertNotEqual(changed["sources"][0]["sha256"],report["sources"][0]["sha256"])

    def test_oversized_guidance_fails_without_silently_cutting_constraints(self):
        with tempfile.TemporaryDirectory() as workspace:
            root=Path(workspace)/"src";root.mkdir()
            (root/"AGENTS.md").write_text("A"*(repository_rules.MAX_RULE_BYTES+1))
            with self.assertRaisesRegex(ValueError,"exceeds"):
                repository_rules.resolve_rules(workspace)

    def test_scopes_cannot_escape_the_workspace(self):
        with tempfile.TemporaryDirectory() as workspace:
            with self.assertRaises(ValueError):
                repository_rules.resolve_rules(workspace,["../outside/app.py"])


class WorkerGuidanceIntegration(unittest.TestCase):
    def test_new_scope_is_reviewed_before_write_and_keeps_original_task(self):
        from test_worker_awareness import Harness,tool_response
        harness=Harness()
        script=[tool_response("write_file",{"path":"backend/output.txt","content":"done"}),
                tool_response("write_file",{"path":"backend/output.txt","content":"done"}),
                tool_response("read_file",{"path":"backend/output.txt"}),
                tool_response("mark_task_complete",{"summary":"done"})]
        failure,errors,remaining,seen,summary=harness.run_worker(script,
            "Create or update exactly these workspace files: none declared.\n",
            existing={"AGENTS.md":"Root instructions", "backend/AGENTS.md":"Backend instructions"})
        self.assertIsNone(failure,errors)
        self.assertEqual(remaining,0)
        self.assertIn("Scoped repository guidance changed",errors)
        context=json.loads(seen["messages"][1]["content"])
        sources=context["repository_guidance"]["sources"]
        self.assertEqual([source["path"] for source in sources],["AGENTS.md","backend/AGENTS.md"])
        self.assertTrue(summary)
