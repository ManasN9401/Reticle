"""Shared bounded workspace tools. Native mode executes trusted code as the user."""
import hashlib
import http.client
import ipaddress
import json
import os
import platform
from pathlib import Path
import re
import shlex
import socket
import ssl
import subprocess
import shutil
import sys
import tempfile
import urllib.parse
import urllib.request

MAX_FILE = 1024 * 1024

def detect_ml_host_environment():
    system = platform.system().lower()
    if system == "windows":
        return "windows"
    if system == "linux":
        try:
            release = Path("/proc/sys/kernel/osrelease").read_text(encoding="utf-8").lower()
        except OSError:
            release = platform.release().lower()
        if os.getenv("WSL_INTEROP") or "microsoft" in release:
            return "wsl2"
        return "linux"
    return "other"

def assess_ml_profile(profile, gpu, host):
    if profile not in ("cpu", "amd-rocm", "nvidia-cuda", "user"):
        raise ValueError("RETICLE_ML_PROFILE must be cpu, amd-rocm, nvidia-cuda, or user")
    if gpu and not re.fullmatch(r"[0-9]+(?:,[0-9]+)*", gpu):
        raise ValueError("RETICLE_GPU_DEVICES must list explicit numeric devices")
    if profile == "cpu" and gpu:
        raise ValueError("CPU profile cannot request RETICLE_GPU_DEVICES")
    if profile in ("amd-rocm", "nvidia-cuda") and not gpu:
        raise ValueError(f"{profile} profile requires explicit RETICLE_GPU_DEVICES")
    if profile == "amd-rocm" and host == "windows":
        return ["Native Windows AMD support is limited to PyTorch and AMD's current listed GPUs; current Windows documentation does not support training. Verify the RX 7800 XT against the current AMD matrix."]
    if profile == "amd-rocm" and host == "wsl2":
        return ["WSL2 detected: verify the exact Windows driver, WSL distribution, ROCm image and RX 7800 XT support before running an experiment."]
    return []

def safe_path(workspace, relative, *, base="src"):
    if not isinstance(relative, str) or "\\" in relative or ":" in relative:
        raise ValueError("A workspace-relative path is required")
    rel = Path(relative)
    if rel.is_absolute() or ".." in rel.parts:
        raise ValueError("Path escapes workspace")
    workspace = Path(workspace).absolute()
    root = workspace / base if base else workspace
    root.mkdir(parents=True, exist_ok=True)
    if root.resolve() != root:
        raise ValueError("Aliased workspace root denied")
    target = root / rel
    target.resolve().relative_to(root)
    for parent in (target, *target.parents):
        if parent == root:
            break
        if parent.is_symlink() or (hasattr(parent, "is_junction") and parent.is_junction()):
            raise ValueError("Symlink or junction path denied")
    return target

def read_file(path, workspace_dir):
    try:
        target = safe_path(workspace_dir, path)
        if not target.exists() and isinstance(path, str) and path.startswith("src/"):
            # safe_path already resolves every path under a workspace-internal
            # "src" root (base="src" above); a model that also includes a
            # "src/" prefix of its own — a reasonable but wrong assumption
            # about the layout — ends up doubled to ".../src/src/...". Retry
            # once with that redundant prefix stripped before giving up.
            stripped = safe_path(workspace_dir, path[len("src/"):])
            if stripped.exists():
                target = stripped
        with target.open("r", encoding="utf-8") as stream:
            return stream.read(MAX_FILE)
    except Exception as exc:
        return f"Error reading file: {exc}"

def write_file(path, content, workspace_dir, files_modified):
    try:
        target = safe_path(workspace_dir, path)
        if not isinstance(content, str) or len(content.encode()) > MAX_FILE:
            raise ValueError("Text file exceeds 1 MiB or content is not text")
        target.parent.mkdir(parents=True, exist_ok=True)
        # Exclusive creation prevents two workers silently overwriting each other.
        with target.open("x", encoding="utf-8", newline="") as stream:
            stream.write(content)
        files_modified[path] = content
        return f"Successfully wrote to {path}"
    except FileExistsError:
        return ("Error: File exists. If it already has the content you intend, read it with "
                 "read_file to verify it, then call mark_task_complete — do not write_file it "
                 "again. Only use replace_file_content if it actually needs a different change.")
    except Exception as exc:
        return f"Error writing file: {exc}"

