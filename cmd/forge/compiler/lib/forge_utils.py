"""Shared bounded workspace tools. Native mode executes trusted code as the user."""
import difflib
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

SKILL_ID = re.compile(r"[a-zA-Z0-9_-]+")

def load_skill_instructions(skills_root, skill_ids, catalog=None):
    """Read skills/<id>/SKILL.md for each id.

    Returns {"text", "applied", "missing"}: the instructions to append to a worker's prompt, the
    [id, characters] pairs that were applied, and the ids with no instruction file. A skill can
    legitimately be dependencies only (llm-worker), so a missing file is reported, not raised;
    the worker shows both lists at startup so a degraded worker is visible.
    """
    text, applied, missing, provenance = "", [], [], []
    for skill in skill_ids:
        if not isinstance(skill, str) or not SKILL_ID.fullmatch(skill):
            raise ValueError("Invalid skill ID")
        registered = None
        if catalog is not None:
            registered = next((entry for entry in catalog.get("skills", []) if entry.get("id") == skill), None)
            if registered is None:
                raise ValueError(f"Skill is not registered: {skill}")
            if not registered.get("instructions_path"):
                if registered.get("instructions_bytes", 0):
                    raise ValueError(f"Registered skill has no instruction source: {skill}")
                continue  # Legacy dependency-only entry, explicitly present in the registry.
        source = Path(registered["instructions_path"]) if registered else Path(skills_root) / skill / "SKILL.md"
        if not source.is_file():
            if registered:
                raise ValueError(f"Registered skill instructions disappeared: {skill}")
            missing.append(skill)
            continue
        raw = source.read_bytes()
        if len(raw) > MAX_FILE:
            raise ValueError(f"Skill instructions exceed 1 MiB: {skill}")
        if registered and hashlib.sha256(raw).hexdigest() != registered.get("instructions_sha256"):
            raise ValueError(f"Registered skill instructions changed: {skill}; reload the registry")
        body = raw.decode("utf-8").replace("\r\n", "\n").replace("\r", "\n")
        text += "\n\n" + body
        applied.append([skill, len(body)])
        provenance.append({"id": skill, "version": registered.get("version") if registered else None,
                           "source": str(source), "sha256": hashlib.sha256(raw).hexdigest()})
    return {"text": text, "applied": applied, "missing": missing, "provenance": provenance}


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

def _expand_short_name(path):
    # CI runners expose TEMP as C:\Users\RUNNER~1; an 8.3 alias names the same
    # directory as its long form, unlike a symlink or junction.
    if os.name != "nt":
        return path
    import ctypes
    buffer = ctypes.create_unicode_buffer(32768)
    length = ctypes.windll.kernel32.GetLongPathNameW(str(path), buffer, len(buffer))
    return Path(buffer.value) if 0 < length < len(buffer) else path

def canonical_workspace_path(relative):
    """The path a tool means. A node works inside the session's src folder, but the architect
    names deliverables as src/index.html, so models write both src/main.js and index.html.
    Without this they landed in different places (src/src/main.js beside src/index.html) and the
    page could not find its own script. One leading src/ is the folder itself; bare "src" is
    the folder."""
    if not isinstance(relative, str):
        return relative
    parts = Path(relative).parts
    if parts and parts[0] == "src" and not Path(relative).is_absolute():
        return Path(*parts[1:]).as_posix() if len(parts) > 1 else "."
    return relative

def safe_path(workspace, relative, *, base="src", literal=False):
    """Resolve a workspace-relative path inside the workspace. For the default src base a
    leading src/ is stripped (see canonical_workspace_path); literal=True resolves the path
    exactly as given, for re-resolving a path that was read from the disk."""
    if not isinstance(relative, str) or "\\" in relative or ":" in relative:
        raise ValueError("A workspace-relative path is required")
    if base == "src" and not literal:
        relative = canonical_workspace_path(relative)
    rel = Path(relative)
    if any(part.endswith(".reticle-lock") for part in rel.parts):
        raise ValueError("Runtime edit lock paths are reserved")
    if rel.is_absolute() or ".." in rel.parts:
        raise ValueError("Path escapes workspace")
    workspace = Path(workspace).absolute()
    root = workspace / base if base else workspace
    root.mkdir(parents=True, exist_ok=True)
    root = _expand_short_name(root)
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

# What one tool call may return to the model, in characters. worker_sdk raises or lowers it from the
# model's context window when that is known. Reads page instead of cutting, so a long file is
# never silently shortened: the reply always says what part it shows and where the next starts.
TOOL_RESULT_CHARS = 20000
READ_NOTICE = "[reticle: "

