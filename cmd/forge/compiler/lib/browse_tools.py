"""browse_web: drive a real headless browser (Playwright) through a public website and read it.

The browser agent's tool. It opens a URL, can click, type, scroll, wait for elements and run small
scripts, and returns what the page shows: its text, headings, links, tables and anything picked out
with CSS selectors. Everything read is saved under browse/ so other agents can use it.

Containment (the page and its scripts are somebody else's code):
* only public http(s) destinations; private, link-local and loopback addresses are refused, and
  every request the page makes, including redirects, is checked;
* read-only by default: only GET/HEAD/OPTIONS leave the browser. allow_submit=true additionally lets
  forms and scripts POST to the site being browsed (never to a third party);
* a throwaway profile, no downloads, no permission prompts, popups closed, dialogs dismissed;
* a hard deadline enforced by a parent process that kills the browser, so a page that never
  yields cannot hang the worker.

It does not solve CAPTCHAs, bypass access walls or pretend to be a person: when a site shows a
check or a wall the result says so and stops.
"""
import json
import os
import re
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from urllib.parse import urlsplit

from forge_utils import safe_path
import browser_tools as bt

DEFAULT_DEADLINE = 60
MAX_DEADLINE = 180
MAX_ACTIONS = 30
DEFAULT_WAIT_MS = 1500
MAX_WAIT_MS = 15000
TEXT_LIMIT = 200_000

NEEDS_PLAYWRIGHT = (
    "browse_web drives the browser with Playwright, which is not installed in this agent's environment. "
    "An agent granted browser.run receives it as an optional dependency on first use; if the install failed "
    "(no network, or no wheel for this platform) it is retried after an hour. Install it with "
    "`pip install playwright==1.63.0` to use it now."
)

_WALL = re.compile(r"captcha|verify (?:that )?you are (?:a )?human|are you a robot|unusual traffic|access denied|request blocked|enable javascript and cookies|attention required|checking your browser", re.I)

_EXTRACT_JS = """
(spec) => {
  const clean = (s) => (s || '').replace(/\\s+/g, ' ').trim();
  const out = { title: document.title, url: location.href };
  const meta = (n) => { const e = document.querySelector('meta[name="' + n + '"],meta[property="' + n + '"]'); return e ? clean(e.content) : ''; };
  out.description = meta('description') || meta('og:description');
  out.published = meta('article:published_time') || meta('og:published_time') || meta('date');
  const canonical = document.querySelector('link[rel="canonical"]');
  out.canonical = canonical ? canonical.href : '';
  out.lang = document.documentElement.lang || '';
  if (spec.text) {
    // Prefer the page's main content over the navigation around it, when there clearly is one.
    let root = document.body;
    if (spec.main) {
      const found = Array.from(document.querySelectorAll('main, article, [role="main"], #mw-content-text, #content'))
        .map((e) => [e, (e.innerText || '').length]).sort((a, b) => b[1] - a[1]);
      if (found.length && found[0][1] >= 300) root = found[0][0];
    }
    const body = root ? root.innerText : '';
    out.scope = root === document.body ? 'page' : 'main content';
    out.text = body.replace(/\\n{3,}/g, '\\n\\n').trim().slice(0, spec.textLimit);
    out.headings = Array.from(root.querySelectorAll('h1,h2,h3')).slice(0, 40).map((h) => [h.tagName.toLowerCase(), clean(h.innerText)]).filter((h) => h[1]);
  }
  if (spec.links) {
    const seen = new Set();
    out.links = [];
    for (const a of document.querySelectorAll('a[href]')) {
      const href = a.href;
      if (!/^https?:/.test(href) || seen.has(href)) continue;
      seen.add(href);
      out.links.push([clean(a.innerText || a.getAttribute('aria-label') || a.title).slice(0, 100), href]);
      if (out.links.length >= spec.maxLinks) break;
    }
    out.links = out.links.filter((l) => l[0]);
  }
  if (spec.tables) {
    out.tables = Array.from(document.querySelectorAll('table')).slice(0, 3).map((t) =>
      Array.from(t.rows).slice(0, 25).map((r) => Array.from(r.cells).slice(0, 8).map((c) => clean(c.innerText).slice(0, 80))));
  }
  out.selected = {};
  for (const [name, item] of Object.entries(spec.selectors || {})) {
    const css = typeof item === 'string' ? item : item.css;
    const attr = typeof item === 'string' ? null : item.attr;
    try {
      out.selected[name] = Array.from(document.querySelectorAll(css)).slice(0, 40)
        .map((e) => attr ? (e.getAttribute(attr) || '') : clean(e.innerText || e.textContent).slice(0, 300));
    } catch (error) { out.selected[name] = 'invalid selector: ' + error.message; }
  }
  return out;
}
"""