def replace_file_content(path, target_content, replacement_content, workspace_dir):
    lock = None
    try:
        target = safe_path(workspace_dir, path)
        lock = target.with_name(target.name + ".reticle-lock")
        fd = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        os.close(fd)
    except Exception as exc:
        return f"Error acquiring edit lock: {exc}"
    try:
        if not target_content:
            raise ValueError("A nonempty exact match is required")
        old = target.read_text(encoding="utf-8")
        if len(old.encode()) > MAX_FILE or old.count(target_content) != 1:
            raise ValueError("Expected exactly one match in a file under 1 MiB")
        new = old.replace(target_content, replacement_content, 1)
        if len(new.encode()) > MAX_FILE:
            raise ValueError("Replacement exceeds 1 MiB")
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", newline="", dir=target.parent, delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(new)
        try:
            if target.read_text(encoding="utf-8") != old:
                raise ValueError("File changed; read it again before editing")
            os.replace(temporary, target)
        finally:
            temporary.unlink(missing_ok=True)
        return f"Successfully replaced content in {path}"
    except Exception as exc:
        return f"Error replacing content: {exc}"
    finally:
        lock.unlink(missing_ok=True)

def list_dir(path, workspace_dir):
    try:
        return "\n".join(sorted(p.name for p in safe_path(workspace_dir, path).iterdir())[:1000])
    except Exception as exc:
        return f"Error listing directory: {exc}"

def search_codebase(regex_pattern, workspace_dir):
    # Literal search deliberately avoids unbounded user-supplied regex execution.
    matches = []
    root = safe_path(workspace_dir, ".")
    for target in root.rglob("*"):
        if len(matches) >= 500:
            break
        try:
            rel = str(target.relative_to(root)).replace("\\", "/")
            checked = safe_path(workspace_dir, rel)
            if checked.is_file() and checked.stat().st_size <= MAX_FILE:
                for number, line in enumerate(checked.read_text(encoding="utf-8").splitlines(), 1):
                    if regex_pattern in line:
                        matches.append(f"{rel}:{number}: {line[:500]}")
                        if len(matches) >= 500:
                            break
        except (OSError, ValueError, UnicodeError):
            continue
    return "\n".join(matches) or "No matches found."

def _public_url(url):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme not in ("https", "http") or not parsed.hostname or parsed.username or parsed.password:
        raise ValueError("Only public HTTP(S) URLs are supported")
    addresses = socket.getaddrinfo(parsed.hostname, parsed.port or (443 if parsed.scheme == "https" else 80))
    if any(not ipaddress.ip_address(address[4][0]).is_global for address in addresses):
        raise ValueError("Private/local network destinations are not authorized")
    return url

class _PublicRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return super().redirect_request(req, fp, code, msg, headers, _public_url(newurl))

def read_url(url, workspace_dir):
    try:
        for _ in range(6):
            parsed = urllib.parse.urlsplit(url)
            if parsed.scheme not in ("https", "http") or not parsed.hostname or parsed.username or parsed.password:
                raise ValueError("Only public HTTP(S) URLs are supported")
            port = parsed.port or (443 if parsed.scheme == "https" else 80)
            addresses = socket.getaddrinfo(parsed.hostname, port, type=socket.SOCK_STREAM)
            if not addresses or any(not ipaddress.ip_address(a[4][0]).is_global for a in addresses):
                raise ValueError("Private/local network destinations are not authorized")
            # Connect to the address that was checked; do not resolve DNS again.
            address = addresses[0][4]
            connection = http.client.HTTPConnection(parsed.hostname, port, timeout=15)
            def connect():
                connection.sock = socket.create_connection(address[:2], timeout=15)
                if parsed.scheme == "https":
                    connection.sock = ssl.create_default_context().wrap_socket(connection.sock, server_hostname=parsed.hostname)
            connection.connect = connect
            try:
                target = urllib.parse.urlunsplit(("", "", parsed.path or "/", parsed.query, ""))
                connection.request("GET", target, headers={"User-Agent":"Reticle-docs/1"})
                response = connection.getresponse()
                if response.status in (301,302,303,307,308):
                    location=response.getheader("Location")
                    if not location: raise ValueError("Redirect has no destination")
                    url=urllib.parse.urljoin(url,location)
                    continue
                if response.status >= 400: raise ValueError(f"HTTP {response.status}")
                body=response.read(MAX_FILE).decode("utf-8",errors="replace")
                break
            finally:
                connection.close()
        else:
            raise ValueError("Too many redirects")
        return re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", body))[:20000]
    except Exception as exc:
        return f"Error reading URL: {exc}"