def _positive_int(value, name):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or int(value) != value or int(value) < 1:
        raise ValueError(f"{name} must be a whole number of 1 or more")
    return int(value)

def _page_of(text, offset, limit):
    """The part of a text a read returns: all of it when it fits, otherwise whole lines from
    `offset` (1-based) for at most `limit` lines and the per-call character budget."""
    budget = max(2000, TOOL_RESULT_CHARS - 400)
    truncated_source = len(text) >= MAX_FILE
    if offset is None and limit is None and len(text) <= budget and not truncated_source:
        return text
    start = 1 if offset is None else _positive_int(offset, "offset")
    cap = None if limit is None else _positive_int(limit, "limit")
    lines = text.splitlines(keepends=True)
    total = len(lines)
    if start > max(total, 1):
        raise ValueError(f"offset {start} is past the end of the file ({total} lines)")
    shown, used, index, partial = [], 0, start - 1, False
    while index < total and (cap is None or len(shown) < cap):
        line = lines[index]
        if used + len(line) > budget:
            if not shown:
                shown.append(line[:budget])
                partial, index = True, index + 1
            break
        shown.append(line)
        used += len(line)
        index += 1
    last = start - 1 + len(shown)
    if start == 1 and last >= total and not partial and not truncated_source:
        return "".join(shown)
    notes = [f"lines {start}-{last} of {total}", f"{len(text)} characters"]
    if partial:
        notes.append(f"line {start} alone is longer than one reply, so only its first {budget} characters are shown; "
                     "use search_codebase to find what you need, or edit by exact text")
    if truncated_source:
        notes.append("the file is larger than 1 MiB and only its first 1 MiB can be read")
    more = f"Call read_file with offset={last + 1} for the next part." if last < total else "This is the end of the readable file."
    return f"{READ_NOTICE}{'; '.join(notes)}. {more}]\n" + "".join(shown)

def read_file(path, workspace_dir, offset=None, limit=None):
    try:
        target = safe_path(workspace_dir, path)
        if not target.exists() and isinstance(path, str) and path.startswith("src/"):
            # Sessions written before paths were canonicalised can hold src/src/...; read those too.
            literal = safe_path(workspace_dir, path, literal=True)
            if literal.exists():
                target = literal
        with target.open("r", encoding="utf-8") as stream:
            return _page_of(stream.read(MAX_FILE), offset, limit)
    except Exception as exc:
        return f"Error reading file: {exc}"

def text_digest(text):
    """The identity of a file's text, so an overwrite can prove it is replacing what was read."""
    return hashlib.sha256(text.encode("utf-8")).hexdigest()

def _acquire_edit_lock(target):
    """An OS-owned lock is released on process death. Keep the lock inode stable;
    unlinking it would allow two processes to lock different inodes for one file.
    Old empty lock files are safe to reuse after upgrading.
    """
    lock = target.with_name(target.name + ".reticle-lock")
    if lock.is_symlink():
        raise ValueError("Edit lock must not be a symlink")
    fd = os.open(lock, os.O_RDWR | os.O_CREAT | getattr(os, "O_NOFOLLOW", 0), 0o600)
    stream = os.fdopen(fd, "r+b")
    try:
        if os.name == "nt":
            import msvcrt
            if lock.stat().st_size == 0:
                stream.write(b"\0")
                stream.flush()
            stream.seek(0)
            msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
        else:
            import fcntl
            fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        return stream
    except BaseException:
        stream.close()
        raise


def _overwrite_file(target, path, content, expected_digest, files_modified):
    """Replace a whole existing file, but only the version the caller read. The swap is atomic
    (a half-written page is never left behind) and holds the same edit lock as replace_file_content."""
    if not expected_digest:
        return (f"Error: {path} exists and you have not read it in this task. Call read_file on it first, "
                "then write_file again with overwrite=true.")
    try:
        lock = _acquire_edit_lock(target)
    except OSError as exc:
        return f"Error acquiring edit lock: {exc}"
    temporary = None
    try:
        if text_digest(target.read_text(encoding="utf-8")) != expected_digest:
            return f"Error: {path} changed since you read it. Read it again, then overwrite it."
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", newline="", dir=target.parent, delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(content)
        if text_digest(target.read_text(encoding="utf-8")) != expected_digest:
            return f"Error: {path} changed during overwrite. Read it again before overwriting."
        os.replace(temporary, target)
        files_modified[path] = content
        return f"Successfully overwrote {path}"
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
        lock.close()

