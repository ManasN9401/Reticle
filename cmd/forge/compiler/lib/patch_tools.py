"""Bounded unified patches with prevalidation, edit locks and recoverable rollback."""
import base64
from contextlib import ExitStack
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tempfile
import time
import uuid

import forge_utils as files

MAX_FILES = 32
MAX_PATCH = 2 * 1024 * 1024
MAX_TOTAL = 8 * 1024 * 1024
MAX_CONTEXT_SCAN = 2_000_000
MAX_HUNKS = 256


def _header(value, prefix):
    value = value[len(prefix):]
    if value == "/dev/null":
        return None
    if value.startswith(("a/", "b/")):
        value = value[2:]
    if not value or any(c in value for c in "\t\r\n\0"):
        raise ValueError("Patch file headers require plain workspace paths without timestamps")
    normalized=Path(files.canonical_workspace_path(value))
    reserved={"con","prn","aux","nul",*("com"+str(n) for n in range(1,10)),*("lpt"+str(n) for n in range(1,10))}
    if any(part.endswith(("."," ")) or part.split(".")[0].casefold() in reserved for part in normalized.parts):
        raise ValueError("Patch paths must be portable names without Windows device or trailing-dot/space aliases")
    return normalized.as_posix()


def parse_patch(patch):
    if not isinstance(patch, str) or len(patch.encode("utf-8")) > MAX_PATCH or "\0" in patch:
        raise ValueError("Patch must be UTF-8 text within 2 MiB")
    lines = patch.splitlines(keepends=True)
    result, index, hunk_count = [], 0, 0
    while index < len(lines):
        if lines[index].startswith("diff --git ") or lines[index].startswith("index ") or not lines[index].strip():
            index += 1
            continue
        if not lines[index].startswith("--- ") or index+1 >= len(lines) or not lines[index+1].startswith("+++ "):
            raise ValueError("Expected unified-diff ---/+++ file headers")
        old = _header(lines[index].rstrip("\r\n"), "--- ")
        new = _header(lines[index+1].rstrip("\r\n"), "+++ ")
        if (old is None and new is None) or (old and new and old != new):
            raise ValueError("Patch renames are unsupported; use an explicit delete and add")
        path = new or old
        if any(part.lower() in {".git", ".reticle"} for part in Path(path).parts):
            raise ValueError("Repository/runtime metadata is not a patch target")
        index += 2
        hunks = []
        while index < len(lines) and lines[index].startswith("@@ "):
            match = re.match(r"^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@", lines[index])
            if not match:
                raise ValueError("Malformed unified-diff hunk header")
            start, count, next_start, next_count = (int(match[1]), int(match[2] or 1), int(match[3]), int(match[4] or 1))
            if (count and not start) or (next_count and not next_start):
                raise ValueError("Nonempty hunk ranges are one-based")
            index += 1
            edits, consumed, produced = [], 0, 0
            while index < len(lines):
                line = lines[index]
                if line.startswith("\\ No newline at end of file"):
                    if not edits:
                        raise ValueError("Misplaced missing-newline marker")
                    edits[-1] = (edits[-1][0], edits[-1][1].rstrip("\r\n"))
                    index += 1
                    continue
                if consumed == count and produced == next_count:
                    break
                if not line or line[0] not in " +-":
                    raise ValueError("Hunk body must use context, addition or removal prefixes")
                tag, text = line[0], line[1:].replace("\r\n", "\n")
                consumed += tag != "+"
                produced += tag != "-"
                if consumed > count or produced > next_count:
                    raise ValueError("Hunk body exceeds declared line counts")
                edits.append((tag, text))
                index += 1
            if consumed != count or produced != next_count:
                raise ValueError("Incomplete hunk body")
            if not any(tag in "+-" for tag,_ in edits):
                raise ValueError("Each hunk must add or remove lines")
            hunk_count += 1
            if hunk_count > MAX_HUNKS:
                raise ValueError("Patch exceeds 256 hunks")
            hunks.append((start, count, edits))
        if not hunks:
            raise ValueError("Each patch file needs at least one hunk; empty-file/mode-only patches are unsupported")
        if path.casefold() in {entry["path"].casefold() for entry in result}:
            raise ValueError("Duplicate or case-aliased patch target")
        result.append({"path": path, "create": old is None, "delete": new is None, "hunks": hunks})
        if len(result) > MAX_FILES:
            raise ValueError("Patch exceeds 32 files")
    if not result:
        raise ValueError("No file changes in patch")
    paths = [entry["path"].casefold().split("/") for entry in result]
    if any(a != b and len(a) < len(b) and b[:len(a)] == a for a in paths for b in paths):
        raise ValueError("Patch file targets cannot be ancestors of other file targets")
    return result


