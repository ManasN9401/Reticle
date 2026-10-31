"""Run fixed requests through a live Forge and score what each run produced.

    python tools/eval_runs.py check                      validate the cases; needs no Forge
    python tools/eval_runs.py run [--cases a,b] [--repeat N] [--execute]
                                                         submit each case to a running Forge and score it
    python tools/eval_runs.py score SESSION_DIR --case ID
                                                         score an existing run's session directory
    python tools/eval_runs.py compare A.json B.json      compare two results files case by case

Every check is objective and recorded separately; a case's score is the fraction of its
applicable checks that passed. See docs/run-evaluation.md for what each check means.
"""
import argparse
import asyncio
import fnmatch
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
LIB = ROOT / "cmd" / "forge" / "compiler" / "lib"
sys.path.insert(0, str(LIB))
CASES = ROOT / "evals" / "runs" / "cases.json"

KINDS = {"web", "python"}
DEPTHS = {1, 3, 5}
EXPECT_KEYS = {"files", "mentions", "images", "canvas", "test_command"}
IMAGE_MAGIC = {".png": (b"\x89PNG",), ".jpg": (b"\xff\xd8",), ".jpeg": (b"\xff\xd8",), ".webp": (b"RIFF",)}
TEXT_SUFFIXES = {".html", ".htm", ".css", ".js", ".mjs", ".ts", ".tsx", ".jsx", ".json", ".md", ".txt",
                 ".py", ".toml", ".cfg", ".ini", ".yml", ".yaml", ".csv", ".svg", ".sh"}
# Generated or tool-owned directories that are not part of what the run delivered.
SKIP_DIRS = {"node_modules", ".git", "__pycache__", ".venv", "venv", ".pytest_cache", "run_page"}
I = re.IGNORECASE
PLACEHOLDERS = [
    (re.compile(r"lorem ipsum", I), "lorem ipsum filler"),
    # Upper case only: a to-do app legitimately says "todo" everywhere.
    (re.compile(r"(#|//|/\*|<!--)\s*(TODO|FIXME)\b"), "TODO/FIXME left in"),
    (re.compile(r"\byour (code|content|text) (goes )?here\b", I), "'your code here' stub"),
    (re.compile(r"\b(rest of (the )?(code|implementation)|implementation goes here)\b", I), "elided implementation"),
    (re.compile(r"via\.placeholder\.com|placehold\.(it|co)|picsum\.photos|source\.unsplash\.com|dummyimage\.com", I), "remote placeholder image"),
]
TERMINAL = {"COMPLETED", "FAILED"}


# ------------------------------------------------------------------------------------------
# Cases
# ------------------------------------------------------------------------------------------

def load_cases(path=CASES, only=None):
    cases = json.loads(Path(path).read_text(encoding="utf-8"))
    ids = set()
    for case in cases:
        cid = case.get("id")
        if not isinstance(cid, str) or not re.fullmatch(r"[a-z0-9-]+", cid) or cid in ids:
            raise ValueError(f"invalid or duplicate case id: {cid!r}")
        ids.add(cid)
        if set(case) - {"id", "kind", "depth", "effort", "timeout_minutes", "prompt", "expect"}:
            raise ValueError(f"{cid}: unknown keys {sorted(set(case) - {'id', 'kind', 'depth', 'effort', 'timeout_minutes', 'prompt', 'expect'})}")
        if case.get("kind") not in KINDS or case.get("depth") not in DEPTHS:
            raise ValueError(f"{cid}: kind must be one of {sorted(KINDS)} and depth one of {sorted(DEPTHS)}")
        if not isinstance(case.get("prompt"), str) or len(case["prompt"]) < 40:
            raise ValueError(f"{cid}: prompt must be a full request")
        if not isinstance(case.get("timeout_minutes"), (int, float)) or case["timeout_minutes"] <= 0:
            raise ValueError(f"{cid}: timeout_minutes must be positive")
        expect = case.get("expect")
        if not isinstance(expect, dict) or set(expect) - EXPECT_KEYS:
            raise ValueError(f"{cid}: expect may only contain {sorted(EXPECT_KEYS)}")
        if not expect.get("files") or not all(isinstance(p, str) for p in expect["files"]):
            raise ValueError(f"{cid}: expect.files must list at least one glob")
        if not all(isinstance(m, str) and m for m in expect.get("mentions", [])):
            raise ValueError(f"{cid}: expect.mentions must be non-empty strings")
        if "images" in expect and (not isinstance(expect["images"], int) or expect["images"] < 1):
            raise ValueError(f"{cid}: expect.images must be a positive integer")
        if ("canvas" in expect or "images" in expect) and case["kind"] != "web":
            raise ValueError(f"{cid}: canvas and images apply only to web cases")
        if "test_command" in expect and (case["kind"] != "python" or not isinstance(expect["test_command"], list)):
            raise ValueError(f"{cid}: test_command is a list of python arguments for a python case")
    if only:
        wanted = set(only)
        if wanted - ids:
            raise ValueError(f"unknown cases: {sorted(wanted - ids)}")
        cases = [case for case in cases if case["id"] in wanted]
    return cases

