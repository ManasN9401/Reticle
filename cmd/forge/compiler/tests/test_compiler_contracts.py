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
        self.architect.validate_dag(dag, {"existing"}, set(), agent_complexity=3)
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
            self.architect.validate_dag(dag, {"existing"}, set(), 3, capabilities)

    def test_build_agent_prompt_resolves_registered_agents_too(self):
        # The architect is told to leave every agent's system_prompt as "TBD"
        # (both new and pre-registered) on the assumption this step fills them
        # all in afterward. A pre-registered agent (is_new: False) reused from
        # the built-in catalog must get a real, task-specific prompt exactly
        # like a newly generated one — otherwise its worker never learns the
        # user's goal or its declared output files and can pass verification
        # without producing them.
        dag = self.fixture()
        self.architect.validate_dag(dag, {"existing"}, set(), agent_complexity=3)
        prompt = self.architect.build_agent_prompt(dag["agents"][0], dag, "make fixture")
        self.assertNotEqual(prompt.strip(), "TBD")
        self.assertIn("plan.md", prompt)
        self.assertIn("make fixture", prompt)

    def test_rejects_input_without_upstream_producer(self):
        dag = self.fixture()
        dag["edges"] = []
        with self.assertRaisesRegex(ValueError, "not an upstream dependency"):
            self.architect.validate_dag(dag, {"existing"}, set(), agent_complexity=3)

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

    def three_node_dag(self):
        dag = self.fixture()
        dag["nodes"].append({"id": "third", "agent_id": "generated", "input_files": ["result.txt"], "output_files": ["final.md"]})
        dag["edges"].append({"from": "second", "to": "third"})
        return dag

    def test_deep_depth_requires_a_decomposed_graph(self):
        # "deep" used to say "choose the smallest graph", so a single node was valid.
        single = self.fixture()
        single["nodes"] = single["nodes"][:1]
        single["edges"] = []
        for depth in (4, 5):
            for dag in (single, self.fixture()):
                with self.assertRaisesRegex(ValueError, "Deep workflow depth requires at least 3 nodes"):
                    self.architect.validate_dag(dag, {"existing"}, set(), agent_complexity=depth)
            self.architect.validate_dag(self.three_node_dag(), {"existing"}, set(), agent_complexity=depth)

    def test_shallower_depths_keep_their_limits(self):
        # Balanced still allows two nodes, and single-agent still requires exactly one.
        self.architect.validate_dag(self.fixture(), {"existing"}, set(), agent_complexity=3)
        with self.assertRaisesRegex(ValueError, "exactly one node"):
            self.architect.validate_dag(self.three_node_dag(), {"existing"}, set(), agent_complexity=1)

    def test_depth_instructions_describe_each_graph_shape(self):
        single = self.architect.depth_instruction(1)
        balanced = self.architect.depth_instruction(3)
        deep = self.architect.depth_instruction(5)
        self.assertIn("EXACTLY 1 single agent", single)
        self.assertIn("2-5 agents", balanced)
        self.assertIn("DEEP workflow", deep)
        self.assertIn("between 3 and 16 nodes", deep)
        self.assertIn("Do NOT collapse the work into a single node", deep)
        self.assertNotIn("smallest graph", deep)
        self.assertEqual(self.architect.depth_instruction(4), deep)

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
            self.architect.validate_dag(dag, {"builder", "reviewer"}, set(), 3, None, None, catalog)
        dag["nodes"][0]["output_files"] = ["site/index.html"]
        # A review-only node on an agent without workspace.write stays valid with no outputs.
        self.architect.validate_dag(dag, {"builder", "reviewer"}, set(), 3, None, None, catalog)
        # Without a capability map nothing is assumed about registered agents.
        dag["nodes"][0]["output_files"] = []
        self.architect.validate_dag(dag, {"builder", "reviewer"}, set(), agent_complexity=3)

    def test_generated_agent_capabilities_come_from_the_dag(self):
        dag = self.fixture()
        dag["nodes"][1]["output_files"] = []
        with self.assertRaisesRegex(ValueError, "Node second .* declares no output_files"):
            self.architect.validate_dag(dag, {"existing"}, set(), agent_complexity=3)
        dag["agents"][1]["capabilities"] = ["workspace.read"]
        self.architect.validate_dag(dag, {"existing"}, set(), agent_complexity=3)

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

    def two_node_generated_agent_dag(self):
        # The observed shape: one generated agent owns the first and the last node.
        agent = lambda name: {"id": name, "description": f"{name} work", "is_new": True, "system_prompt": "TBD",
                              "inputs": [], "outputs": [], "memory": [], "skills": [], "capabilities": ["workspace.read", "workspace.write"]}
        return {
            "agents": [agent("designer"), agent("assets"), agent("backend")],
            "nodes": [
                {"id": "design", "agent_id": "designer", "input_files": [], "output_files": ["src/spec.md"]},
                {"id": "thumbs", "agent_id": "assets", "input_files": ["src/spec.md"], "output_files": ["src/thumbs.json"]},
                {"id": "api", "agent_id": "backend", "input_files": ["src/spec.md"], "output_files": ["src/server.js"]},
                {"id": "assemble", "agent_id": "designer", "input_files": ["src/thumbs.json", "src/server.js"], "output_files": ["src/index.html"]},
            ],
            "edges": [{"from": "design", "to": "thumbs"}, {"from": "design", "to": "api"},
                      {"from": "thumbs", "to": "assemble"}, {"from": "api", "to": "assemble"}],
        }

    def test_an_agent_owning_several_nodes_is_not_given_a_merged_prompt(self):
        dag = self.two_node_generated_agent_dag()
        designer = dag["agents"][0]
        merged = self.architect.build_agent_prompt(designer, dag, "make a gallery")
        # What used to be baked into the agent: every node's files, with both neighbours on both sides.
        self.assertIn("src/index.html", merged)
        self.assertIn("src/spec.md", merged)
        generic = self.architect.build_agent_prompt(designer, dag, "make a gallery", generic=True)
        for path in ("src/index.html", "src/spec.md", "src/thumbs.json", "Upstream nodes", "Downstream nodes"):
            self.assertNotIn(path, generic)
        self.assertIn("make a gallery", generic)
        self.assertIn("once for each workflow node", generic)

    def test_each_node_of_a_multi_node_generated_agent_gets_only_its_own_task(self):
        dag = self.two_node_generated_agent_dag()
        self.architect.attach_node_prompts(dag, "make a gallery")
        design, thumbs, api, assemble = dag["nodes"]
        first = design["parameters"]["system_prompt"]
        last = assemble["parameters"]["system_prompt"]
        # The first node builds only the specification and has nothing upstream.
        self.assertIn("exactly these workspace files: src/spec.md", first)
        self.assertNotIn("index.html", first)
        self.assertIn("Upstream nodes are: none declared", first)
        self.assertIn("Downstream nodes are: api, thumbs", first)
        # The last node assembles from its two inputs and has nothing downstream.
        self.assertIn("exactly these workspace files: src/index.html", last)
        self.assertIn("Read these workspace files: src/server.js, src/thumbs.json", last)
        self.assertIn("Upstream nodes are: api, thumbs", last)
        self.assertIn("Downstream nodes are: none declared", last)
        # Agents that own a single node keep their prompt in their own script and are not duplicated.
        self.assertNotIn("parameters", thumbs)
        self.assertNotIn("parameters", api)

    def test_the_architect_bakes_a_generic_prompt_only_for_multi_node_agents(self):
        dag = self.two_node_generated_agent_dag()
        for agent in dag["agents"]:
            agent["system_prompt"] = self.architect.build_agent_prompt(agent, dag, "g", generic=self.architect.owns_several_nodes(dag, agent["id"]))
        by_id = {agent["id"]: agent["system_prompt"] for agent in dag["agents"]}
        self.assertNotIn("src/index.html", by_id["designer"])
        self.assertIn("exactly these workspace files: src/thumbs.json", by_id["assets"])
        self.assertIn("exactly these workspace files: src/server.js", by_id["backend"])

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
