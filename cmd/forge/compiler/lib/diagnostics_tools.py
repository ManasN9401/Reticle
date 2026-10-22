"""Detected diagnostics with artifact-bound receipts; no model-supplied shell."""
import ast
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile
import time
import tomllib
import uuid

import forge_utils

KINDS = ("test", "typecheck", "lint", "build")
MAX_TRACKED_FILES = 500
MAX_HASH_BYTES = 16 * 1024 * 1024


def _small_text(path):
    with path.open("rb") as stream:
        raw = stream.read(forge_utils.MAX_FILE+1)
    if len(raw) > forge_utils.MAX_FILE:
        raise ValueError(f"Diagnostic configuration exceeds 1 MiB: {path.name}")
    return raw.decode("utf-8")


def _javascript_manager(config, workspace_dir, relative):
    declared = config.get("packageManager")
    if declared is not None:
        if not isinstance(declared, str):
            raise ValueError("packageManager must be a string")
        manager = declared.split("@", 1)[0]
        if manager not in {"npm", "pnpm", "yarn", "bun"}:
            raise ValueError(f"Unsupported package manager: {manager}")
        return manager
    locks = {manager for name, manager in (("package-lock.json", "npm"), ("pnpm-lock.yaml", "pnpm"),
                                           ("yarn.lock", "yarn"), ("bun.lock", "bun"), ("bun.lockb", "bun"))
             if forge_utils.safe_path(workspace_dir, (Path(relative)/name).as_posix()).is_file()}
    if len(locks) > 1:
        raise ValueError("Multiple package-manager lockfiles: declare packageManager in package.json")
    return next(iter(locks), "npm")


def detect_diagnostics(workspace_dir, kind="auto", path="."):
    if kind not in ("auto", *KINDS):
        raise ValueError("kind must be auto, test, typecheck, lint or build")
    project = forge_utils.safe_path(workspace_dir, path)
    root = forge_utils.safe_path(workspace_dir, ".")
    if not project.is_dir():
        raise ValueError("Diagnostic path must be a project directory")
    relative = project.relative_to(root).as_posix()
    selected = KINDS if kind == "auto" else (kind,)
    package = forge_utils.safe_path(workspace_dir, (Path(relative)/"package.json").as_posix())
    if package.is_file():
        config = json.loads(_small_text(package))
        manager = _javascript_manager(config, workspace_dir, relative)
        scripts = config.get("scripts", {})
        if not isinstance(scripts, dict):
            raise ValueError("package.json scripts must be an object")
        plans = []
        for check in selected:
            script = next((s for s in (("typecheck", "check") if check == "typecheck" else (check,))
                           if isinstance(scripts.get(s), str) and scripts[s].strip()), None)
            if script:
                argv = ["npm", "--prefix", relative, "run", script] if manager == "npm" else [manager, "run", script]
                plans.append({"tool": manager, "kind": check, "argv": argv,
                              "cwd": "." if manager == "npm" else relative})
        return plans, f"No matching {manager} diagnostic script" if not plans else ""
    go_mod = forge_utils.safe_path(workspace_dir, (Path(relative)/"go.mod").as_posix())
    if go_mod.is_file():
        return [{"tool": "go", "kind": check, "argv": ["go", "-C", relative,
                 {"test": "test", "lint": "vet", "build": "build"}[check], "./..."]}
                for check in selected if check in {"test", "lint", "build"}], ""
    tests = forge_utils.safe_path(workspace_dir, (Path(relative)/"tests").as_posix())
    python_config = any(forge_utils.safe_path(workspace_dir, (Path(relative)/name).as_posix()).is_file()
                        for name in ("pyproject.toml", "pytest.ini", "setup.cfg", "requirements.txt",
                                     "ruff.toml", ".ruff.toml", "mypy.ini", ".mypy.ini", "pyrightconfig.json"))
    if tests.is_dir() or python_config:
        plans = []
        pyproject = forge_utils.safe_path(workspace_dir, (Path(relative)/"pyproject.toml").as_posix())
        config = tomllib.loads(_small_text(pyproject)) if pyproject.is_file() else {}
        tools = config.get("tool", {})
        def configured(*names):
            return any(forge_utils.safe_path(workspace_dir, (Path(relative)/name).as_posix()).is_file() for name in names)
        for check in selected:
            if check == "test":
                # A unittest project can run in the base Python image without
                # guessing that pytest and its plugins have been installed.
                uses_unittest = False
                if tests.is_dir():
                    for test in sorted(tests.glob("test*.py"))[:100]:
                        checked = forge_utils.safe_path(workspace_dir, test.relative_to(root).as_posix(), literal=True)
                        try:
                            tree = ast.parse(_small_text(checked))
                        except SyntaxError:
                            continue
                        if any((isinstance(n, ast.Import) and any(a.name == "unittest" for a in n.names)) or
                               (isinstance(n, ast.ImportFrom) and n.module == "unittest") for n in ast.walk(tree)):
                            uses_unittest = True
                if uses_unittest:
                    argv = ["python", "-B", "-m", "unittest", "discover", "-s", tests.relative_to(root).as_posix()]
                    tool = "unittest"
                else:
                    argv = ["python", "-B", "-m", "pytest", relative]
                    tool = "pytest"
                plans.append({"tool": tool, "kind": check, "argv": argv})
            elif check == "lint" and ("ruff" in tools or configured("ruff.toml", ".ruff.toml")):
                plans.append({"tool": "ruff", "kind": check, "argv": ["python", "-B", "-m", "ruff", "check", relative]})
            elif check == "typecheck":
                if "mypy" in tools or configured("mypy.ini", ".mypy.ini"):
                    plans.append({"tool": "mypy", "kind": check, "argv": ["python", "-B", "-m", "mypy", "."], "cwd": relative})
                elif "pyright" in tools or configured("pyrightconfig.json"):
                    plans.append({"tool": "pyright", "kind": check, "argv": ["python", "-B", "-m", "pyright", "."], "cwd": relative})
            elif check == "build" and "build-system" in config:
                plans.append({"tool": "build", "kind": check, "argv": ["python", "-B", "-m", "build", "--no-isolation", relative]})
        return plans, "No supported Python diagnostic for this kind" if not plans else ""
    return [], "Nothing to run: no supported project configuration or tests directory"


