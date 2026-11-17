"""Attempt-owned project environments and services. No detached shell jobs."""
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import threading
import time
import uuid

import forge_utils


class ProjectEnvironments:
    def __init__(self, workspace, native=False, network=False, writable=False):
        self.workspace, self.native = workspace, native
        self.network, self.writable = network, writable
        self.prepared = {}
        self.leases = {}

    def _project(self, path):
        root = forge_utils.safe_path(self.workspace, ".")
        project = forge_utils.safe_path(self.workspace, path)
        if not project.is_dir():
            raise ValueError("Project path must be a workspace directory")
        return project, project.relative_to(root).as_posix()

    def _identity(self, relative):
        names = ("package.json", "package-lock.json", "pyproject.toml", "uv.lock", "go.mod", "go.sum")
        hashes = {}
        for name in names:
            target = forge_utils.safe_path(self.workspace, (Path(relative)/name).as_posix(), literal=True)
            if target.is_file():
                with target.open("rb") as stream:
                    raw = stream.read(forge_utils.MAX_FILE + 1)
                if len(raw) > forge_utils.MAX_FILE:
                    raise ValueError("Project dependency file exceeds 1 MiB: " + name)
                hashes[name] = hashlib.sha256(raw).hexdigest()
        return hashes

    def options(self, path="."):
        _, relative = self._project(path)
        record = self.prepared.get(relative)
        if not record:
            return {}
        if record["dependencies"] != self._identity(relative):
            raise ValueError("Dependency configuration changed; call prepare_environment again")
        prefix = "/workspace/src" + ("/"+relative if relative != "." else "")
        environment = self._cache_environment(record["profile"], prefix)
        if record["profile"] == "python":
            environment["PATH"] = prefix+"/.venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
        return {"execution_image": record["image_id"], "command_environment": environment}

    @staticmethod
    def _cache_environment(profile, prefix):
        if profile == "go":
            return {"GOMODCACHE": prefix+"/.reticle-go-cache/mod", "GOCACHE": prefix+"/.reticle-go-cache/build"}
        if profile == "node":
            return {"NPM_CONFIG_CACHE": prefix+"/.reticle-npm-cache"}
        return {"UV_CACHE_DIR": prefix+"/.reticle-uv-cache"}

    def prepare(self, path=".", profile="auto"):
        if self.native:
            raise ValueError("Prepared environments currently require container mode; native commands use the operator's host environment")
        if not self.writable or not self.network:
            raise PermissionError("Preparation requires workspace.write and network.public for dependency installation")
        project, relative = self._project(path)
        dependencies = self._identity(relative)
        if profile == "auto":
            profiles = [kind for kind, name in (("node", "package.json"), ("python", "pyproject.toml"), ("go", "go.mod")) if name in dependencies]
            if len(profiles) != 1:
                raise ValueError("Select node, python or go explicitly for an ambiguous or unsupported project")
            profile = profiles[0]
        if profile not in {"node", "python", "go"}:
            raise ValueError("profile must be auto, node, python or go")
        image = os.getenv("RETICLE_"+profile.upper()+"_IMAGE", "").strip()
        if not image:
            raise ValueError("Configure RETICLE_"+profile.upper()+"_IMAGE with a reviewed toolchain image before preparation")
        if image.startswith("-") or any(char.isspace() for char in image):
            raise ValueError("Invalid configured toolchain image")
        required = {"node": ("package.json", "package-lock.json"), "python": ("pyproject.toml", "uv.lock"), "go": ("go.mod", "go.sum")}[profile]
        if any(name not in dependencies for name in required):
            raise ValueError("Locked preparation requires " + " and ".join(required))
        if profile == "node":
            import diagnostics_tools
            config = json.loads(diagnostics_tools._small_text(project/"package.json"))
            if diagnostics_tools._javascript_manager(config, self.workspace, relative) != "npm":
                raise ValueError("Node preparation currently supports npm ci; pnpm/Yarn/Bun need a reviewed prepared image")
        if relative not in self.leases:
            try:
                self.leases[relative] = forge_utils._acquire_edit_lock(project/".reticle-environment")
            except OSError:
                raise ValueError("Another attempt owns this project's prepared environment; use a separate project/worktree or wait for it to finish") from None
        # A partial install must never leave the previous prepared lease active.
        self.prepared.pop(relative, None)
        problem = forge_utils._ensure_worker_image(image)
        if problem:
            raise RuntimeError(problem)
        result = subprocess.run(["docker", "image", "inspect", "--format", "{{.Id}}", image], capture_output=True, timeout=30)
        image_id = result.stdout.decode("utf-8", errors="replace").strip()
        if result.returncode or not re.fullmatch(r"sha256:[a-f0-9]{64}", image_id):
            raise RuntimeError("Could not resolve an immutable toolchain image identity")
        install = {"node": ["npm", "ci", "--no-audit", "--no-fund"],
                   "python": ["uv", "sync", "--frozen", "--no-managed-python"],
                   "go": ["go", "mod", "download"]}[profile]
        checks = {"node": [["node", "--version"], ["npm", "--version"]],
                  "python": [["python", "--version"], ["uv", "--version"]],
                  "go": [["go", "version"]]}[profile]
        outputs = []
        for command in checks+[install]:
            output = forge_utils.execute_terminal_command(command, self.workspace, allow_network=True,
                command_cwd=relative, allow_workspace_write=True, execution_image=image_id,
                command_environment=self._cache_environment(profile, "/workspace/src"+("/"+relative if relative != "." else "")))
            outputs.append({"argv": command, "output": output[-4000:]})
            if not output.startswith("Exit code: 0\n"):
                return {"ok": False, "profile": profile, "path": relative, "checks": outputs,
                        "summary": "Environment preparation failed; no prepared environment registered"}
        if dependencies != self._identity(relative):
            raise ValueError("Dependency files changed during preparation; review and prepare again")
        record = {"profile": profile, "path": relative, "image": image, "image_id": image_id,
                  "dependencies": dependencies, "checks": outputs}
        self.prepared[relative] = record
        return {"ok": True, **record, "summary": "Prepared dependencies in the project; this attempt will use the resolved image"}


    def close(self):
        for lease in self.leases.values():
            lease.close()
        self.leases.clear()
        self.prepared.clear()