def _target(workspace,path):
    target=files.safe_path(workspace,path,literal=True)
    root=files.safe_path(workspace,".")
    actual=target.resolve().relative_to(root.resolve())
    if any(part.casefold() in {".git",".reticle"} for part in actual.parts):
        raise ValueError("Repository/runtime metadata is not a patch target")
    return target


def _read(target):
    if not target.exists():
        return None
    if not target.is_file():
        raise ValueError("Patch target must be a regular file")
    with target.open("rb") as stream:
        raw = stream.read(files.MAX_FILE+1)
    if len(raw) > files.MAX_FILE:
        raise ValueError("Patch file exceeds 1 MiB")
    return raw


def _matches(source, needle, start):
    # Linear-time line matching; stop once uniqueness is disproved.
    prefix=[0]*len(needle)
    at=0
    for index in range(1,len(needle)):
        while at and needle[index]!=needle[at]: at=prefix[at-1]
        if needle[index]==needle[at]: at+=1
        prefix[index]=at
    result=[];at=0
    for index in range(start,len(source)):
        while at and source[index]!=needle[at]: at=prefix[at-1]
        if source[index]==needle[at]: at+=1
        if at==len(needle):
            result.append(index-len(needle)+1)
            if len(result)==2: return result
            at=prefix[at-1]
    return result


def _transform(raw, change):
    if change["create"] and raw is not None:
        raise ValueError("New patch target already exists: " + change["path"])
    if not change["create"] and raw is None:
        raise ValueError("Patch target is missing: " + change["path"])
    text = (raw or b"").decode("utf-8")
    newline = "\r\n" if "\r\n" in text and "\n" not in text.replace("\r\n", "") else "\n"
    source = text.replace("\r\n", "\n").splitlines(keepends=True)
    cursor, output, scan_work = 0, [], 0
    for start, count, edits in change["hunks"]:
        expected = [line for tag, line in edits if tag != "+"]
        at = start-1 if count else start
        if not expected:
            if at < cursor or at > len(source):
                raise ValueError("Insertion range is outside the file")
        else:
            # Exact declared position wins; otherwise require a unique exact or
            # whitespace-only match. Never choose an arbitrary similar block.
            if at < cursor or source[at:at+len(expected)] != expected:
                scan_work += len(source)+len(expected)
                if scan_work > MAX_CONTEXT_SCAN:
                    raise ValueError("Patch context scan budget exceeded; use accurate hunk line positions")
                candidates = _matches(source,expected,cursor)
                if not candidates:
                    squash = lambda lines: [re.sub(r"[ \t]+", " ", line).strip() for line in lines]
                    needle = squash(expected)
                    candidates = _matches(squash(source),needle,cursor)
                if len(candidates) != 1:
                    hint = files._no_match_hint("".join(line[:200] for line in source[:5000]), "".join(expected[:3])[:200])
                    raise ValueError("Patch context is missing or ambiguous in "+change["path"]+": "+hint)
                at = candidates[0]
        output.extend(source[cursor:at])
        consumed = at
        for tag, line in edits:
            if tag == "+":
                output.append(line)
            elif tag == " ":
                output.append(source[consumed])
                consumed += 1
            else:
                consumed += 1
        cursor = consumed
    output.extend(source[cursor:])
    result = "".join(output).replace("\n", newline).encode("utf-8")
    if change["delete"]:
        if result:
            raise ValueError("A delete patch must remove the entire file")
        return None
    if len(result) > files.MAX_FILE:
        raise ValueError("Patched file exceeds 1 MiB")
    duplicate = files._extra_document_root(change["path"], text, result.decode("utf-8"))
    if duplicate:
        raise ValueError("Patch would duplicate HTML document root: " + duplicate)
    return result