def execute_terminal_command(command, workspace_dir, allow_native=False):
    """Bounded validation command. Real training uses an explicit configured budget."""
    container = None
    try:
        root = safe_path(workspace_dir, ".")
        timeout = min(max(int(os.getenv("RETICLE_COMMAND_TIMEOUT", "120")), 1), 3600)
        gpu = os.getenv("RETICLE_GPU_DEVICES", "").strip()
        profile = os.getenv("RETICLE_ML_PROFILE", "cpu").strip().lower()
        host = detect_ml_host_environment()
        warnings = assess_ml_profile(profile, gpu, host)
        if allow_native:
            argv = command
            env = os.environ.copy()
            env["PATH"] = str(Path(sys.executable).parent) + os.pathsep + env.get("PATH", "")
            options = dict(shell=True, cwd=root, env=env)
        else:
            if profile == "amd-rocm" and host == "windows":
                raise ValueError("Native Windows AMD execution requires the explicit native-execution setting; Linux ROCm device mappings cannot be applied to this Windows container path")
            # Each command owns its container; timeout/cancellation cleanup cannot
            # accidentally remove another run's shared container.
            import uuid
            container = "reticle-" + uuid.uuid4().hex
            image = os.getenv("RETICLE_WORKER_IMAGE", "ghcr.io/astral-sh/uv:python3.12-bookworm-slim")
            argv = ["docker", "run", "--rm", "--name", container,
                    "--label", "reticle.execution="+os.getenv("RETICLE_EXECUTION_ID", "standalone"),
                    "--memory", os.getenv("RETICLE_MEMORY_LIMIT", "2g"), "--cpus", os.getenv("RETICLE_CPU_LIMIT", "2"),
                    "--pids-limit", "256", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
                    "--mount", f"type=bind,source={root},target=/workspace/src", "-w", "/workspace/src",
                    image, "sh", "-c", command]
            if profile == "amd-rocm":
                argv[2:2] = ["--device=/dev/kfd", "--device=/dev/dri", "--group-add=video"]
            if gpu:
                if profile == "nvidia-cuda":
                    argv[2:2] = ["--gpus", "device=" + gpu]
            options = {}
        # Temporary output files bound resident memory even for verbose children.
        with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
            result = subprocess.run(argv, stdout=stdout, stderr=stderr, timeout=timeout, **options)
            stdout.seek(0); stderr.seek(0)
            output = (stdout.read(10000) + b"\n" + stderr.read(10000)).decode("utf-8", errors="replace")
        warning_text = "".join(f"ML environment warning: {warning}\n" for warning in warnings)
        return f"Exit code: {result.returncode}\n{warning_text}{output}"
    except Exception as exc:
        return f"Error executing command: {exc}"
    finally:
        if container:
            try:
                subprocess.run(["docker", "rm", "-f", container], capture_output=True, timeout=15)
            except (OSError, subprocess.TimeoutExpired) as exc:
                print(f"Container cleanup failed: {exc}", file=sys.stderr)

_definitions = {
 "read_file": ("Read a workspace text file", {"path": "string"}),
 "write_file": ("Create a new workspace text file", {"path": "string", "content": "string"}),
 "replace_file_content": ("Replace one exact occurrence after reading the file", {"path": "string", "target_content": "string", "replacement_content": "string"}),
 "list_dir": ("List a workspace directory", {"path": "string"}),
 "search_codebase": ("Search workspace text literally", {"regex_pattern": "string"}),
 "read_url": ("Read public documentation", {"url": "string"}),
 "execute_terminal_command": ("Run a bounded validation command in src", {"command": "string"}),
 "mark_task_complete": ("Finish with a summary after verification", {"summary": "string"}),
}
tools = [{"type":"function","function":{"name":name,"description":description,"parameters":{"type":"object","properties":{key:{"type":kind} for key,kind in properties.items()},"required":list(properties),"additionalProperties":False}}} for name,(description,properties) in _definitions.items()]