def case_digest(case):
    """Identifies a case's exact definition, so a comparison can tell when a case changed."""
    return hashlib.sha256(json.dumps(case, sort_keys=True).encode()).hexdigest()[:12]


# ------------------------------------------------------------------------------------------
# Scoring a workspace
# ------------------------------------------------------------------------------------------

def delivered_files(src):
    files = []
    for path in sorted(src.rglob("*")):
        rel = path.relative_to(src)
        if path.is_file() and not SKIP_DIRS.intersection(rel.parts[:-1]):
            files.append(path)
    return files

def glob_matches(rel, pattern):
    """`**/x` matches x at any depth, including the top level."""
    if pattern.startswith("**/"):
        tail = pattern[3:]
        return fnmatch.fnmatch(rel, tail) or fnmatch.fnmatch(rel, "*/" + tail)
    return fnmatch.fnmatch(rel, pattern)

def read_text(path):
    if path.suffix.lower() not in TEXT_SUFFIXES:
        return None
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return None

def find_placeholders(texts):
    hits = []
    for rel, text in texts.items():
        for lineno, line in enumerate(text.splitlines(), 1):
            for pattern, label in PLACEHOLDERS:
                if pattern.search(line):
                    hits.append(f"{rel}:{lineno}: {label}")
    return hits

def generated_images(src, files, texts):
    """Real image files the delivered pages, styles or scripts actually reference."""
    found, unreferenced = [], []
    corpus = "\n".join(texts.values())
    for path in files:
        magic = IMAGE_MAGIC.get(path.suffix.lower())
        if not magic or path.stat().st_size < 1024:
            continue
        with open(path, "rb") as stream:
            head = stream.read(8)
        if not head.startswith(magic):
            continue
        (found if path.name in corpus else unreferenced).append(path.relative_to(src).as_posix())
    return found, unreferenced

def project_dir(src, files):
    """Where to run a Python project's tests from: the shallowest directory holding project files."""
    markers = {"requirements.txt", "pyproject.toml", "setup.py", "setup.cfg", "README.md"}
    dirs = sorted({path.parent for path in files if path.name in markers}, key=lambda d: len(d.parts))
    return dirs[0] if dirs else src