def _digest(raw):
    return hashlib.sha256(raw).hexdigest() if raw is not None else None


def _write_json(path, record):
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, delete=False) as stream:
            temporary = Path(stream.name)
            json.dump(record, stream, ensure_ascii=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def _replace(target, raw, mode=0o644, *, expected):
    if _digest(_read(target)) != expected:
        raise ValueError("File changed before replacement: "+str(target.name))
    if raw is None:
        target.unlink(missing_ok=True)
        return
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=target.parent, delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, mode)
        if _digest(_read(target)) != expected:
            raise ValueError("File changed during patch replacement: "+str(target.name))
        os.replace(temporary, target)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def _directory(workspace):
    directory = files.safe_path(workspace, "patches", base="")
    directory.mkdir(exist_ok=True)
    return directory


def _load_record(workspace, path):
    # Recovery never trusts a journal to name arbitrary filesystem locations.
    with path.open("rb") as stream:
        raw = stream.read(24*1024*1024+1)
    if len(raw) > 24*1024*1024:
        raise ValueError("Patch journal exceeds its recovery budget")
    record = json.loads(raw)
    entries = record.get("files")
    if record.get("version") != 1 or not isinstance(entries, list) or not 1 <= len(entries) <= MAX_FILES:
        raise ValueError("Invalid patch journal")
    seen, total = set(), 0
    for entry in entries:
        path_name = entry["path"]
        target = _target(workspace,path_name)
        canonical = target.relative_to(files.safe_path(workspace, ".")).as_posix()
        if canonical != path_name or canonical.casefold() in seen or any(p.lower() in {".git", ".reticle"} for p in Path(canonical).parts):
            raise ValueError("Invalid or duplicate patch recovery path")
        seen.add(canonical.casefold())
        for kind in ("before", "after"):
            value = entry[kind]
            decoded = base64.b64decode(value, validate=True) if value is not None else None
            if decoded is not None and len(decoded) > files.MAX_FILE:
                raise ValueError("Patch recovery file exceeds 1 MiB")
            total += len(decoded or b"")
            if _digest(decoded) != entry[kind+"_sha256"]:
                raise ValueError("Patch journal digest mismatch")
        if isinstance(entry["mode"],bool) or not isinstance(entry["mode"], int) or entry["mode"] & ~0o777:
            raise ValueError("Invalid patch file mode")
    if total > MAX_TOTAL:
        raise ValueError("Patch recovery exceeds 8 MiB")
    return record


def _rollback(workspace, journal, record):
    with ExitStack() as locks:
        targets = []
        for entry in record["files"]:
            target = _target(workspace,entry["path"])
            target.parent.mkdir(parents=True, exist_ok=True)
            locks.enter_context(files._acquire_edit_lock(target))
            current = _read(target)
            if _digest(current) not in (entry["before_sha256"], entry["after_sha256"]):
                record["state"] = "conflict"
                _write_json(journal, record)
                raise ValueError("Interrupted patch conflicts with a later edit: " + entry["path"])
            targets.append((entry, target, current))
        for entry, target, current in targets:
            before = base64.b64decode(entry["before"]) if entry["before"] is not None else None
            if current != before:
                _replace(target, before, entry["mode"], expected=_digest(current))
        record["state"] = "rolled_back"
        _write_json(journal, record)