def write_file(path, content, workspace_dir, files_modified, overwrite=False, expected_digest=None):
    try:
        target = safe_path(workspace_dir, path)
        if not isinstance(content, str) or len(content.encode()) > MAX_FILE:
            raise ValueError("Text file exceeds 1 MiB or content is not text")
        target.parent.mkdir(parents=True, exist_ok=True)
        if overwrite is True and target.exists():
            return _overwrite_file(target, path, content, expected_digest, files_modified)
        # Exclusive creation prevents two workers silently overwriting each other.
        with target.open("x", encoding="utf-8", newline="") as stream:
            stream.write(content)
        files_modified[path] = content
        return f"Successfully wrote to {path}"
    except FileExistsError:
        return ("Error: File exists. If it already has the content you intend, read it with "
                 "read_file to verify it, then call mark_task_complete. To replace the whole file, read it "
                 "with read_file and call write_file again with overwrite=true. To change one part, use "
                 "replace_file_content with only that part.")
    except Exception as exc:
        return f"Error writing file: {exc}"

_DOCUMENT_ROOTS = {"<!doctype": "<!DOCTYPE> declaration", "<html": "<html> element", "</html>": "</html> tag"}

def _extra_document_root(path, old, new):
    """Name the HTML document marker an edit would duplicate, or "". A model that cannot overwrite a
    file tends to "replace" one small unique string with a whole new page; each such edit matches
    exactly once, succeeds, and nests another copy (one run ended with nine pages in one file)."""
    if not str(path).lower().endswith((".html", ".htm")):
        return ""
    before, after = old.lower(), new.lower()
    for marker, label in _DOCUMENT_ROOTS.items():
        if before.count(marker) >= 1 and after.count(marker) > before.count(marker):
            return label
    return ""

def _line_numbers(text, needle, limit=8):
    found, start = [], 0
    while len(found) < limit:
        index = text.find(needle, start)
        if index < 0:
            break
        found.append(text.count("\n", 0, index) + 1)
        start = index + max(1, len(needle))
    return found

def _no_match_hint(old, target):
    """Say what is in the file instead of repeating that there was no match: a model told only
    "no match" tends to send the same request again until it is stopped."""
    squash = lambda value: re.sub(r"\s+", " ", value).strip()
    first = next((line for line in target.splitlines() if line.strip()), "")
    if squash(target) and squash(target) in squash(old):
        for number, line in enumerate(old.splitlines(), 1):
            if squash(first) and squash(first) in squash(line):
                return (f"that text is in the file near line {number}, but its whitespace, indentation or line breaks "
                        "differ. Read that part again with read_file and copy it exactly.")
        return "that text is in the file, but its whitespace, indentation or line breaks differ. Copy it exactly from read_file."
    lines = old.splitlines()[:5000]
    stripped = [line.strip() for line in lines]
    if first.strip():
        close = difflib.get_close_matches(first.strip(), stripped, n=3, cutoff=0.6)
        shown = []
        for candidate in close:
            number = stripped.index(candidate) + 1
            shown.append(f"line {number}: {lines[number - 1].strip()[:160]}")
        if shown:
            return "no exact match. The closest lines in the file are " + "; ".join(shown) + ". Read the file and copy the exact text."
    return "that text is not in the file (it may have changed). Read the file again before editing."

def _edit_once(text, target_content, replacement_content, occurrence=None):
    """Apply one exact-text edit. Returns the new text; raises ValueError saying what to do next."""
    if not isinstance(target_content, str) or not target_content:
        raise ValueError("a nonempty exact target_content is required")
    if not isinstance(replacement_content, str):
        raise ValueError("replacement_content must be text")
    head = text.lstrip()[:160]
    if len(text) >= 400 and len(head) >= 60 and head in replacement_content and head not in target_content:
        raise ValueError(
            "replacement_content contains the start of the file again, so this edit would insert a second copy "
            "of it. Pass only the part that changes, or, to replace the entire file, read it with read_file and "
            "call write_file with overwrite=true.")
    count = text.count(target_content)
    if count == 0:
        raise ValueError(_no_match_hint(text, target_content))
    if occurrence is None:
        if count > 1:
            where = ", ".join(str(n) for n in _line_numbers(text, target_content))
            raise ValueError(
                f"that text matches {count} places (lines {where}). Include a few more surrounding lines in "
                f"target_content so it matches once, or pass occurrence (1 to {count}) to choose one.")
        position = text.find(target_content)
    else:
        number = _positive_int(occurrence, "occurrence")
        if number > count:
            raise ValueError(f"occurrence {number} was requested but the text matches only {count} time(s)")
        position, start = -1, 0
        for _ in range(number):
            position = text.find(target_content, start)
            start = position + len(target_content)
    return text[:position] + replacement_content + text[position + len(target_content):]

