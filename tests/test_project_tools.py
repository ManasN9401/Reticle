"""Environment policy and real HTTP/process lifecycle checks without Docker or APIs."""
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/"cmd/forge/compiler/lib"))
import browser_tools
import forge_utils
import project_tools


class Environments(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)/"src"
        self.root.mkdir()
        self.environments = project_tools.ProjectEnvironments(self.temp.name, network=True, writable=True)
        self.addCleanup(self.environments.close)
        self.image_id = "sha256:"+"a"*64

    def project(self, files):
        for name, text in files.items():
            target = self.root/name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(text, encoding="utf-8")

    def preparation(self, path=".", profile="auto", output="Exit code: 0\nok", callback=None):
        def execute(command, workspace, **options):
            if callback:
                callback(command, options)
            return output
        with patch.dict(os.environ, {"RETICLE_NODE_IMAGE": "reviewed/node:locked", "RETICLE_PYTHON_IMAGE": "reviewed/python:locked", "RETICLE_GO_IMAGE": "reviewed/go:locked"}), \
             patch.object(forge_utils, "_ensure_worker_image", return_value=""), \
             patch.object(project_tools.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout=self.image_id.encode())) as inspect, \
             patch.object(forge_utils, "execute_terminal_command", side_effect=execute) as commands:
            result = self.environments.prepare(path=path, profile=profile)
        return result, commands, inspect

    def test_locked_node_subproject_reuses_immutable_image(self):
        self.project({"app space/package.json": '{"packageManager":"npm@11"}', "app space/package-lock.json": "{}"})
        result, commands, _ = self.preparation("app space")
        self.assertTrue(result["ok"])
        self.assertEqual(commands.call_args.args[0], ["npm", "ci", "--no-audit", "--no-fund"])
        self.assertEqual(commands.call_args.kwargs["command_cwd"], "app space")
        self.assertEqual(self.environments.options("app space")["execution_image"], self.image_id)
        self.assertEqual(self.environments.options("."), {})
        self.project({"app space/package-lock.json": '{"changed":true}'})
        with self.assertRaisesRegex(ValueError, "changed"):
            self.environments.options("app space")

    def test_python_and_go_keep_dependency_caches_between_commands(self):
        for profile, files, install in (
            ("python", {"pyproject.toml": "", "uv.lock": ""}, ["uv", "sync", "--frozen", "--no-managed-python"]),
            ("go", {"go.mod": "module fixture", "go.sum": ""}, ["go", "mod", "download"])):
            with self.subTest(profile=profile):
                self.project(files)
                result, commands, _ = self.preparation(profile=profile)
                self.assertTrue(result["ok"])
                self.assertEqual(commands.call_args.args[0], install)
                environment = self.environments.options()["command_environment"]
                self.assertEqual(commands.call_args.kwargs["command_environment"], {k:v for k,v in environment.items() if k != "PATH"})
                if profile == "python":
                    self.assertTrue(environment["PATH"].startswith("/workspace/src/.venv/bin:"))

    def test_unsupported_unlocked_and_unconfigured_projects_fail_before_execution(self):
        self.project({"package.json": "{}"})
        with patch.dict(os.environ, {"RETICLE_NODE_IMAGE": ""}):
            with self.assertRaisesRegex(ValueError, "RETICLE_NODE_IMAGE"):
                self.environments.prepare()
        with patch.dict(os.environ, {"RETICLE_NODE_IMAGE": "reviewed/node"}):
            with self.assertRaisesRegex(ValueError, "package-lock"):
                self.environments.prepare()
        self.project({"package-lock.json": "{}", "pnpm-lock.yaml": ""})
        with patch.dict(os.environ, {"RETICLE_NODE_IMAGE": "reviewed/node"}):
            with self.assertRaisesRegex(ValueError, "Multiple"):
                self.environments.prepare()

    def test_partial_install_never_keeps_previous_lease(self):
        self.project({"package.json": "{}", "package-lock.json": "{}"})
        self.preparation()
        result, _, _ = self.preparation(output="Exit code: 1\ninstall failed")
        self.assertFalse(result["ok"])
        self.assertEqual(self.environments.options(), {})

    def test_install_that_changes_manifest_does_not_register(self):
        self.project({"package.json": "{}", "package-lock.json": "{}"})
        with self.assertRaisesRegex(ValueError, "changed during"):
            self.preparation(callback=lambda command, _: self.project({"package-lock.json": "changed"}) if command[1] == "ci" else None)
        self.assertEqual(self.environments.prepared, {})

    def test_grants_and_traversal_are_enforced(self):
        for network, writable in ((False, True), (True, False)):
            with self.assertRaises(PermissionError):
                project_tools.ProjectEnvironments(self.temp.name, network=network, writable=writable).prepare()
        with self.assertRaises(ValueError):
            self.environments.prepare(path="../escape")
        with self.assertRaisesRegex(ValueError, "container mode"):
            project_tools.ProjectEnvironments(self.temp.name, native=True).prepare()

    def test_prepared_environment_excludes_other_attempts_until_closed(self):
        self.project({"package.json":"{}", "package-lock.json":"{}"})
        self.preparation()
        other = project_tools.ProjectEnvironments(self.temp.name, network=True, writable=True)
        self.addCleanup(other.close)
        with patch.dict(os.environ, {"RETICLE_NODE_IMAGE":"reviewed/node"}):
            with self.assertRaisesRegex(ValueError,"Another attempt"):
                other.prepare()
        self.environments.close()
        self.assertEqual(self.environments.options(),{})
        # Reacquisition proves the OS-owned lease is released, not just hidden.
        lease = forge_utils._acquire_edit_lock(self.root/".reticle-environment")
        lease.close()

    def test_toolchain_launch_has_owner_and_restricted_mount(self):
        owner = "b"*32
        with patch.dict(os.environ, {"RETICLE_RESOURCE_OWNER": owner, "RETICLE_RESOURCE_MARKER": ""}), \
             patch.object(forge_utils, "_ensure_worker_image", return_value=""), \
             patch.object(forge_utils, "detect_ml_host_environment", return_value="linux"):
            argv, _, _, _ = forge_utils.command_launch(["node", "--version"], self.temp.name,
                execution_image=self.image_id, allow_workspace_write=False,
                command_environment={"NPM_CONFIG_CACHE": "/workspace/src/cache"})
        self.assertIn(self.image_id, argv)
        self.assertIn("reticle.owner="+owner, argv)
        self.assertEqual(argv[argv.index("--network")+1], "none")
        self.assertTrue(argv[argv.index("--mount")+1].endswith(",readonly"))