def run_tests(case, src, files, timeout=180):
    cwd = project_dir(src, files)
    command = [sys.executable, *case["expect"]["test_command"]]
    try:
        done = subprocess.run(command, cwd=cwd, capture_output=True, text=True, timeout=timeout,
                              env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
    except subprocess.TimeoutExpired:
        return False, {"cwd": str(cwd), "error": f"tests did not finish within {timeout}s"}
    output = (done.stdout + done.stderr)[-4000:]
    detail = {"cwd": str(cwd.relative_to(src)) if cwd != src else ".", "returncode": done.returncode, "output_tail": output}
    missing = re.search(r"No module named '([\w.]+)'", output)
    if done.returncode != 0 and missing:
        # A dependency the project declares but this interpreter lacks is an environment gap,
        # not a defect of the run. A module it neither declares nor contains is a real failure.
        name = missing.group(1).split(".")[0].lower()
        declared = " ".join((read_text(p) or "").lower() for p in files if p.name == "requirements.txt")
        if re.search(rf"(^|\s){re.escape(name)}([<>=~!\[\s]|$)", declared):
            detail["skipped"] = f"declared dependency {name!r} is not installed for {sys.executable}"
            return None, detail
    return done.returncode == 0, detail

def browser_checks(case, workspace, out_dir):
    import browser_tools
    result = browser_tools.observe_page(str(workspace), wait_ms=3000, screenshot=True)
    if "error" in result:
        unavailable = result["error"] == browser_tools.BROWSER_HELP
        reason = "no headless browser is available" if unavailable else result["error"]
        verdict = None if unavailable else False
        checks = {"page_loads": verdict, "no_page_errors": None if unavailable else False, "has_content": None if unavailable else False}
        if case["expect"].get("canvas"):
            checks["animates"] = checks["has_content"]
        return checks, {"browser_error": reason}
    obs = result["observation"]
    stats = obs.get("stats")
    if result["image"] and out_dir:
        (out_dir / "screenshot.png").write_bytes(result["image"])
    canvases = (obs.get("page") or {}).get("canvases") or []
    checks = {
        "page_loads": bool(obs["loaded"] and not obs["navigation_error"]),
        "no_page_errors": browser_tools.count_problems(obs) == 0,
        # Mostly one colour is an empty page, whatever produced it.
        "has_content": None if stats is None else stats["dominant"] < 0.95,
    }
    if case["expect"].get("canvas"):
        checks["animates"] = obs["frames_during_wait"] > 0 and any(c.get("context") not in (None, "none") for c in canvases)
    detail = {
        "page": result["page"], "backend": result["backend"], "load_ms": obs["load_ms"],
        "problems": browser_tools.count_problems(obs),
        "exceptions": obs["exceptions"][:5], "console_errors": [t for k, t in obs["console"] if k in ("error", "assert")][:5],
        "failed_requests": obs["failed"][:5], "http_errors": obs["http_errors"][:5],
        "screen": browser_tools.blank_verdict(stats), "frames": obs["frames_during_wait"],
        "title": (obs.get("page") or {}).get("title"),
    }
    return checks, detail

def score_workspace(case, workspace, *, finished=True, execute=False, image_generation=None, browser=True, out_dir=None):
    """Score a run's session directory (the one holding src/). Returns {checks, details, score}."""
    workspace = Path(workspace)
    src = workspace / "src"
    files = delivered_files(src) if src.is_dir() else []
    rels = {path: path.relative_to(src).as_posix() for path in files}
    texts = {rels[path]: text for path in files if (text := read_text(path)) is not None}
    expect = case["expect"]
    checks, details = {"finished": finished}, {"file_count": len(files), "bytes": sum(p.stat().st_size for p in files)}

    missing = [pattern for pattern in expect["files"]
               if not any(glob_matches(rels[p], pattern) and p.stat().st_size > 0 for p in files)]
    checks["files_present"] = not missing
    details["missing_files"] = missing

    placeholders = find_placeholders(texts)
    checks["no_placeholders"] = bool(files) and not placeholders  # an empty run earns nothing
    details["placeholders"] = placeholders[:10]

    if expect.get("mentions"):
        corpus = "\n".join([*texts.values(), *rels.values()]).lower()
        absent = [m for m in expect["mentions"] if m.lower() not in corpus]
        checks["mentions"] = not absent
        details["missing_mentions"] = absent

    if case["kind"] == "web":
        if files:
            import web_tools
            report, blocking = web_tools.run_check(str(workspace))
            checks["static_web"] = blocking == 0
            details["static_web_report"] = report[:3000]
        else:
            checks["static_web"] = False
        if browser and files:
            browser_result, browser_detail = browser_checks(case, workspace, out_dir)
            checks.update(browser_result)
            details["browser"] = browser_detail
        else:
            for name in ("page_loads", "no_page_errors", "has_content") + (("animates",) if expect.get("canvas") else ()):
                checks[name] = False if not files else None
        if expect.get("images"):
            found, unreferenced = generated_images(src, files, texts) if files else ([], [])
            details["images"] = {"referenced": found, "unreferenced": unreferenced, "expected": expect["images"]}
            if image_generation is False:
                checks["images"] = None
                details["images"]["skipped"] = "ComfyUI could not generate images when the run started"
            else:
                checks["images"] = len(found) >= expect["images"]

    if case["kind"] == "python":
        failures = []
        for path in files:
            if path.suffix == ".py":
                try:
                    compile(path.read_text(encoding="utf-8", errors="replace"), rels[path], "exec")
                except SyntaxError as error:
                    failures.append(f"{rels[path]}:{error.lineno}: {error.msg}")
        checks["python_compiles"] = bool(files) and not failures
        details["syntax_errors"] = failures
        if expect.get("test_command"):
            if execute and files:
                checks["tests_pass"], details["tests"] = run_tests(case, src, files)
            else:
                checks["tests_pass"] = None if files else False
                details["tests"] = {"skipped": "run with --execute to run the generated tests"} if files else {}

    applicable = [value for value in checks.values() if value is not None]
    return {"checks": checks, "details": details,
            "score": round(sum(applicable) / len(applicable), 3) if applicable else 0.0}


# ------------------------------------------------------------------------------------------
# Run metrics from the runtime's execution record
# ------------------------------------------------------------------------------------------

def run_metrics(reticle_root, exec_id):
    """Node and attempt counts for a run and its compile phase, from .reticle/executions/state.json."""
    path = Path(reticle_root) / ".reticle" / "executions" / "state.json"
    try:
        executions = json.loads(path.read_text(encoding="utf-8")).get("executions", {})
    except (OSError, ValueError):
        return {}
    metrics = {"nodes": 0, "nodes_done": 0, "attempts": 0, "failed_attempts": 0, "models": {}, "failure_reasons": {}}
    for key in (exec_id, "compile-" + exec_id):
        record = executions.get(key)
        if not isinstance(record, dict):
            continue
        states = record.get("node_states") or {}
        if key == exec_id:
            metrics["nodes"] = len(states)
            metrics["nodes_done"] = sum(1 for state in states.values() if state == "done")
        for attempts in (record.get("attempts") or {}).values():
            for attempt in attempts if isinstance(attempts, list) else []:
                metrics["attempts"] += 1
                model = attempt.get("model") or "unknown"
                metrics["models"][model] = metrics["models"].get(model, 0) + 1
                if attempt.get("state") == "failed":
                    metrics["failed_attempts"] += 1
                    reason = attempt.get("reason") or "unknown"
                    metrics["failure_reasons"][reason] = metrics["failure_reasons"].get(reason, 0) + 1
    return metrics


# ------------------------------------------------------------------------------------------
# Driving a live Forge
# ------------------------------------------------------------------------------------------

def approve_pending(reticle_root, exec_id, approved):
    """Approve this run's checkpoints, and only this run's: a request names its task, which
    starts with the execution id. The decision is bound to the request file's exact bytes."""
    directory = Path(reticle_root) / ".reticle" / "approvals"
    owners = {exec_id, "compile-" + exec_id}
    for request in sorted(directory.glob("approval_req_*.md")) if directory.is_dir() else []:
        decision = request.with_name(request.name + ".decision.json")
        if decision.exists() or request.name in approved:
            continue
        content = request.read_bytes()
        block = re.search(rb"```json\r?\n(.*?)\r?\n```", content, re.S)
        try:
            task = json.loads(block.group(1)).get("task", "") if block else ""
        except ValueError:
            continue
        if str(task).split("|")[0] not in owners:
            continue
        decision.write_text(json.dumps({"hash": hashlib.sha256(content).hexdigest(), "decision": "APPROVED",
                                        "feedback": "approved automatically by tools/eval_runs.py"}), encoding="utf-8")
        approved[request.name] = str(task)

def control_token(reticle_root):
    try:
        return (Path(reticle_root) / ".reticle" / "control-token").read_text(encoding="utf-8").strip()
    except OSError:
        return ""

def forge_models(port, token):
    request = urllib.request.Request(f"http://127.0.0.1:{port}/api/models",
                                     headers={"Authorization": f"Bearer {token}"} if token else {})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            models = json.loads(response.read(8 * 1024 * 1024))
    except Exception as error:  # noqa: BLE001 - recorded, not fatal
        return {"error": str(error)}
    enabled = sorted(m.get("id") or m.get("ID") for m in models or []
                     if isinstance(m, dict) and (m.get("enabled", m.get("Enabled")) is True))
    return {"enabled": enabled}

def git_state(reticle_root):
    def git(*args):
        try:
            return subprocess.run(["git", "-C", str(reticle_root), *args], capture_output=True, text=True, timeout=20).stdout.strip()
        except (OSError, subprocess.TimeoutExpired):
            return ""
    return {"commit": git("rev-parse", "HEAD"), "dirty_files": len([l for l in git("status", "--porcelain").splitlines() if l.strip()])}

async def drive_case(case, *, port, token, reticle_root, group, auto_approve, log):
    """Submit one case and follow it to a terminal status. Returns (exec_id, status, seconds, approved)."""
    import websockets
    uri = f"ws://127.0.0.1:{port}/ws"
    headers = {"Authorization": f"Bearer {token}"} if token else {}
    deadline = time.monotonic() + case["timeout_minutes"] * 60
    started, exec_id, status, approved, submitted, killed = time.monotonic(), None, None, {}, False, False
    while True:
        try:
            async with websockets.connect(uri, additional_headers=headers, max_size=64 * 1024 * 1024) as ws:
                if not submitted:
                    await ws.send(json.dumps({"action": "enqueue", "prompt": case["prompt"], "mode": "parallel", "group": group,
                                              "agent_complexity": case["depth"], "effort": case.get("effort", "auto")}))
                    submitted = True
                    log(f"submitted {case['id']} (depth {case['depth']}, timeout {case['timeout_minutes']} min)")
                quiet_since = time.monotonic()
                while True:
                    try:
                        message = json.loads(await asyncio.wait_for(ws.recv(), timeout=1))
                    except asyncio.TimeoutError:
                        message = None
                    if message and message.get("Type") == "WaitlistUpdated":
                        quiet_since = time.monotonic()
                        for item in (message.get("Payload") or {}).get("items") or []:
                            if item.get("group") == group:
                                if exec_id is None:
                                    log(f"{case['id']} is {item.get('id')}")
                                exec_id, status = item.get("id"), item.get("status")
                    if exec_id and auto_approve:
                        before = len(approved)
                        approve_pending(reticle_root, exec_id, approved)
                        for name in list(approved)[before:]:
                            log(f"approved checkpoint {approved[name]}")
                    if status in TERMINAL:
                        return exec_id, status, time.monotonic() - started, approved
                    if time.monotonic() > deadline and not killed:
                        log(f"{case['id']} exceeded {case['timeout_minutes']} min; stopping it")
                        if exec_id:
                            await ws.send(json.dumps({"action": "kill", "id": exec_id}))
                        killed, deadline = True, time.monotonic() + 120
                    elif killed and time.monotonic() > deadline:
                        return exec_id, "TIMEOUT", time.monotonic() - started, approved
                    if time.monotonic() - quiet_since > 60:
                        break  # reconnecting makes the runtime broadcast the queue again
        except (OSError, websockets.exceptions.WebSocketException) as error:
            if not submitted:
                raise RuntimeError(f"cannot reach Forge on port {port}: {error}") from error
            log(f"connection lost ({error}); reconnecting")
            await asyncio.sleep(3)
        if killed and time.monotonic() > deadline:
            return exec_id, "TIMEOUT", time.monotonic() - started, approved


def summarise(results):
    """Per-case mean score and finish rate across repeats."""
    summary = {}
    for result in results:
        entry = summary.setdefault(result["case"], {"runs": 0, "finished": 0, "score_total": 0.0, "minutes_total": 0.0, "checks": {}})
        entry["runs"] += 1
        entry["finished"] += bool(result["checks"].get("finished"))
        entry["score_total"] += result["score"]
        entry["minutes_total"] += result.get("seconds", 0) / 60
        for name, value in result["checks"].items():
            if value is not None:
                tally = entry["checks"].setdefault(name, [0, 0])
                tally[0] += bool(value)
                tally[1] += 1
    for entry in summary.values():
        entry["mean_score"] = round(entry.pop("score_total") / entry["runs"], 3)
        entry["mean_minutes"] = round(entry.pop("minutes_total") / entry["runs"], 1)
    return summary

def markdown_summary(document):
    env = document["environment"]
    lines = [f"# Run evaluation {document['run_id']}", "",
             f"Commit `{env['git'].get('commit', '')[:10]}` ({env['git'].get('dirty_files', 0)} uncommitted files), "
             f"image generation {'available' if env.get('image_generation') else 'unavailable'}, "
             f"enabled models: {', '.join(env.get('models', {}).get('enabled', [])) or 'unknown'}.", "",
             "| Case | Runs | Finished | Mean score | Mean minutes | Failed checks |", "|---|---|---|---|---|---|"]
    for case, entry in document["summary"].items():
        failed = ", ".join(f"{name} {passed}/{total}" for name, (passed, total) in entry["checks"].items() if passed < total)
        lines.append(f"| {case} | {entry['runs']} | {entry['finished']}/{entry['runs']} | {entry['mean_score']:.2f} | "
                     f"{entry['mean_minutes']} | {failed or '-'} |")
    overall = [entry["mean_score"] for entry in document["summary"].values()]
    if overall:
        lines += ["", f"Overall mean score: {sum(overall) / len(overall):.2f} across {len(overall)} cases."]
    return "\n".join(lines) + "\n"

def command_run(args):
    import comfy_tools
    cases = load_cases(args.cases_file, args.cases.split(",") if args.cases else None)
    reticle_root = Path(args.reticle_root).resolve()
    token = control_token(reticle_root)
    git = git_state(reticle_root)
    run_id = time.strftime("%Y%m%d-%H%M%S") + "-" + (git["commit"][:7] or "nogit")
    out = Path(args.out or ROOT / ".reticle" / "evals" / run_id)
    out.mkdir(parents=True, exist_ok=True)
    log_path = out / "run.log"
    def log(text):
        line = f"[{time.strftime('%H:%M:%S')}] {text}"
        print(line, flush=True)
        with open(log_path, "a", encoding="utf-8") as stream:
            stream.write(line + "\n")
    environment = {"git": git, "reticle_root": str(reticle_root), "port": args.port, "python": sys.version.split()[0],
                   "models": forge_models(args.port, token), "auto_approve": not args.no_auto_approve, "execute": args.execute}
    if "error" in environment["models"]:
        raise SystemExit(f"Forge is not answering on port {args.port}: {environment['models']['error']}")
    log(f"evaluating {len(cases)} case(s) x {args.repeat} against {reticle_root} (commit {git['commit'][:10]}); "
        f"enabled models: {', '.join(environment['models']['enabled']) or 'none reported'}")
    results = []
    for repeat in range(1, args.repeat + 1):
        for case in cases:
            image_generation = comfy_tools.local_image_generation_available()
            environment.setdefault("image_generation", image_generation)
            group = f"eval-{run_id}-{case['id']}-{repeat}"
            exec_id, status, seconds, approved = asyncio.run(drive_case(
                case, port=args.port, token=token, reticle_root=reticle_root, group=group,
                auto_approve=not args.no_auto_approve, log=log))
            case_dir = out / f"{case['id']}-{repeat}"
            case_dir.mkdir(exist_ok=True)
            session = reticle_root / ".reticle" / "sessions" / (exec_id or "")
            workspace = case_dir / "workspace"
            if exec_id and session.is_dir():
                shutil.copytree(session, workspace, dirs_exist_ok=True, ignore=shutil.ignore_patterns("node_modules", ".venv", "__pycache__"))
            scored = score_workspace(case, workspace, finished=status == "COMPLETED", execute=args.execute,
                                     image_generation=image_generation, browser=not args.no_browser, out_dir=case_dir)
            result = {"case": case["id"], "repeat": repeat, "case_digest": case_digest(case), "exec_id": exec_id,
                      "status": status, "seconds": round(seconds), "image_generation": image_generation,
                      "approvals": sorted(approved.values()), "metrics": run_metrics(reticle_root, exec_id or ""), **scored}
            results.append(result)
            (case_dir / "result.json").write_text(json.dumps(result, indent=2), encoding="utf-8")
            failed = [name for name, value in scored["checks"].items() if value is False]
            log(f"{case['id']} #{repeat}: {status} in {seconds / 60:.1f} min, score {scored['score']:.2f}"
                + (f"; failed: {', '.join(failed)}" if failed else ""))
    document = {"run_id": run_id, "environment": environment, "results": results, "summary": summarise(results)}
    (out / "results.json").write_text(json.dumps(document, indent=2), encoding="utf-8")
    (out / "summary.md").write_text(markdown_summary(document), encoding="utf-8")
    print("\n" + markdown_summary(document))
    print(f"Results: {out / 'results.json'}")

def command_score(args):
    case = load_cases(args.cases_file, [args.case])[0]
    out = Path(args.out) if args.out else None
    if out:
        out.mkdir(parents=True, exist_ok=True)
    scored = score_workspace(case, args.workspace, execute=args.execute, browser=not args.no_browser, out_dir=out)
    print(json.dumps(scored, indent=2))

def compare(a, b):
    """Markdown comparing two results documents case by case (B relative to A)."""
    lines = [f"# {a['run_id']} (A) vs {b['run_id']} (B)", ""]
    models_a, models_b = a["environment"].get("models", {}).get("enabled"), b["environment"].get("models", {}).get("enabled")
    if models_a != models_b:
        lines += [f"Warning: the enabled models differ (A: {models_a}; B: {models_b}), so differences may come from the models.", ""]
    digests = lambda doc: {r["case"]: r["case_digest"] for r in doc["results"]}
    changed = [case for case, digest in digests(b).items() if digests(a).get(case) not in (None, digest)]
    if changed:
        lines += [f"Warning: these cases were edited between the runs: {', '.join(changed)}.", ""]
    lines += ["| Case | Score A | Score B | Change | Finished A | Finished B | Checks that changed |", "|---|---|---|---|---|---|---|"]
    for case in sorted(set(a["summary"]) | set(b["summary"])):
        sa, sb = a["summary"].get(case), b["summary"].get(case)
        if not sa or not sb:
            lines.append(f"| {case} | {sa['mean_score'] if sa else '-'} | {sb['mean_score'] if sb else '-'} | - | - | - | only in one run |")
            continue
        moved = []
        for name in sorted(set(sa["checks"]) | set(sb["checks"])):
            ra = sa["checks"].get(name)
            rb = sb["checks"].get(name)
            fa = ra[0] / ra[1] if ra else None
            fb = rb[0] / rb[1] if rb else None
            if fa != fb:
                moved.append(f"{name} {'-' if fa is None else f'{fa:.0%}'}→{'-' if fb is None else f'{fb:.0%}'}")
        delta = sb["mean_score"] - sa["mean_score"]
        lines.append(f"| {case} | {sa['mean_score']:.2f} | {sb['mean_score']:.2f} | {delta:+.2f} | {sa['finished']}/{sa['runs']} | "
                     f"{sb['finished']}/{sb['runs']} | {', '.join(moved) or '-'} |")
    runs = min(min(e["runs"] for e in a["summary"].values()), min(e["runs"] for e in b["summary"].values()))
    if runs < 3:
        lines += ["", f"Each case ran {runs} time(s) in at least one document; model output varies run to run, so treat a "
                      "single-run difference smaller than one check as noise. Use --repeat 3 or more to compare."]
    return "\n".join(lines) + "\n"

def command_compare(args):
    load = lambda path: json.loads(Path(path).read_text(encoding="utf-8"))
    print(compare(load(args.a), load(args.b)))

def command_check(args):
    cases = load_cases(args.cases_file)
    import web_tools, browser_tools, comfy_tools  # noqa: F401 - the scorer's dependencies must import
    print(f"{len(cases)} cases are valid: {', '.join(case['id'] for case in cases)}")
    print(f"headless browser: {'found' if browser_tools.choose_backend(browser_tools.find_chromium()) else 'not found (browser checks will be skipped)'}")
    print(f"local image generation: {'available' if comfy_tools.local_image_generation_available() else 'unavailable (image checks will be skipped)'}")

def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--cases-file", default=str(CASES))
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("check", help="validate the cases and the scorer's dependencies")
    run = sub.add_parser("run", help="run cases through a live Forge")
    run.add_argument("--port", type=int, default=8080)
    run.add_argument("--reticle-root", default=str(ROOT), help="the checkout the running Forge was started from")
    run.add_argument("--cases", help="comma-separated case ids (default: all)")
    run.add_argument("--repeat", type=int, default=1)
    run.add_argument("--out", help="results directory (default: .reticle/evals/<run id>)")
    run.add_argument("--execute", action="store_true", help="run the generated tests on this machine")
    run.add_argument("--no-browser", action="store_true", help="skip loading pages in a headless browser")
    run.add_argument("--no-auto-approve", action="store_true", help="leave approval checkpoints for a person")
    score = sub.add_parser("score", help="score an existing session directory")
    score.add_argument("workspace")
    score.add_argument("--case", required=True)
    score.add_argument("--out", help="directory for the screenshot")
    score.add_argument("--execute", action="store_true")
    score.add_argument("--no-browser", action="store_true")
    comp = sub.add_parser("compare", help="compare two results.json files")
    comp.add_argument("a")
    comp.add_argument("b")
    args = parser.parse_args(argv)
    if getattr(args, "repeat", 1) < 1:
        parser.error("--repeat must be at least 1")
    {"check": command_check, "run": command_run, "score": command_score, "compare": command_compare}[args.command](args)

if __name__ == "__main__":
    main()