def _copies_the_file_start(old, new):
    """True when an edit adds a second copy of the file's own opening. Any file type: a model that
    cannot overwrite tends to insert the whole file again at one unique string."""
    head = old.lstrip()[:160]
    return len(old) >= 400 and len(head) >= 60 and new.count(head) > old.count(head)

def _rewrite_locked(path, workspace_dir, transform, label):
    """Read a file, let `transform` produce its new text (raising ValueError with advice), refuse
    edits that corrupt it, and swap the result in atomically under the edit lock."""
    lock = None
    try:
        target = safe_path(workspace_dir, path)
        lock = _acquire_edit_lock(target)
    except Exception as exc:
        return f"Error acquiring edit lock: {exc}"
    try:
        old = target.read_text(encoding="utf-8")
        if len(old.encode()) > MAX_FILE:
            raise ValueError("the file is larger than 1 MiB")
        new = transform(old)
        if len(new.encode()) > MAX_FILE:
            raise ValueError("the result would exceed 1 MiB")
        extra = _extra_document_root(str(path), old, new)
        if extra:
            raise ValueError(
                f"this edit would leave the file with a second {extra}: replacement_content is a whole page "
                "inserted into one. Pass only the part that changes, or, to replace the entire file, read "
                "it with read_file and call write_file with overwrite=true.")
        if _copies_the_file_start(old, new):
            raise ValueError(
                "this edit would insert a second copy of the start of the file. Pass only the part that "
                "changes, or, to replace the entire file, read it with read_file and call write_file with "
                "overwrite=true.")
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", newline="", dir=target.parent, delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(new)
        try:
            if target.read_text(encoding="utf-8") != old:
                raise ValueError("File changed; read it again before editing")
            os.replace(temporary, target)
        finally:
            temporary.unlink(missing_ok=True)
        return label
    except Exception as exc:
        return f"Error replacing content: {exc}"
    finally:
        lock.close()

def replace_file_content(path, target_content, replacement_content, workspace_dir, occurrence=None):
    return _rewrite_locked(
        path, workspace_dir,
        lambda old: _edit_once(old, target_content, replacement_content, occurrence),
        f"Successfully replaced content in {path}")

MAX_EDITS = 20

def apply_edits(path, edits, workspace_dir):
    """Several exact-text edits to one file, in order, all or nothing."""
    if not isinstance(edits, list) or not edits:
        return "Error replacing content: edits must be a non-empty list of {target_content, replacement_content}"
    if len(edits) > MAX_EDITS:
        return f"Error replacing content: at most {MAX_EDITS} edits per call"

    def transform(old):
        text = old
        for number, edit in enumerate(edits, 1):
            if not isinstance(edit, dict):
                raise ValueError(f"edit {number} must be an object with target_content and replacement_content")
            try:
                text = _edit_once(text, edit.get("target_content"), edit.get("replacement_content"), edit.get("occurrence"))
            except ValueError as exc:
                raise ValueError(f"edit {number} of {len(edits)} failed and nothing was changed: {exc}") from None
        return text
    return _rewrite_locked(path, workspace_dir, transform, f"Successfully applied {len(edits)} edit(s) to {path}")

def find_files(pattern, workspace_dir):
    """Workspace files whose path matches a glob such as **/*.js, src/*.html or just *.css."""
    try:
        if not isinstance(pattern, str) or not pattern.strip():
            raise ValueError("a glob pattern such as **/*.js is required")
        pattern = pattern.strip().replace("\\", "/")
        if pattern.startswith("/") or ".." in pattern.split("/") or ":" in pattern:
            raise ValueError("the pattern must be a relative glob inside the workspace")
        if "/" not in pattern:
            pattern = "**/" + pattern
        root = safe_path(workspace_dir, ".")
        found = []
        for candidate in root.glob(pattern):
            try:
                rel = str(candidate.relative_to(root)).replace("\\", "/")
                checked = safe_path(workspace_dir, rel, literal=True)
                if checked.is_file() and not checked.name.endswith(".reticle-lock"):
                    found.append(f"{rel} ({checked.stat().st_size} bytes)")
            except (OSError, ValueError):
                continue
            if len(found) >= 500:
                break
        return "\n".join(sorted(found)) or "No files match."
    except Exception as exc:
        return f"Error finding files: {exc}"