def parse_diagnostic_output(text, kind):
    """Conservative common-location parser; unknown output remains in the receipt."""
    errors = []
    for line in text.splitlines():
        match = re.match(r"^(.+?):(\d+)(?::(\d+))?:\s*(.+)$", line.lstrip())
        if not match:
            match = re.match(r"^(.+?)\((\d+),(\d+)\):\s*(.+)$", line.lstrip())
        if match:
            errors.append({"file": match[1], "line": int(match[2]),
                           "column": int(match[3] or 0), "message": match[4][:1000]})
        else:
            match = re.search(r'File "([^"]+)", line (\d+)', line)
            if match:
                errors.append({"file": match[1], "line": int(match[2]), "column": 0, "message": line.strip()[:1000]})
    counts = []
    for pattern in (r"Ran\s+(\d+)\s+tests?", r"\b(\d+)\s+passed\b", r"\bTests:\s+(\d+)\s+passed",
                    r"\bTests\s+(\d+)\s+passed"):
        counts.extend(int(n) for n in re.findall(pattern, text, re.IGNORECASE))
    # Go's successful package summary is evidence of executed tests only when
    # it is not the explicit '[no test files]' form.
    go_tests = bool(re.search(r"^ok\s+\S+\s+", text, re.MULTILINE))
    collected = max(counts) if counts else None
    return {"errors": errors[:100], "test_count": collected,
            "tests_observed": bool(collected and collected > 0) or go_tests if kind == "test" else None}


def _snapshot(workspace, paths):
    if len(paths) > MAX_TRACKED_FILES:
        raise ValueError("Too many artifacts for one diagnostic receipt")
    hashes = {}
    total = 0
    for path in sorted(set(paths)):
        target = forge_utils.safe_path(workspace, path)
        if not target.is_file() or target.stat().st_size > MAX_HASH_BYTES:
            raise ValueError(f"Diagnostic artifact is missing, non-file or exceeds 16 MiB: {path}")
        with target.open("rb") as stream:
            raw = stream.read(MAX_HASH_BYTES+1)
        if len(raw) > MAX_HASH_BYTES:
            raise ValueError(f"Diagnostic artifact exceeds 16 MiB: {path}")
        total += len(raw)
        if total > 64 * 1024 * 1024:
            raise ValueError("Diagnostic artifacts exceed the 64 MiB total hashing budget")
        hashes[forge_utils.canonical_workspace_path(path)] = hashlib.sha256(raw).hexdigest()
    return hashes


def receipt_matches(workspace_dir, receipt):
    """Recheck model-independent hashes before accepting completion."""
    try:
        expected = receipt["checked_files"]
        return bool(receipt["ok"]) and _snapshot(workspace_dir, expected) == expected
    except (KeyError, OSError, ValueError):
        return False