# ---------------------------------------------------------------------------------------------
# The Playwright child (python browse_tools.py --child ...)
# ---------------------------------------------------------------------------------------------

def _child(request, shot_path):
    from playwright.sync_api import sync_playwright  # optional dependency

    observation = {
        "final_url": "", "status": None, "blocked": [], "console": [], "popups": 0, "dialogs": [],
        "downloads": 0, "action_notes": [], "action_errors": [], "page": {}, "typed_password": False,
        "navigation_error": "", "requests": 0,
    }
    deadline = time.monotonic() + float(request["budget"])
    origin_port = request.get("origin_port", -1)
    first_party = request.get("first_party")
    blocked = set()

    def left():
        return max(0.5, deadline - time.monotonic())

    with sync_playwright() as driver:
        launch = {"headless": True, "args": ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--disable-dev-shm-usage"]}
        if request.get("executable"):
            launch["executable_path"] = request["executable"]
        browser = driver.chromium.launch(**launch)
        try:
            context = browser.new_context(
                viewport={"width": 1280, "height": 900}, accept_downloads=False, service_workers="block",
                locale="en-US", java_script_enabled=True, permissions=[],
            )

            def guard(route):
                try:
                    target = route.request
                    allowed, reason = bt.request_allowed(target.url, target.method, origin_port, first_party)
                    if allowed and target.method.upper() not in ("GET", "HEAD", "OPTIONS") and not bt._same_site((urlsplit(target.url).hostname or "").lower(), first_party):
                        # Reading is the default whatever the origin: a POST needs allow_submit and the site being browsed.
                        allowed, reason = False, f"{target.method.upper()} is not allowed in read-only browsing (allow_submit lets forms post to the site you are on)"
                    if allowed:
                        route.continue_()
                    else:
                        blocked.add(target.url)
                        observation["blocked"].append(f"{target.method} {bt._short(target.url, 140)}: {reason}")
                        route.abort()
                except Exception:  # noqa: BLE001 - the page may have closed under the request
                    pass
            context.route("**/*", guard)
            page = context.new_page()
            page.set_default_timeout(int(min(10000, left() * 1000)))

            def on_popup(popup):
                observation["popups"] += 1
                try:
                    popup.close()
                except Exception:  # noqa: BLE001
                    pass
            context.on("page", lambda new: on_popup(new) if new is not page else None)
            page.on("dialog", lambda dialog: (observation["dialogs"].append(bt._short(f"{dialog.type}: {dialog.message}", 160)), dialog.dismiss()))
            page.on("download", lambda download: observation.__setitem__("downloads", observation["downloads"] + 1))
            page.on("console", lambda message: observation["console"].append(bt._short(message.text, 160)) if message.type == "error" and len(observation["console"]) < 8 else None)
            page.on("request", lambda r: observation.__setitem__("requests", observation["requests"] + 1))

            def on_response(response):
                try:
                    if response.request.is_navigation_request() and response.frame == page.main_frame:
                        observation["status"] = response.status
                except Exception:  # noqa: BLE001
                    pass
            page.on("response", on_response)

            try:
                page.goto(request["url"], wait_until="domcontentloaded", timeout=int(min(30000, left() * 1000)))
                try:
                    page.wait_for_load_state("load", timeout=int(min(8000, left() * 1000)))
                except Exception:  # noqa: BLE001 - slow trackers must not fail a page that already shows its content
                    pass
            except Exception as error:  # noqa: BLE001
                observation["navigation_error"] = bt._short(error, 300)

            if not observation["navigation_error"]:
                page.wait_for_timeout(int(min(request["wait_ms"], left() * 1000 - 1000)))
                for number, action in enumerate(request["actions"], 1):
                    if time.monotonic() > deadline - 4:
                        observation["action_errors"].append("stopped: out of time")
                        break
                    try:
                        _do(page, action, observation, request, left)
                    except Exception as error:  # noqa: BLE001
                        text = bt._short(error, 240)
                        if "click" in action and "Timeout" in text:
                            text = f"no clickable element matches {action['click']!r} (waited {min(10, left()):.0f} s)"
                        observation["action_errors"].append(f"{number}. {json.dumps(action)[:120]}: {text}")

            observation["final_url"] = page.url
            try:
                spec = {
                    "text": True, "main": request["extract"].get("main", True), "textLimit": TEXT_LIMIT, "links": request["extract"].get("links", True), "maxLinks": 80,
                    "tables": request["extract"].get("tables", False), "selectors": request["extract"].get("selectors") or {},
                }
                observation["page"] = page.evaluate(_EXTRACT_JS, spec)
            except Exception as error:  # noqa: BLE001
                observation["page"] = {"error": bt._short(error, 300)}
            if request.get("screenshot"):
                try:
                    Path(shot_path).write_bytes(page.screenshot(type="png"))
                except Exception as error:  # noqa: BLE001
                    observation["action_errors"].append(f"screenshot failed: {bt._short(error, 160)}")
        finally:
            try:
                browser.close()
            except Exception:  # noqa: BLE001
                pass
    return observation


def _do(page, action, observation, request, left):
    if not isinstance(action, dict) or not action:
        raise ValueError('an action is an object such as {"click": "#next"}')
    pause = int(min(5000, left() * 1000))
    if "goto" in action:
        page.goto(str(action["goto"]), wait_until="domcontentloaded", timeout=int(min(30000, left() * 1000)))
    elif "click" in action:
        page.click(str(action["click"]), timeout=pause)
        _settle(page, left)
    elif "fill" in action:
        selector, text = action["fill"]
        if page.evaluate("(s) => { const e = document.querySelector(s); return !!e && e.type === 'password'; }", str(selector)):
            observation["typed_password"] = True
        page.fill(str(selector), str(text), timeout=pause)
    elif "press" in action:
        key = str(action["press"])
        if action.get("selector"):
            page.press(str(action["selector"]), key, timeout=pause)
        else:
            page.keyboard.press(key)
        _settle(page, left)
    elif "select" in action:
        selector, value = action["select"]
        page.select_option(str(selector), str(value), timeout=pause)
    elif "scroll" in action:
        page.mouse.wheel(0, float(action["scroll"]))
        page.wait_for_timeout(400)
    elif "wait_ms" in action:
        page.wait_for_timeout(int(min(max(int(action["wait_ms"]), 0), 5000)))
    elif "wait_for" in action:
        page.wait_for_selector(str(action["wait_for"]), timeout=pause)
    elif "eval" in action:
        value = page.evaluate(str(action["eval"]))
        observation["action_notes"].append(f"eval {bt._short(str(action['eval']), 80)} -> {bt._short(json.dumps(value, default=str), 600)}")
    else:
        raise ValueError("unknown action; use goto, click, fill, press, select, scroll, wait_ms, wait_for or eval")


def _settle(page, left):
    try:
        page.wait_for_load_state("domcontentloaded", timeout=int(min(4000, left() * 1000)))
        page.wait_for_timeout(250)
    except Exception:  # noqa: BLE001
        pass


# ---------------------------------------------------------------------------------------------
# The parent: validate, supervise, report
# ---------------------------------------------------------------------------------------------

def _validate_url(url, local_port):
    parts = urlsplit(url) if isinstance(url, str) else None
    if parts is None or parts.scheme not in ("http", "https") or not parts.hostname or parts.username or parts.password:
        return "url must be a public http(s) address (no file:, data: or other schemes, and no user name or password in it)"
    allowed, reason = bt.request_allowed(url, "GET", local_port if local_port is not None else -1)
    return "" if allowed else f"{reason}; browse_web only opens public websites"


def _clean_actions(actions):
    if actions is None:
        return [], ""
    if not isinstance(actions, list):
        return [], 'actions must be a list of objects such as {"click": "#next"}'
    if len(actions) > MAX_ACTIONS:
        return [], f"at most {MAX_ACTIONS} actions per browse_web call"
    return actions, ""


def _next_name(workspace_dir, host):
    folder = safe_path(workspace_dir, "browse")
    folder.mkdir(parents=True, exist_ok=True)
    slug = re.sub(r"[^a-z0-9]+", "-", host.lower()).strip("-") or "site"
    number = 1
    while (folder / f"{slug}-{number}.md").exists() or (folder / f"{slug}-{number}.png").exists():
        number += 1
    return f"browse/{slug}-{number}"


def browse_web(workspace_dir, url, actions=None, extract=None, wait_ms=DEFAULT_WAIT_MS, screenshot=False,
               allow_submit=False, max_chars=None, deadline_seconds=None, _local_port=None):
    problem = _validate_url(url, _local_port)
    if problem:
        return f"Error: {problem}."
    actions, problem = _clean_actions(actions)
    if problem:
        return f"Error: {problem}."
    if extract is not None and not isinstance(extract, dict):
        return 'Error: extract must be an object such as {"links": true, "tables": true, "selectors": {"prices": ".price"}}.'
    if not bt._playwright_available():
        return "Error: " + NEEDS_PLAYWRIGHT
    executable = bt.find_chromium()
    budget = max(15, min(int(deadline_seconds) if isinstance(deadline_seconds, (int, float)) and not isinstance(deadline_seconds, bool) else DEFAULT_DEADLINE, MAX_DEADLINE))
    request = {
        "url": url, "actions": actions, "extract": extract or {}, "executable": executable,
        "wait_ms": max(0, min(int(wait_ms) if isinstance(wait_ms, (int, float)) and not isinstance(wait_ms, bool) else DEFAULT_WAIT_MS, MAX_WAIT_MS)),
        "screenshot": bool(screenshot), "budget": budget - 4,
        "origin_port": _local_port if _local_port is not None else -1,
        "first_party": (urlsplit(url).hostname or "").lower() if allow_submit is True else None,
    }
    bt.sweep_stale_profiles()
    folder = tempfile.mkdtemp(prefix=bt.PROFILE_PREFIX)
    try:
        result_path, shot_path = Path(folder) / "result.json", Path(folder) / "shot.png"
        launch = {"stdin": subprocess.PIPE, "stdout": subprocess.DEVNULL, "stderr": subprocess.PIPE}
        if sys.platform.startswith("win"):
            launch["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0) | getattr(subprocess, "CREATE_NO_WINDOW", 0)
        else:
            launch["start_new_session"] = True
        process = subprocess.Popen([sys.executable, "-B", str(Path(__file__).resolve()), "--child", str(result_path), str(shot_path)], **launch)
        timed_out, stderr = False, b""
        try:
            _, stderr = process.communicate(json.dumps(request).encode("utf-8"), timeout=budget)
        except subprocess.TimeoutExpired:
            timed_out = True
            bt._kill_tree(process)
            try:
                _, stderr = process.communicate(timeout=5)
            except (subprocess.TimeoutExpired, OSError, ValueError):
                stderr = b""
        try:
            outcome = json.loads(result_path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            outcome = None
        if outcome is None:
            if timed_out:
                return f"Error: the page did not finish within {budget} s and the browser was stopped. It may be very slow or its scripts never yield; try a simpler URL, fewer actions, or a smaller wait_ms."
            tail = stderr.decode("utf-8", errors="replace").strip().splitlines()[-1:] or ["no output"]
            return f"Error: the browser could not run ({bt._short(tail[0], 300)})."
        if "error" in outcome:
            return f"Error: the browser could not run ({bt._short(outcome['error'], 300)})."
        base = _next_name(workspace_dir, urlsplit(url).hostname or "site")
        saved_shot = ""
        if screenshot and shot_path.is_file():
            target = safe_path(workspace_dir, base + ".png")
            target.write_bytes(shot_path.read_bytes())
            saved_shot = base + ".png"
        return _report(workspace_dir, base, saved_shot, url, outcome, timed_out, max_chars)
    finally:
        for _ in range(15):
            try:
                import shutil
                shutil.rmtree(folder)
                break
            except OSError:
                time.sleep(0.3)


def _report(workspace_dir, base, shot, requested, observation, timed_out, max_chars):
    page = observation.get("page") or {}
    status = observation.get("status")
    lines = []
    final = observation.get("final_url") or requested
    head = f"Browsed {requested}"
    if final.rstrip("/") != requested.rstrip("/"):
        head += f" -> {final}"
    lines.append(head + (f" (HTTP {status})" if status else ""))
    if observation.get("navigation_error"):
        lines.append("NAVIGATION FAILED: " + bt._explain_error(observation["navigation_error"]))
    if page.get("title"):
        lines.append(f"Title: {page['title']}")
    for key in ("description", "published"):
        if page.get(key):
            lines.append(f"{key.capitalize()}: {bt._short(page[key], 240)}")
    text = page.get("text", "") if isinstance(page, dict) else ""
    wall = status in (401, 403, 429) or bool(text and len(text) < 1500 and _WALL.search(text))
    if wall:
        lines.append(f"ACCESS WALL: the site answered {status or 'with a check'} or shows a bot/consent check. Do not try to get around it; "
                     "report that this page could not be read, or ask the user for another source.")
    if timed_out:
        lines.append("The time limit was reached; what follows is what the page showed when the browser was stopped.")
    if observation.get("blocked"):
        lines.append(f"Blocked by the sandbox ({len(observation['blocked'])}): " + "; ".join(observation["blocked"][:5]))
    if observation.get("popups"):
        lines.append(f"{observation['popups']} popup window(s) were closed.")
    if observation.get("dialogs"):
        lines.append("Dialogs dismissed: " + "; ".join(observation["dialogs"][:4]))
    if observation.get("downloads"):
        lines.append(f"{observation['downloads']} download(s) were refused (downloads are not allowed).")
    if observation.get("typed_password"):
        lines.append("Note: a password field was filled by an action. Credentials belong only in a task the user gave them for.")
    if observation.get("console"):
        lines.append("Console errors: " + "; ".join(observation["console"][:3]))
    for note in observation.get("action_notes", []):
        lines.append(note)
    if observation.get("action_errors"):
        lines.append("Action problems:")
        lines.extend("  - " + item for item in observation["action_errors"])
    if page.get("error"):
        lines.append("Could not read the page: " + page["error"])

    saved = [f"# {page.get('title') or final}", f"url: {final}", f"retrieved: {time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}", ""]
    saved.append(text)
    if page.get("links"):
        saved += ["", "## Links"] + [f"- [{label}]({href})" for label, href in page["links"]]
    if page.get("tables"):
        for number, table in enumerate(page["tables"], 1):
            saved += ["", f"## Table {number}"] + ["\t".join(row) for row in table]
    if page.get("selected"):
        saved += ["", "## Selected"] + [f"{name}: {json.dumps(values, ensure_ascii=False)}" for name, values in page["selected"].items()]
    target = safe_path(workspace_dir, base + ".md")
    target.write_text("\n".join(saved), encoding="utf-8", newline="\n")

    budget = max(1500, bt_reply_budget() - len("\n".join(lines)) - 1500)
    limit = int(max_chars) if isinstance(max_chars, (int, float)) and not isinstance(max_chars, bool) and max_chars > 0 else 6000
    limit = min(limit, budget)
    if page.get("headings"):
        lines.append("Headings: " + " | ".join(f"{tag} {text_}" for tag, text_ in page["headings"][:12]))
    if page.get("selected"):
        for name, values in page["selected"].items():
            shown = values if isinstance(values, str) else json.dumps(values[:12], ensure_ascii=False)
            lines.append(f"Selected {name} ({len(values) if not isinstance(values, str) else 0}): {bt._short(shown, 700)}")
    if page.get("tables"):
        for number, table in enumerate(page["tables"], 1):
            lines.append(f"Table {number} ({len(table)} rows):")
            lines.extend("  " + " | ".join(row) for row in table[:8])
    if text:
        lines.append(f"Text ({len(text)} characters, {page.get('scope', 'page')}):")
        lines.append(text[:limit] + (f"\n[... {len(text) - limit} more characters in {base}.md]" if len(text) > limit else ""))
    if page.get("links"):
        lines.append(f"Links ({len(page['links'])}):")
        lines.extend(f"  {label} -> {href}" for label, href in page["links"][:25])
        if len(page["links"]) > 25:
            lines.append(f"  [... {len(page['links']) - 25} more in {base}.md]")
    lines.append(f"Saved: {base}.md")
    if shot:
        lines.append(f"Screenshot: {shot}")
    return "\n".join(lines)


def bt_reply_budget():
    try:
        import forge_utils
        return int(getattr(forge_utils, "TOOL_RESULT_CHARS", 20000))
    except Exception:  # noqa: BLE001
        return 20000


if __name__ == "__main__" and len(sys.argv) == 4 and sys.argv[1] == "--child":
    _request = json.loads(sys.stdin.read())
    try:
        _outcome = _child(_request, sys.argv[3])
    except BaseException as _error:  # noqa: BLE001 - the parent decides what a failure means
        _outcome = {"error": f"{type(_error).__name__}: {_error}"}
    _temp = sys.argv[2] + ".tmp"
    with open(_temp, "w", encoding="utf-8") as _stream:
        json.dump(_outcome, _stream, default=str)
    os.replace(_temp, sys.argv[2])
    sys.exit(0 if "error" not in _outcome else 1)