def move_path(source_path, destination_path, workspace_dir):
    try:
        root = safe_path(workspace_dir, ".")
        source = safe_path(workspace_dir, source_path)
        destination = safe_path(workspace_dir, destination_path)
        if source == root:
            raise ValueError("the workspace itself cannot be moved")
        if not source.exists():
            raise ValueError(f"{source_path} does not exist")
        if destination.exists():
            raise ValueError(f"{destination_path} already exists; choose another name or delete_path it first")
        if destination == source or source in destination.parents:
            raise ValueError("a path cannot be moved into itself")
        destination.parent.mkdir(parents=True, exist_ok=True)
        os.replace(source, destination)
        return f"Successfully moved {source_path} to {destination_path}"
    except Exception as exc:
        return f"Error moving path: {exc}"

def delete_path(path, workspace_dir, recursive=False):
    try:
        root = safe_path(workspace_dir, ".")
        target = safe_path(workspace_dir, path)
        if target == root:
            raise ValueError("the workspace itself cannot be deleted")
        if not target.exists():
            raise ValueError(f"{path} does not exist")
        if target.is_dir():
            if any(target.iterdir()):
                if recursive is not True:
                    raise ValueError(f"{path} is a directory that is not empty; pass recursive=true to delete it and everything in it")
                shutil.rmtree(target)
            else:
                target.rmdir()
        else:
            target.unlink()
        return f"Successfully deleted {path}"
    except Exception as exc:
        return f"Error deleting path: {exc}"

def list_dir(path, workspace_dir):
    try:
        return "\n".join(sorted(p.name for p in safe_path(workspace_dir, path).iterdir() if not p.name.endswith(".reticle-lock"))[:1000])
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
            checked = safe_path(workspace_dir, rel, literal=True)
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

def _ensure_worker_image(image):
    """Pull the container image once, before the command, so the pull's progress output is not handed
    to the model as the command's output and does not use up the command's time limit. Returns an
    error message, or "" when the image is ready."""
    try:
        if subprocess.run(["docker", "image", "inspect", image], capture_output=True, timeout=30).returncode == 0:
            return ""
        pulled = subprocess.run(["docker", "pull", "-q", image], capture_output=True, timeout=900)
        if pulled.returncode != 0:
            return "could not pull the worker image " + image + ": " + pulled.stderr.decode("utf-8", errors="replace").strip()[-300:]
        return ""
    except FileNotFoundError:
        return "Docker is not installed or not on PATH; commands need a container unless native execution is enabled"
    except subprocess.TimeoutExpired:
        return "Docker did not answer in time (is Docker Desktop running?)"
    except OSError as exc:
        return f"Docker could not be used: {exc}"

