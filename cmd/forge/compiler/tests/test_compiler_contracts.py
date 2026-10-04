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
from unittest import mock


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


def load_comfy_tools():
    toolset = types.ModuleType("forge_utils")
    toolset.safe_path = lambda root, path: Path(root) / path
    previous = sys.modules.get("forge_utils")
    sys.modules["forge_utils"] = toolset
    try:
        path = COMPILER / "lib" / "comfy_tools.py"
        spec = importlib.util.spec_from_file_location("reticle_comfy_tools", path)
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
        cls.comfy_tools = load_comfy_tools()

    def fixture(self):
        return {
            "workflow_name": "fixture",
            "agents": [
                {"id": "existing", "description": "maintained", "is_new": False,
                 "system_prompt": "TBD", "inputs": [], "outputs": [], "memory": [], "skills": [], "capabilities": []},
                {"id": "generated", "description": "write the result", "is_new": True,
                 "system_prompt": "TBD", "inputs": [], "outputs": [], "memory": [], "skills": [],
                 "capabilities": ["workspace.read", "workspace.write"]},
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

    def test_catalog_uses_manifest_ids_and_validates_capabilities(self):
        raw = json.dumps({
            "agents": [{"id": "browser-agent", "description": "browse"}],
            "skills": [{"id": "llm-worker", "description": "model client"}],
            "capabilities": ["workspace.read", "workspace.write"],
            "tools": [{"id": "read_file", "capability": "workspace.read"}],
            "mcp_servers": [],
        })
        _, agents, skills, capabilities, mcp_servers = self.architect.load_architect_catalog(raw)
        self.assertEqual(agents, {"browser-agent"})
        self.assertEqual(skills, {"llm-worker"})
        self.assertEqual(capabilities, {"workspace.read", "workspace.write"})
        self.assertEqual(mcp_servers, set())
        dag = self.fixture()
        dag["agents"][1]["capabilities"].append("network.imaginary")
        with self.assertRaisesRegex(ValueError, "unavailable capabilities"):
            self.architect.validate_dag(dag, {"existing"}, set(), 5, capabilities)

    def test_build_agent_prompt_resolves_registered_agents_too(self):
        # The architect is told to leave every agent's system_prompt as "TBD"
        # (both new and pre-registered) on the assumption this step fills them
        # all in afterward. A pre-registered agent (is_new: False) reused from
        # the built-in catalog must get a real, task-specific prompt exactly
        # like a newly generated one — otherwise its worker never learns the
        # user's goal or its declared output files and can pass verification
        # without producing them.
        dag = self.fixture()
        self.architect.validate_dag(dag, {"existing"}, set())
        prompt = self.architect.build_agent_prompt(dag["agents"][0], dag, "make fixture")
        self.assertNotEqual(prompt.strip(), "TBD")
        self.assertIn("plan.md", prompt)
        self.assertIn("make fixture", prompt)

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
            # A generated worker runs from its own copy of the SDK, so every sibling
            # module the SDK imports must be copied too; a missing one crashes the
            # worker before it starts and is not retried (image_tools once was missing).
            worker_dir = root / "agents" / "generated" / "workers"
            lib_modules = {path.stem for path in (COMPILER / "lib").glob("*.py")}
            sdk_lines = (worker_dir / "worker_sdk.py").read_text(encoding="utf-8").splitlines()
            imported = {
                line.split()[1].split(".")[0]
                for line in sdk_lines
                if line.strip().startswith(("import ", "from ")) and len(line.split()) > 1
            }
            # rag_tools is only imported for the rag worker kind, which generated agents never use.
            for module in sorted((imported & lib_modules) - {"rag_tools"}):
                self.assertTrue((worker_dir / (module + ".py")).is_file(), module + " was not copied next to the generated worker")
            generated = json.loads((root / "agents" / "generated" / "generated.yaml").read_text())
            self.assertEqual(generated["capabilities"], ["workspace.read", "workspace.write"])

    def test_write_capable_agents_must_declare_outputs(self):
        catalog = {"builder": {"workspace.read", "workspace.write"}, "reviewer": {"workspace.read"}}
        dag = {
            "agents": [{"id": "builder", "is_new": False}, {"id": "reviewer", "is_new": False}],
            "nodes": [
                {"id": "build", "agent_id": "builder", "input_files": [], "output_files": []},
                {"id": "review", "agent_id": "reviewer", "input_files": [], "output_files": []},
            ],
            "edges": [{"from": "build", "to": "review"}],
        }
        with self.assertRaisesRegex(ValueError, "Node build .* declares no output_files"):
            self.architect.validate_dag(dag, {"builder", "reviewer"}, set(), 5, None, None, catalog)
        dag["nodes"][0]["output_files"] = ["site/index.html"]
        # A review-only node on an agent without workspace.write stays valid with no outputs.
        self.architect.validate_dag(dag, {"builder", "reviewer"}, set(), 5, None, None, catalog)
        # Without a capability map nothing is assumed about registered agents.
        dag["nodes"][0]["output_files"] = []
        self.architect.validate_dag(dag, {"builder", "reviewer"}, set())

    def test_generated_agent_capabilities_come_from_the_dag(self):
        dag = self.fixture()
        dag["nodes"][1]["output_files"] = []
        with self.assertRaisesRegex(ValueError, "Node second .* declares no output_files"):
            self.architect.validate_dag(dag, {"existing"}, set())
        dag["agents"][1]["capabilities"] = ["workspace.read"]
        self.architect.validate_dag(dag, {"existing"}, set())

    def test_registered_agent_nodes_receive_their_own_task_prompt(self):
        dag = self.fixture()
        dag["agents"][0]["description"] = "builds the site"
        dag["nodes"].append({"id": "third", "agent_id": "existing", "input_files": ["result.txt"], "output_files": ["final.md"]})
        dag["edges"].append({"from": "second", "to": "third"})
        dag["nodes"][2]["parameters"] = {"effort": "high"}
        self.architect.attach_node_prompts(dag, "make a gallery")
        first, second, third = dag["nodes"]
        # A registered agent's node carries a prompt scoped to that node only.
        self.assertIn("make a gallery", first["parameters"]["system_prompt"])
        self.assertIn("plan.md", first["parameters"]["system_prompt"])
        self.assertNotIn("final.md", first["parameters"]["system_prompt"])
        self.assertIn("final.md", third["parameters"]["system_prompt"])
        # Existing parameters survive, and generated agents (which bake the prompt into their worker) are untouched.
        self.assertEqual(third["parameters"]["effort"], "high")
        self.assertNotIn("parameters", second)

    def test_node_prompt_does_not_override_or_trust_malformed_parameters(self):
        dag = self.fixture()
        dag["nodes"][0]["parameters"] = {"system_prompt": "custom"}
        self.architect.attach_node_prompts(dag, "goal")
        self.assertEqual(dag["nodes"][0]["parameters"]["system_prompt"], "custom")
        dag = self.fixture()
        dag["nodes"][0]["parameters"] = "not an object"
        self.architect.attach_node_prompts(dag, "goal")
        self.assertIn("goal", dag["nodes"][0]["parameters"]["system_prompt"])

    def test_scaffolder_carries_the_node_prompt_into_the_workflow(self):
        dag = self.fixture()
        self.architect.attach_node_prompts(dag, "make fixture")
        with tempfile.TemporaryDirectory() as tmp:
            request = {"id": "scaff-1", "execution": "compile-fixture", "inputs": [{"name": "DAG JSON", "data": json.dumps(dag)}], "memory": {"workspace_dir": tmp}}
            result = subprocess.run(
                [sys.executable, str(COMPILER / "agents/scaffolder-agent/workers/scaffolder.py")],
                input=json.dumps(request), text=True, capture_output=True, check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            workflow = json.loads((Path(tmp) / "workflows" / "workflow_fixture.yaml").read_text())
            first = next(node for node in workflow["nodes"] if node["id"] == "first")
            self.assertIn("plan.md", first["parameters"]["system_prompt"])

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
            {"num_ctx": 8192, "keep_alive": "5m", "max_tokens": 4096, "temperature": 0.1},
        )

    def test_llm_controls_are_not_exposed_as_shared_memory_facts(self):
        context = self.worker_sdk.shared_memory_context({
            "project_fact": "keep me",
            "llm_num_ctx": 8192,
            "llm_max_tokens": 4096,
            "llm_temperature": 0.1,
            "llm_first_token_timeout_seconds": 180,
            "ollama_keep_alive": "5m",
        })
        self.assertEqual(context, {"project_fact": "keep me"})

    def test_local_skill_compaction_removes_fenced_examples_and_preserves_directives(self):
        source = "Agent contract\n" + ("plain prose\n" * 2000) + "# Required\n- Verify output\n```python\nsecret_example()\n```\n"
        compacted = self.worker_sdk._compact_local_instructions(source, limit=500)
        self.assertLessEqual(len(compacted), 570)
        self.assertIn("Agent contract", compacted)
        self.assertNotIn("secret_example", compacted)

    def test_comfy_release_uses_dynamic_local_endpoint(self):
        calls = []
        with mock.patch.dict("os.environ", {"COMFYUI_HOST": "http://localhost:9000"}), \
             mock.patch.object(self.comfy_tools, "_post_json", side_effect=lambda *args, **kwargs: calls.append((args, kwargs))):
            self.comfy_tools.release_comfy_models(log=lambda _message: None)
        self.assertEqual(calls[0][0], (
            "http://localhost:9000/free",
            {"unload_models": True, "free_memory": True},
        ))

    def test_ollama_models_are_discovered_and_dynamically_unloaded(self):
        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _limit):
                return json.dumps({"models": [{"name": "qwen:7b"}, {"model": "coder:3b"}]}).encode()

        calls = []
        with mock.patch.dict("os.environ", {"OLLAMA_HOST": "http://127.0.0.1:11434"}), \
             mock.patch.object(self.comfy_tools.urllib.request, "urlopen", return_value=Response()), \
             mock.patch.object(self.comfy_tools, "_post_json", side_effect=lambda *args, **kwargs: calls.append((args, kwargs))):
            self.comfy_tools.unload_ollama_models(log=lambda _message: None)
        self.assertEqual(
            [call[0] for call in calls],
            [
                ("http://127.0.0.1:11434/api/generate", {"model": "coder:3b", "keep_alive": 0}),
                ("http://127.0.0.1:11434/api/generate", {"model": "qwen:7b", "keep_alive": 0}),
            ],
        )

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

    def test_tool_signature_ignores_argument_formatting_and_call_order(self):
        first = [
            {"function": {"name": "read_file", "arguments": '{"path": "design-spec.md"}'}},
            {"function": {"name": "list_dir", "arguments": '{"path":"."}'}},
        ]
        reordered = [
            {"function": {"name": "list_dir", "arguments": '{ "path" : "." }'}},
            {"function": {"name": "read_file", "arguments": '{"path":"design-spec.md"}'}},
        ]
        self.assertEqual(
            self.worker_sdk._tool_call_signature(first),
            self.worker_sdk._tool_call_signature(reordered),
        )
        signature, rounds = self.worker_sdk._tool_repeat_state(None, 0, first)
        signature, rounds = self.worker_sdk._tool_repeat_state(signature, rounds, reordered)
        signature, rounds = self.worker_sdk._tool_repeat_state(signature, rounds, first)
        self.assertEqual(rounds, 3)
        self.assertEqual(self.worker_sdk._tool_repeat_state(signature, rounds, []), (None, 0))


if __name__ == "__main__":
    unittest.main()
