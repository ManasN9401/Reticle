import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from contextlib import redirect_stderr


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


def load_worker_sdk():
    toolset = types.ModuleType("forge_utils")
    toolset._definitions = {}
    previous = sys.modules.get("forge_utils")
    sys.modules["forge_utils"] = toolset
    try:
        path = COMPILER / "lib" / "worker_sdk.py"
        spec = importlib.util.spec_from_file_location("reticle_worker_sdk", path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module
    finally:
        if previous is None:
            sys.modules.pop("forge_utils", None)
        else:
            sys.modules["forge_utils"] = previous


class CompilerContractsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.architect = load_architect()
        cls.worker_sdk = load_worker_sdk()

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

    def test_context_window_option_is_only_sent_to_ollama(self):
        memory = {
            "llm_num_ctx": 8192,
            "llm_max_tokens": 4096,
            "llm_temperature": 0.1,
        }
        for model in (
            "groq/openai/gpt-oss-20b",
            "gemini/gemini-3.1-flash-lite",
            "openrouter/nvidia/nemotron-3.5-lightning:free",
            "llama/local-model",
        ):
            with self.subTest(model=model):
                options = self.worker_sdk.generation_options(model, memory)
                self.assertNotIn("num_ctx", options)
                self.assertEqual(options["max_tokens"], 4096)
                self.assertEqual(options["temperature"], 0.1)

        self.assertEqual(
            self.worker_sdk.generation_options("ollama/qwen3:8b", memory),
            {"num_ctx": 8192, "max_tokens": 4096, "temperature": 0.1},
        )

    def test_llm_controls_are_not_exposed_as_shared_memory_facts(self):
        context = self.worker_sdk.shared_memory_context({
            "project_fact": "keep me",
            "llm_num_ctx": 8192,
            "llm_max_tokens": 4096,
            "llm_temperature": 0.1,
        })
        self.assertEqual(context, {"project_fact": "keep me"})

    def test_reasoning_diagnostics_accept_standard_litellm_shapes(self):
        direct = types.SimpleNamespace(reasoning_content="inspect inputs")
        blocks = {
            "thinking_blocks": [
                {"type": "thinking", "thinking": "compare "},
                {"type": "thinking", "thinking": "outputs", "signature": "not-displayable"},
            ]
        }
        for module in (self.architect, self.worker_sdk):
            with self.subTest(module=module.__name__):
                self.assertEqual(module._llm_reasoning(direct), "inspect inputs")
                self.assertEqual(module._llm_reasoning(blocks), "compare outputs")

    def test_worker_llm_diagnostic_is_single_line_structured_json(self):
        captured = io.StringIO()
        with redirect_stderr(captured):
            self.worker_sdk._emit_llm("reasoning", "first\nsecond")
        line = captured.getvalue().strip()
        self.assertTrue(line.startswith("[LLM_STREAM] "))
        self.assertEqual(
            json.loads(line.removeprefix("[LLM_STREAM] ")),
            {"kind": "reasoning", "text": "first\nsecond"},
        )


if __name__ == "__main__":
    unittest.main()