def execute_terminal_command(command, workspace_dir, allow_native=False, allow_network=False, command_cwd=".", allow_workspace_write=True):
    """Bounded validation command. Real training uses an explicit configured budget."""
    container = None
    try:
        root = safe_path(workspace_dir, ".")
        cwd = safe_path(workspace_dir, command_cwd)
        if not cwd.is_dir():
            raise ValueError("Command working directory does not exist")
        timeout = min(max(int(os.getenv("RETICLE_COMMAND_TIMEOUT", "120")), 1), 3600)
        gpu = os.getenv("RETICLE_GPU_DEVICES", "").strip()
        profile = os.getenv("RETICLE_ML_PROFILE", "cpu").strip().lower()
        host = detect_ml_host_environment()
        warnings = assess_ml_profile(profile, gpu, host)
        if allow_native:
            argv = command
            env = os.environ.copy()
            env["PATH"] = str(Path(sys.executable).parent) + os.pathsep + env.get("PATH", "")
            if isinstance(command, list):
                if not command or not all(isinstance(arg, str) and "\0" not in arg for arg in command):
                    raise ValueError("Command arguments must be nonempty strings")
                executable = shutil.which(command[0], path=env["PATH"])
                if executable is None:
                    raise ValueError(f"Executable is not available: {command[0]}")
                argv = [executable, *command[1:]]
            options = dict(shell=isinstance(command, str), cwd=cwd, env=env)
        else:
            if profile == "amd-rocm" and host == "windows":
                raise ValueError("Native Windows AMD execution requires the explicit native-execution setting; Linux ROCm device mappings cannot be applied to this Windows container path")
            # Each command owns its container; timeout/cancellation cleanup cannot
            # accidentally remove another run's shared container.
            import uuid
            container = "reticle-" + uuid.uuid4().hex
            image = os.getenv("RETICLE_WORKER_IMAGE", "ghcr.io/astral-sh/uv:python3.12-bookworm-slim")
            image_problem = _ensure_worker_image(image)
            if image_problem:
                return "Error executing command: " + image_problem
            argv = ["docker", "run", "--rm", "--name", container,
                    "--label", "reticle.execution="+os.getenv("RETICLE_EXECUTION_ID", "standalone"),
                    "--memory", os.getenv("RETICLE_MEMORY_LIMIT", "2g"), "--cpus", os.getenv("RETICLE_CPU_LIMIT", "2"),
                    "--pids-limit", "256", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
                    "--mount", f"type=bind,source={root},target=/workspace/src"+("" if allow_workspace_write else ",readonly"), "-w", "/workspace/src" if cwd == root else "/workspace/src/"+cwd.relative_to(root).as_posix(),
                    image, "sh", "-c", command if isinstance(command, str) else shlex.join(command)]
            if not allow_network:
                argv[2:2] = ["--network", "none"]
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
            out, err = stdout.read(10001), stderr.read(10001)
            output = (out[:10000] + b"\n" + err[:10000]).decode("utf-8", errors="replace")
            if len(out) > 10000 or len(err) > 10000:
                output += "\n[reticle: output truncated at 10000 bytes per stream]"
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
 "read_file": ("Read a workspace text file. A long file comes back in parts: the reply names the lines it shows and the offset of the next part. Pass offset (1-based line number) and limit (number of lines) to read a specific part.", {"path": "string", "offset": {"type": "integer", "optional": True}, "limit": {"type": "integer", "optional": True}}),
 "write_file": ("Create a new workspace text file. To replace the whole content of one that already exists, read it with read_file first and pass overwrite=true", {"path": "string", "content": "string", "overwrite": {"type": "boolean", "optional": True}}),
 "replace_file_content": ("Change one exact piece of an existing file. target_content must match the file exactly, so read the file first. Pass only the part that changes, never the whole file. If the text matches more than once, include more surrounding lines or pass occurrence. To rewrite a whole file, read it and use write_file with overwrite=true.", {"path": "string", "target_content": "string", "replacement_content": "string", "occurrence": {"type": "integer", "optional": True}}),
 "apply_edits": ("Make several exact-text edits to one existing file in one call, in order, all or nothing. Each edit is {target_content, replacement_content} and optionally occurrence. Prefer this to several replace_file_content calls.", {"path": "string", "edits": {"type": "array", "items": {"type": "object", "properties": {"target_content": {"type": "string"}, "replacement_content": {"type": "string"}, "occurrence": {"type": "integer"}}, "required": ["target_content", "replacement_content"], "additionalProperties": False}}}),
 "find_files": ("List workspace files whose path matches a glob such as **/*.js, assets/*.png or just *.css (a pattern without a slash matches in every folder). Returns paths with sizes.", {"pattern": "string"}),
 "move_path": ("Move or rename a file or folder inside the workspace. Fails if the destination exists.", {"source_path": "string", "destination_path": "string"}),
 "delete_path": ("Delete a file, or an empty folder, inside the workspace. A folder that is not empty is deleted only with recursive=true.", {"path": "string", "recursive": {"type": "boolean", "optional": True}}),
 "list_dir": ("List a workspace directory", {"path": "string"}),
 "search_codebase": ("Search workspace text literally", {"regex_pattern": "string"}),
 "read_url": ("Read public documentation", {"url": "string"}),
 "execute_terminal_command": ("Run a shell command in the workspace folder and read its exit code and output (the first 10,000 characters of each of stdout and stderr). Use it to run the project's tests or scripts, install packages, execute what you wrote and see whether it works, or inspect files. Each call is independent: no state, such as cd or exported variables, carries over.", {"command": "string"}),
 "mark_task_complete": ("Finish with a summary after verification", {"summary": "string"}),
}
tools = [{"type":"function","function":{"name":name,"description":description,"parameters":{"type":"object","properties":{key:{"type":kind} for key,kind in properties.items()},"required":list(properties),"additionalProperties":False}}} for name,(description,properties) in _definitions.items()]