def _archive(workspace, journal):
    directory = files.safe_path(workspace, "patches/completed", base="")
    directory.mkdir(exist_ok=True)
    destination = directory/journal.name
    os.replace(journal, destination)
    return destination


def pending(workspace):
    directory = files.safe_path(workspace, "patches", base="")
    if not directory.exists():
        return []
    return sorted(directory.glob("*.json"))


def _recover_locked(workspace, directory):
    recovered = []
    journals = sorted(directory.glob("*.json"))
    if len(journals) > 16:
        raise ValueError("Too many pending patch journals; operator recovery is required")
    for journal in journals:
        checked = files.safe_path(workspace, "patches/"+journal.name, base="")
        record = _load_record(workspace, checked)
        if record.get("state") in {"prepared", "committing", "conflict"}:
            _rollback(workspace, checked, record)
            recovered.append(journal.stem)
            _archive(workspace, checked)
        elif record.get("state") not in {"committed", "rolled_back"}:
            raise ValueError("Unknown patch journal state")
        else:
            _archive(workspace, checked)
    return recovered


def recover(workspace):
    directory = _directory(workspace)
    with files._acquire_edit_lock(directory/"transaction"):
        return _recover_locked(workspace, directory)


def apply_patch(patch, workspace_dir, *, task_id="", attempt_id=""):
    changes = parse_patch(patch)
    # Resolve all targets before making directories or taking any per-file locks.
    for change in changes:
        _target(workspace_dir,change["path"])
    directory = _directory(workspace_dir)
    with files._acquire_edit_lock(directory/"transaction"):
        _recover_locked(workspace_dir, directory)
        with ExitStack() as locks:
            entries, total = [], 0
            for change in sorted(changes, key=lambda c:c["path"]):
                target = _target(workspace_dir,change["path"])
                target.parent.mkdir(parents=True, exist_ok=True)
                locks.enter_context(files._acquire_edit_lock(target))
                before = _read(target)
                after = _transform(before, change)
                total += len(before or b"")+len(after or b"")
                if total > MAX_TOTAL:
                    raise ValueError("Patch before/after images exceed 8 MiB")
                entries.append({"path": change["path"], "before": base64.b64encode(before).decode() if before is not None else None,
                    "after": base64.b64encode(after).decode() if after is not None else None,
                    "before_sha256": _digest(before), "after_sha256": _digest(after),
                    "mode": stat.S_IMODE(target.stat().st_mode) & 0o777 if before is not None else 0o644})
            journal = directory/(uuid.uuid4().hex+".json")
            record = {"version":1, "operation":"apply_patch", "created_at":time.time(), "state":"prepared", "task_id":task_id, "attempt_id":attempt_id, "files":entries}
            _write_json(journal, record)
            try:
                record["state"] = "committing"
                _write_json(journal, record)
                # Recheck the complete read-set before the first replacement.
                for entry in entries:
                    if _digest(_read(_target(workspace_dir,entry["path"]))) != entry["before_sha256"]:
                        raise ValueError("File changed during patch preparation: "+entry["path"])
                for entry in entries:
                    target = _target(workspace_dir,entry["path"])
                    if _digest(_read(target)) != entry["before_sha256"]:
                        raise ValueError("File changed during patch commit: "+entry["path"])
                    _replace(target, base64.b64decode(entry["after"]) if entry["after"] is not None else None, entry["mode"], expected=entry["before_sha256"])
                record["state"] = "committed"
                _write_json(journal, record)
            except BaseException:
                # Existing locks must be released before the recovery routine acquires them.
                locks.close()
                _rollback(workspace_dir, journal, record)
                _archive(workspace_dir, journal)
                raise
            journal = _archive(workspace_dir, journal)
    return {"ok":True, "receipt":"patches/completed/"+journal.name,
            "files":[{"path":entry["path"], "deleted":entry["after"] is None,
                      "before_sha256":entry["before_sha256"], "after_sha256":entry["after_sha256"]} for entry in entries]}