class ServerLifecycle(unittest.TestCase):
    """Run real local children through a fixture launcher, not a Docker acceptance claim."""
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)/"src"
        self.root.mkdir()
        (self.root/"index.html").write_text("<h1>fixture ready</h1>")
        self.servers = project_tools.ManagedServers(project_tools.ProjectEnvironments(self.temp.name, network=True, writable=True))
        self.addCleanup(self.servers.close)
        self.port = self.free_port()
        def fixture_launch(command, workspace, **options):
            # Keep the production supervisor/readiness/logging logic, substitute the container launch only.
            return command, {"cwd": self.root}, None, []
        self.launch = patch.object(forge_utils, "command_launch", side_effect=fixture_launch)
        self.launch.start()
        self.addCleanup(self.launch.stop)

    @staticmethod
    def free_port():
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            return listener.getsockname()[1]

    def start(self, **options):
        command = [sys.executable, "-u", "-m", "http.server", str(self.port), "--bind", "127.0.0.1"]
        return self.servers.start(command, self.port, timeout_seconds=3, **options)

    def test_ready_logs_one_server_handle_and_cleanup(self):
        result = self.start()
        server_id = result["id"]
        self.assertEqual(result["ready_status"], 200)
        self.assertEqual(self.servers.site(server_id), self.port)
        connection = http.client.HTTPConnection("127.0.0.1", self.port, timeout=1)
        try:
            connection.request("GET", "/")
            self.assertIn(b"fixture ready", connection.getresponse().read())
        finally:
            connection.close()
        with self.assertRaisesRegex(ValueError, "current server"):
            self.start()
        self.servers.close()
        self.assertFalse(self.servers.logs(server_id)["running"])
        self.assertIn("GET /", self.servers.logs(server_id)["output"])
        with self.assertRaisesRegex(ValueError, "Unknown"):
            self.servers.logs("another-attempt")
        with self.assertRaises(ValueError):
            self.servers.site(server_id)

    def test_deadline_cleans_up_even_when_process_does_not_serve_http(self):
        with self.assertRaises(TimeoutError):
            self.servers.start([sys.executable, "-u", "-c", "import time; print('not ready'); time.sleep(60)"], self.port, timeout_seconds=1)
        self.assertTrue(all(e["process"].poll() is not None for e in self.servers.servers.values()))

    def test_early_exit_reports_error_and_large_logs_stay_bounded(self):
        with self.assertRaisesRegex(RuntimeError, "exited"):
            self.servers.start([sys.executable, "-u", "-c", "print('x'*200000); raise SystemExit(7)"], self.port, timeout_seconds=3)
        entry = next(iter(self.servers.servers.values()))
        self.assertLessEqual(len(entry["log"]), 65536)
        self.assertEqual(entry["process"].returncode, 7)

    def test_occupied_port_and_invalid_scope_are_rejected(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", self.port))
            listener.listen()
            with self.assertRaises(OSError):
                self.start()
        self.assertEqual(self.servers.servers, {})
        self.servers.environments.network = False
        with self.assertRaises(PermissionError):
            self.start()
        self.servers.environments.native = True
        with self.assertRaisesRegex(ValueError, "container mode"):
            self.start()

    def test_container_readiness_checks_runtime_state_before_http(self):
        command = [sys.executable, "-u", "-m", "http.server", str(self.port), "--bind", "127.0.0.1"]
        def docker(command, **options):
            return SimpleNamespace(returncode=0, stdout=b"true\n", stderr=b"")
        with patch.object(forge_utils, "command_launch", return_value=(command, {"cwd":self.root}, "reticle-fixture", [])), \
             patch.object(project_tools.subprocess, "run", side_effect=docker) as run:
            result = self.start()
            self.servers.close()
        self.assertEqual(result["ready_status"],200)
        self.assertTrue(any(call.args[0][:2] == ["docker","inspect"] for call in run.call_args_list))
        self.assertTrue(any(call.args[0][:3] == ["docker","rm","-f"] for call in run.call_args_list))
        # Do not ask the real Docker daemon to remove a fixture on test cleanup.
        self.servers.servers.clear()

    def test_failed_docker_cleanup_is_not_success_even_if_client_exited(self):
        entry={"process":SimpleNamespace(poll=lambda:7), "container":"reticle-fixture"}
        self.servers.servers["owned"] = entry
        with patch.object(project_tools.subprocess,"run",return_value=SimpleNamespace(returncode=1,stdout=b"",stderr=b"daemon unavailable")):
            with self.assertRaisesRegex(RuntimeError,"cleanup failed"):
                self.servers.stop("owned")
        self.servers.servers.clear()

    def test_managed_browser_keeps_origin_checks_and_path(self):
        result = self.start()
        with patch.object(browser_tools, "find_chromium", return_value="fixture"), \
             patch.object(browser_tools, "choose_backend", return_value="cdp"), \
             patch.object(browser_tools, "_cdp_session", return_value=b"") as observe:
            report = browser_tools.run_page(self.temp.name, path="/route?q=1", managed_port=self.servers.site(result["id"]))
        site = observe.call_args.args[2]
        self.assertEqual(site.url("ignored"), result["url"]+"/route?q=1")
        self.assertIn("managed-server", report)
        self.assertFalse(browser_tools.request_allowed("http://127.0.0.1:1/private", "GET", self.port)[0])
        self.assertTrue(browser_tools.run_page(self.temp.name, path="https://example.org", managed_port=self.port).startswith("Error"))


if __name__ == "__main__":
    unittest.main()