class ManagedServers:
    """One active server per worker; handles never cross attempt boundaries."""
    def __init__(self, environments):
        self.environments = environments
        self.servers = {}

    def _entry(self, server_id):
        entry = self.servers.get(server_id)
        if entry is None:
            raise ValueError("Unknown server handle for this attempt")
        return entry

    def logs(self, server_id, tail=100):
        if isinstance(tail, bool) or not isinstance(tail, int) or not 1 <= tail <= 200:
            raise ValueError("tail must be between 1 and 200 lines")
        entry = self._entry(server_id)
        with entry["lock"]:
            text = bytes(entry["log"]).decode("utf-8", errors="replace")
        return {"id": server_id, "running": entry["process"].poll() is None,
                "exit_code": entry["process"].poll(), "output": "\n".join(text.splitlines()[-tail:])[-20000:]}

    def start(self, command, port, path=".", ready_path="/", timeout_seconds=30):
        if any(e["process"].poll() is None for e in self.servers.values()):
            raise ValueError("Stop the current server before starting another")
        if len(self.servers) >= 16:
            raise ValueError("Attempt server-start budget exhausted (16)")
        if not isinstance(command, list) or not command or len(command) > 64 or any(not isinstance(x, str) or not x or "\0" in x or len(x) > 4096 for x in command):
            raise ValueError("command must be a bounded, nonempty argv array; no shell wrapper is added")
        if isinstance(port, bool) or not isinstance(port, int) or not 1024 <= port <= 65535:
            raise ValueError("port must be between 1024 and 65535")
        if not isinstance(ready_path, str) or not ready_path.startswith("/") or ready_path.startswith("//") or ("\\" in ready_path or any(ord(c) < 32 for c in ready_path) or "#" in ready_path) or len(ready_path) > 2048:
            raise ValueError("ready_path must be a local HTTP path")
        if isinstance(timeout_seconds, bool) or not isinstance(timeout_seconds, int) or not 1 <= timeout_seconds <= 60:
            raise ValueError("timeout_seconds must be between 1 and 60")
        envs = self.environments
        if envs.native:
            raise ValueError("Managed servers currently require container mode; native process ownership on forced cancellation is not implemented")
        if not envs.native and not envs.network:
            raise PermissionError("Container servers currently require network.public: Docker network none cannot publish a port")
        # Avoid mistaking an already running service for this server's readiness.
        with socket.socket() as reservation:
            if os.name == "nt":
                reservation.setsockopt(socket.SOL_SOCKET, socket.SO_EXCLUSIVEADDRUSE, 1)
            reservation.bind(("127.0.0.1", port))
        argv, launch, container, _ = forge_utils.command_launch(command, envs.workspace,
            allow_native=envs.native, allow_network=envs.network, command_cwd=path,
            allow_workspace_write=envs.writable, container_args=["--publish", f"127.0.0.1:{port}:{port}"], **envs.options(path))
        launch["creationflags" if os.name == "nt" else "start_new_session"] = getattr(subprocess, "CREATE_NO_WINDOW", 0) if os.name == "nt" else True
        process = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, **launch)
        server_id = uuid.uuid4().hex
        entry = {"process": process, "container": container, "log": bytearray(), "lock": threading.Lock(), "port": port}
        self.servers[server_id] = entry
        def drain():
            try:
                while chunk := process.stdout.read1(4096):
                    with entry["lock"]:
                        entry["log"].extend(chunk)
                        del entry["log"][:-65536]
            finally:
                process.stdout.close()
        entry["reader"] = threading.Thread(target=drain, name="reticle-server-logs", daemon=True)
        entry["reader"].start()
        deadline = time.monotonic()+timeout_seconds
        try:
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError("Server exited before readiness: " + self.logs(server_id)["output"][-2000:])
                if container:
                    # A port response before our container is running must not
                    # be attributed to us if another process raced the bind.
                    try:
                        state = subprocess.run(["docker", "inspect", "--format", "{{.State.Running}}", container],
                            capture_output=True, timeout=min(2, max(0.01, deadline-time.monotonic())))
                    except subprocess.TimeoutExpired:
                        continue
                    if state.returncode or state.stdout.strip() != b"true":
                        time.sleep(0.05)
                        continue
                connection = http.client.HTTPConnection("127.0.0.1", port, timeout=min(0.5, max(0.01, deadline-time.monotonic())))
                try:
                    connection.request("GET", ready_path)
                    response = connection.getresponse()
                    if 200 <= response.status < 300 and process.poll() is None:
                        return {"id": server_id, "url": f"http://127.0.0.1:{port}", "ready_path": ready_path,
                                "ready_status": response.status, "mode": "native" if envs.native else "container"}
                except (OSError, http.client.HTTPException):
                    pass
                finally:
                    connection.close()
                time.sleep(0.05)
            raise TimeoutError("Server did not return HTTP 2xx before its readiness deadline: " + self.logs(server_id)["output"][-2000:])
        except BaseException:
            self.stop(server_id)
            raise

    def site(self, server_id):
        entry = self._entry(server_id)
        if entry["process"].poll() is not None:
            raise ValueError("Managed server is no longer running")
        return entry["port"]

    def stop(self, server_id):
        entry = self._entry(server_id)
        if entry["container"]:
            result = subprocess.run(["docker", "rm", "-f", entry["container"]], capture_output=True, timeout=15)
            if result.returncode and b"No such container" not in result.stderr:
                raise RuntimeError("Container cleanup failed; server ownership retained for retry")
        import browser_tools
        browser_tools._kill_tree(entry["process"])
        entry["reader"].join(timeout=2)
        return {"id": server_id, "stopped": entry["process"].poll() is not None}

    def close(self):
        errors = []
        for server_id in self.servers:
            try:
                self.stop(server_id)
            except Exception as error:
                errors.append(str(error))
        if errors:
            raise RuntimeError("Managed server cleanup failed: " + "; ".join(errors))
