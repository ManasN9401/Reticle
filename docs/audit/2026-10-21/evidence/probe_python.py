"""Offline audit probes; no providers, browser launch, Docker, or outbound requests.

Run from the repository root: python -B docs/audit/2026-10-21/evidence/probe_python.py
The output describes current behavior, not assertions that the defects are correct.
"""
import json
import ipaddress
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / "cmd/forge/compiler/lib"))
import browser_tools
import forge_utils
import rag_tools
import worker_sdk


class Collection:
    def __init__(self):
        self.rows = []

    def add(self, **kwargs):
        self.rows.extend(kwargs["metadatas"])


class Client:
    def __init__(self):
        self.collections = {}

    def create_collection(self, name, **kwargs):
        self.collections[name] = Collection()
        return self.collections[name]

    def get_collection(self, name, **kwargs):
        return self.collections[name]

    def delete_collection(self, name):
        del self.collections[name]


def main():
    # Use the writable checkout; Windows app-container TEMP can be inaccessible.
    temporary_root = Path(__file__).resolve().parent
    with tempfile.TemporaryDirectory(prefix="reticle-audit-", dir=temporary_root) as tmp:
        base = Path(tmp)
        result = {}
        indexed = {}
        for label, workspace in (("ordinary", base / "ordinary"), ("production_layout", base / ".reticle/sessions/fixture")):
            (workspace / "src").mkdir(parents=True)
            (workspace / "src/report.md").write_text("The audit fixture has one document.", encoding="utf-8")
            client = Client()
            with patch.object(rag_tools, "_client", return_value=client), patch.object(rag_tools, "_embedding", return_value=None):
                indexed[label] = rag_tools.index_directory(".", str(workspace))
        result["rag_path_comparison"] = indexed

        result["inspection_classifier"] = {command: worker_sdk._is_inspection_command(command) for command in (
            "git branch audit-probe", "git branch -D audit-probe", "rg --pre ./helper needle .", "tsc --noEmit --incremental",
        )}
        result["verification_classifier"] = {command: worker_sdk._meaningful_terminal_verification("coding", command.split()) for command in (
            "pytest --version", "tsc --version", "eslint --help", "npm run build",
        )}

        def response(name, arguments):
            call = SimpleNamespace(id=name, function=SimpleNamespace(name=name, arguments=json.dumps(arguments)))
            message = SimpleNamespace(content=None, tool_calls=[call], model_dump=lambda **kw: {"role": "assistant"})
            return SimpleNamespace(choices=[SimpleNamespace(message=message)])

        replies = iter([
            response("write_file", {"path": "broken.py", "content": "def broken(\n"}),
            response("execute_terminal_command", {"command": "pytest --version"}),
            response("mark_task_complete", {"summary": "complete"}),
        ])
        request = {"id": "audit", "memory": {"workspace_dir": str(base / "verification")}, "parameters": {"llm_model": "openai/audit-fixture"}, "capabilities": ["workspace.read", "workspace.write", "process.container"]}
        with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=lambda **kw: next(replies))}), patch.object(worker_sdk, "_broker_catalog", return_value={}), patch.object(forge_utils, "execute_terminal_command", return_value="Exit code: 0\npytest 9.0.0"), patch.object(sys, "stdin", io.StringIO(json.dumps(request))), patch.object(sys, "stdout", io.StringIO()) as output, patch.object(sys, "stderr", io.StringIO()):
            worker_sdk.run("Write a Python program and verify it.")
        completed = json.loads(output.getvalue())
        result["invalid_python_completed_after_version_probe"] = completed["verification"]

        stale_workspace = base / "stale-lock"
        (stale_workspace / "src").mkdir(parents=True)
        (stale_workspace / "src/file.txt").write_text("before", encoding="utf-8")
        child = "import os,sys; sys.path.insert(0,sys.argv[1]); import forge_utils; forge_utils._rewrite_locked('file.txt',sys.argv[2],lambda old:os._exit(23),'changed')"
        stopped = subprocess.run([sys.executable, "-B", "-c", child, str(ROOT / "cmd/forge/compiler/lib"), str(stale_workspace)], capture_output=True)
        result["interrupted_edit"] = {
            "child_exit": stopped.returncode,
            "lock_remains": (stale_workspace / "src/file.txt.reticle-lock").exists(),
            "next_edit": forge_utils.replace_file_content("file.txt", "before", "after", str(stale_workspace)),
        }

        # A public IP is a stand-in only; request_allowed does not make a request.
        with patch.object(browser_tools, "_addresses", return_value=[ipaddress.ip_address("8.8.8.8")]):
            result["browser_get_with_data_allowed"] = browser_tools.request_allowed("https://audit.example.invalid/pixel?data=fixture-only", "GET", 12345)
        with patch.object(browser_tools, "_addresses", return_value=[]):
            result["browser_unresolved_host_allowed"] = browser_tools.request_allowed("https://audit.example.invalid/", "GET", 12345)

        calls = []

        def fake_run(argv, **kwargs):
            calls.append(argv)
            return SimpleNamespace(returncode=0)

        with patch.object(forge_utils, "_ensure_worker_image", return_value=""), patch.object(forge_utils, "detect_ml_host_environment", return_value="windows"), patch.object(forge_utils.subprocess, "run", side_effect=fake_run), patch.dict(os.environ, {"RETICLE_ML_PROFILE": "cpu", "RETICLE_GPU_DEVICES": ""}):
            forge_utils.execute_terminal_command("echo fixture", str(base / "container"), False)
        docker_run = next(argv for argv in calls if argv[:2] == ["docker", "run"])
        result["container_command_network_flags"] = [arg for arg in docker_run if arg.startswith(("--network", "--net="))]

        plugin_skill = base / ".reticle/plugins/audit-plugin/skills/audit-plugin-only-skill/SKILL.md"
        plugin_skill.parent.mkdir(parents=True)
        plugin_skill.write_text("AUDIT_PLUGIN_INSTRUCTION_SENTINEL", encoding="utf-8")
        compiled_workspace = base / "compiled"
        compiled_workspace.mkdir()
        compiler_request = {"id":"compile-fixture", "memory":{"workspace_dir":str(compiled_workspace)}, "inputs":[{"name":"DAG JSON", "data":json.dumps({"agents":[{"is_new":True, "id":"fixture", "system_prompt":"Do the task", "skills":["audit-plugin-only-skill"]}]})}]}
        compiled = subprocess.run([sys.executable, "-B", str(ROOT / "cmd/forge/compiler/agents/coder-agent/workers/coder.py")], input=json.dumps(compiler_request), text=True, capture_output=True)
        result["plugin_instruction_discovery"] = {
            "compiler_exit": compiled.returncode,
            "diagnostic": compiled.stderr.strip(),
            "plugin_text_present": "AUDIT_PLUGIN_INSTRUCTION_SENTINEL" in (compiled_workspace / "agents/fixture/workers/fixture.instructions.md").read_text(encoding="utf-8"),
            "qualification": "Compiler-side fixture; plugin registration/enablement was not invoked. Source review confirms registry metadata does not carry an instruction path.",
        }
        result["scope"] = "Local and mocked policy probes only. No browser, container, API, real secret, or network request."
        print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