def read_diagnostics(workspace_dir, receipt, *, task_id, check=0, offset=1, limit=200):
    if not re.fullmatch(r"diagnostics/[a-f0-9]{32}\.json", receipt):
        raise ValueError("A diagnostic receipt reference is required")
    source = forge_utils.safe_path(workspace_dir, receipt, base="")
    if source.stat().st_size > forge_utils.MAX_FILE:
        raise ValueError("Diagnostic receipt exceeds 1 MiB")
    record = json.loads(_small_text(source))
    if not task_id or record.get("task_id") != task_id:
        raise PermissionError("Diagnostic receipt belongs to another task")
    checks = record.get("checks", [])
    if isinstance(check, bool) or not isinstance(check, int) or check < 0 or check >= len(checks):
        raise ValueError("Diagnostic check index is out of range")
    selected = checks[check]
    return json.dumps({"receipt": receipt, "attempt_id": record.get("attempt_id"), "ok": record.get("ok"),
                       "check": check, "argv": selected["argv"], "exit_code": selected["exit_code"],
                       "truncated": selected["truncated"], "output": forge_utils._page_of(selected["output"], offset, limit)}, ensure_ascii=False)


def model_report(report, budget=20000):
    """Keep the JSON reply valid within the conversation's tool-result budget."""
    result = {key: value for key, value in report.items() if key not in {"checks", "checked_files", "errors"}}
    result["checked_file_count"] = len(report.get("checked_files", {}))
    result["checks"] = [{key: value for key, value in check.items() if key not in {"errors", "output_excerpt"}}
                        for check in report["checks"]]
    result["errors"] = [{**error, "message": error["message"][:500]} for error in report["errors"][:10]]
    result["output_excerpts"] = [check.get("output_excerpt", "")[-1000:] for check in report["checks"]]
    result["details_truncated"] = len(report["errors"]) > len(result["errors"])
    while len(json.dumps(result, ensure_ascii=False)) > budget:
        result["details_truncated"] = True
        if result["output_excerpts"]:
            result["output_excerpts"].pop()
        elif result["errors"]:
            result["errors"].pop()
        elif result["checks"]:
            result["checks"].pop()
        else:
            raise ValueError("Diagnostic summary exceeds tool-result budget")
    return json.dumps(result, ensure_ascii=False)


def run_diagnostics(workspace_dir, kind="auto", path=".", *, tracked_paths=(),
                    allow_native=False, allow_network=False, allow_workspace_write=True, attempt_id="", task_id=""):
    plans, reason = detect_diagnostics(workspace_dir, kind, path)
    if not plans:
        return {"ok": False, "summary": reason, "checks": [], "errors": [], "receipt": None}
    before = _snapshot(workspace_dir, tracked_paths)
    checks = []
    for plan in plans:
        output = forge_utils.execute_terminal_command(plan["argv"], workspace_dir,
                        allow_native=allow_native, allow_network=allow_network, command_cwd=plan.get("cwd", "."),
                        allow_workspace_write=allow_workspace_write)
        exit_match = re.match(r"Exit code: (-?\d+)\n", output)
        exit_code = int(exit_match[1]) if exit_match else None
        parsed = parse_diagnostic_output(output, plan["kind"])
        ok = exit_code == 0 and (plan["kind"] != "test" or parsed["tests_observed"])
        checks.append({**plan, **parsed, "ok": bool(ok), "exit_code": exit_code, "output": output[:20000],
                       "truncated": len(output) > 20000 or "[reticle: output" in output})
    after = _snapshot(workspace_dir, tracked_paths)
    unchanged = before == after
    ok = all(check["ok"] for check in checks) and unchanged
    receipt = {"version": 1, "attempt_id": attempt_id, "task_id": task_id, "created_at": time.time(),
            "ok": ok, "checked_files": before, "artifacts_unchanged": unchanged, "checks": checks}
    directory = forge_utils.safe_path(workspace_dir, "diagnostics", base="")
    directory.mkdir(exist_ok=True)
    target = directory/(uuid.uuid4().hex+".json")
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=directory, delete=False) as stream:
            temporary = Path(stream.name)
            json.dump(receipt, stream, ensure_ascii=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, target)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    summary = f"{sum(check['ok'] for check in checks)}/{len(checks)} diagnostics passed"
    if not unchanged:
        summary += "; artifacts changed during validation: rerun after edits stop"
    return {"ok": ok, "summary": summary, "checks": [{**{k:v for k,v in check.items() if k != "output"}, "output_excerpt": check["output"][-4000:]} for check in checks],
            "errors": [error for check in checks for error in check["errors"]][:100],
            "checked_files": before, "receipt": "diagnostics/"+target.name}
