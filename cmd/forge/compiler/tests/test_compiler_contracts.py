import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest


COMPILER = Path(__file__).resolve().parents[1]


def load_architect():
    # The pure compiler helpers do not need a provider client. Keeping the unit
    # test independent of the runtime environment also catches accidental
    # provider work during validation.
    litellm = types.ModuleType("litellm")
    litellm.completion = lambda *args, **kwargs: (_ for _ in ()).throw(
        AssertionError("validation must not call a provider")
    )
    sys.modules.setdefault("litellm", litellm)
    path = COMPILER / "agents" / "architect-agent" / "workers" / "architect.py"
    spec = importlib.util.spec_from_file_location("reticle_architect", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class CompilerContractsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.architect = load_architect()

    def fixture(self):
        return {
            "workflow_name": "fixture",
            "agents": [
                {"id": "existing", "description": "maintained", "is_new": False,
                 "system_prompt": "TBD", "inputs": [], "outputs": [], "memory": [], "skills": []},
                {"id": "generated", "description": "write the result", "is_new": True,
                 "system_prompt": "TBD", "inputs": [], "outputs": [], "memory": [], "skills": []},
            ],
            "nodes": [
                {"id": "first", "agent_id": "existing", "input_files": [], "output_files": ["plan.md"]},
                {"id": "second", "agent_id": "generated", "input_files": ["plan.md"], "output_files": ["result.txt"]},
            ],
            "edges": [{"from": "first", "to": "second"}],
        }

    def test_validates_file_dependencies_and_builds_bounded_prompt(self):
        dag = self.fixture()
        self.architect.validate_dag(dag, {"existing"}, set())
        prompt = self.architect.build_agent_prompt(dag["agents"][1], dag, "make fixture")
        self.assertIn("plan.md", prompt)
        self.assertIn("result.txt", prompt)
        self.assertLess(len(prompt), 2000)

    def test_rejects_input_without_upstream_producer(self):
        dag = self.fixture()
        dag["edges"] = []
        with self.assertRaisesRegex(ValueError, "not an upstream dependency"):
            self.architect.validate_dag(dag, {"existing"}, set())

    def test_single_agent_depth_rejects_multiple_nodes(self):
        with self.assertRaisesRegex(ValueError, "exactly one node"):
            self.architect.validate_dag(self.fixture(), {"existing"}, set(), agent_complexity=1)

    def test_codegen_never_replaces_registered_agent_with_tbd_prompt(self):
        dag = self.fixture()
        with tempfile.TemporaryDirectory() as tmp:
            request = {
                "id": "code-1",
                "execution": "compile-fixture",
                "inputs": [{"name": "DAG JSON", "data": json.dumps(dag)}],
                "memory": {"workspace_dir": tmp},
            }
            for relative in (
                Path("agents/coder-agent/workers/coder.py"),
                Path("agents/scaffolder-agent/workers/scaffolder.py"),
            ):
                result = subprocess.run(
                    [sys.executable, str(COMPILER / relative)],
                    input=json.dumps(request), text=True, capture_output=True, check=False,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
            root = Path(tmp)
            self.assertFalse((root / "agents" / "existing").exists())
            self.assertTrue((root / "agents" / "generated" / "workers" / "generated.py").is_file())
            self.assertTrue((root / "agents" / "generated" / "generated.yaml").is_file())


if __name__ == "__main__":
    unittest.main()
