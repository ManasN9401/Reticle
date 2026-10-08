"""Static checks for a generated web page.

Generated sites have repeatedly "finished" while being dead on arrival: a script
that uses `import` but is loaded as a classic <script>, so the browser stops at
its first line; bare module imports ('three') with no import map; images that
exist on disk but are only referenced from the dead script. Nothing ran the page,
so nothing noticed. This module reads the HTML and the scripts it loads and
reports what a browser would choke on, with no browser and no dependencies
(an installed `node` is used as an extra syntax check when present).

Paths are relative to the session's src folder, as for the other file tools.
"""
import json
import re
import shutil
import subprocess
import tempfile
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote

MAX_SCRIPT_BYTES = 2 * 1024 * 1024
MAX_PAGES = 5
MAX_MODULE_FILES = 25
SKIP_DIRS = {"node_modules", ".git", ".rag", "__pycache__", "dist", "build"}
IMAGE_SUFFIXES = {".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg"}
REMOTE_PREFIXES = ("http:", "https:", "//", "data:", "blob:", "mailto:", "tel:", "javascript:", "#")
CLASSIC_TYPES = {"", "text/javascript", "application/javascript", "text/ecmascript"}

_IMPORT = re.compile(r"""(?m)^\s*import\s+(?:[\w*${][^;\n]*?\s+from\s+)?["'][^"']+["']""")
_EXPORT = re.compile(r"(?m)^\s*export\s+(?:default\b|const\b|let\b|var\b|function\b|class\b|async\b|\{|\*)")
_SPECIFIER = re.compile(r"""(?m)^\s*(?:import|export)\s[^;\n]*?\bfrom\s+["']([^"']+)["']|^\s*import\s+["']([^"']+)["']""")
_CANVAS_OPTION = re.compile(r"""canvas\s*:\s*document\.(?:getElementById|querySelector)\(\s*["']#?([\w-]+)["']\s*\)""")
_ID_LOOKUP = re.compile(r"""getElementById\(\s*["']([\w-]+)["']\s*\)""")


class _Page(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.scripts = []       # {"src", "type", "inline"}
        self.resources = []     # (tag, attribute, value)
        self.ids = {}           # id -> tag
        self.import_map = {}
        self._script = None

    def handle_starttag(self, tag, attrs):
        attributes = {key: (value or "") for key, value in attrs}
        if "id" in attributes:
            self.ids.setdefault(attributes["id"], tag)
        if tag == "script":
            self._script = {"src": attributes.get("src"), "type": attributes.get("type", "").strip().lower(), "inline": ""}
            self.scripts.append(self._script)
        elif tag == "link" and "stylesheet" in attributes.get("rel", "").lower().split() and attributes.get("href"):
            self.resources.append((tag, "href", attributes["href"]))
        elif tag in ("img", "source", "video", "audio") and attributes.get("src"):
            self.resources.append((tag, "src", attributes["src"]))

    def handle_data(self, data):
        if self._script is not None:
            self._script["inline"] += data

    def handle_endtag(self, tag):
        if tag == "script" and self._script is not None:
            if self._script["type"] == "importmap":
                try:
                    imports = json.loads(self._script["inline"]).get("imports", {})
                    if isinstance(imports, dict):
                        self.import_map.update(imports)
                except (ValueError, AttributeError):
                    pass
            self._script = None


def _is_local(reference):
    value = reference.strip()
    return bool(value) and not value.lower().startswith(REMOTE_PREFIXES)


def _resolve(root, page_dir, reference):
    cleaned = unquote(reference.split("#", 1)[0].split("?", 1)[0])
    base = root if cleaned.startswith("/") else page_dir
    target = (base / cleaned.lstrip("/")).resolve()
    try:
        target.relative_to(root.resolve())
    except ValueError:
        return None
    return target


def _read(path):
    try:
        if path.is_file() and path.stat().st_size <= MAX_SCRIPT_BYTES:
            return path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        pass
    return None


def _bare(specifier):
    return not specifier.startswith((".", "/", "http:", "https:", "data:"))


def _mapped(specifier, import_map):
    return specifier in import_map or any(key.endswith("/") and specifier.startswith(key) for key in import_map)


def _specifiers(source):
    return [first or second for first, second in _SPECIFIER.findall(source)]


def _node_check(source, module):
    """A syntax error from an installed node, or None. A classic script is parsed as CommonJS
    so an `import` statement is reported the way a browser's classic script would."""
    node = shutil.which("node")
    if not node:
        return None
    with tempfile.TemporaryDirectory() as folder:
        target = Path(folder) / ("script.mjs" if module else "script.cjs")
        target.write_text(source, encoding="utf-8")
        try:
            result = subprocess.run([node, "--check", str(target)], capture_output=True, text=True, timeout=10)
        except (OSError, subprocess.TimeoutExpired):
            return None
    if result.returncode == 0:
        return None
    lines = [line for line in result.stderr.splitlines() if "Error" in line]
    return (lines[0] if lines else result.stderr.strip().splitlines()[-1] if result.stderr.strip() else "syntax error")[:200]


def _check_page(root, page):
    blocking, warnings = [], []
    page_dir = page.parent
    label = page.relative_to(root).as_posix()
    source = _read(page)
    if source is None:
        return [f"{label} cannot be read"], []
    parsed = _Page()
    parsed.feed(source)

    for tag, attribute, reference in parsed.resources:
        if _is_local(reference):
            target = _resolve(root, page_dir, reference)
            if target is None or not target.is_file():
                blocking.append(f"{label}: <{tag} {attribute}=\"{reference}\"> points at a file that does not exist")

    scripts_text = []
    for script in parsed.scripts:
        module = script["type"] == "module"
        if not module and script["type"] not in CLASSIC_TYPES:
            continue  # importmap, JSON data blocks and the like
        if script["src"]:
            if not _is_local(script["src"]):
                continue
            target = _resolve(root, page_dir, script["src"])
            text = _read(target) if target is not None else None
            if text is None:
                blocking.append(f"{label}: <script src=\"{script['src']}\"> points at a file that does not exist")
                continue
            name = target.relative_to(root).as_posix()
        else:
            text, name = script["inline"], f"the inline script in {label}"
            if not text.strip():
                continue
        scripts_text.append(text)

        if not module:
            if _IMPORT.search(text) or _EXPORT.search(text):
                blocking.append(
                    f"{name} uses import/export but {label} loads it as a classic script, so the browser stops with "
                    "'Cannot use import statement outside a module' and nothing in it runs. Either load it with "
                    "<script type=\"module\"> plus an import map, or drop the import lines and use libraries loaded "
                    "from a CDN as globals (for example THREE from a <script> tag)."
                )
                continue
            problem = _node_check(text, module=False)
            if problem:
                blocking.append(f"{name} has a syntax error: {problem}")
            continue

        seen, queue = set(), [(name, text, (target.parent if script["src"] else page_dir))]
        while queue and len(seen) < MAX_MODULE_FILES:
            module_name, module_text, module_dir = queue.pop(0)
            if module_name in seen:
                continue
            seen.add(module_name)
            problem = _node_check(module_text, module=True)
            if problem:
                blocking.append(f"{module_name} has a syntax error: {problem}")
            for specifier in _specifiers(module_text):
                if _bare(specifier):
                    if not _mapped(specifier, parsed.import_map):
                        blocking.append(
                            f"{module_name} imports '{specifier}' by bare name, which a browser cannot resolve without an "
                            f"import map entry in {label}. Add one, or import it from a full URL."
                        )
                elif _is_local(specifier):
                    child = _resolve(root, module_dir, specifier)
                    child_text = _read(child) if child is not None else None
                    if child_text is None:
                        blocking.append(f"{module_name} imports '{specifier}' which does not exist")
                    else:
                        queue.append((child.relative_to(root).as_posix(), child_text, child.parent))
            scripts_text.append(module_text)

    for text in scripts_text:
        for element_id in _CANVAS_OPTION.findall(text):
            tag = parsed.ids.get(element_id)
            if tag is not None and tag != "canvas":
                blocking.append(
                    f"a script passes #{element_id} as the renderer's canvas, but {label} defines it as a <{tag}>; "
                    "it must be a <canvas> element"
                )
        for element_id in _ID_LOOKUP.findall(text):
            if element_id not in parsed.ids and element_id not in " ".join(warnings):
                warnings.append(f"a script looks up #{element_id}, which {label} does not define")
    return list(dict.fromkeys(blocking)), warnings[:10]


def _find_pages(root, path):
    if path:
        return [root / path.lstrip("/")]
    pages = []
    for candidate in sorted(root.rglob("*.html")):
        parts = candidate.relative_to(root).parts
        if not any(part in SKIP_DIRS for part in parts) and len(parts) <= 4:
            pages.append(candidate)
    return pages[:MAX_PAGES]


def _unused_images(root, pages):
    texts = []
    for candidate in root.rglob("*"):
        if candidate.suffix.lower() in {".html", ".htm", ".js", ".mjs", ".css"} and not any(part in SKIP_DIRS for part in candidate.relative_to(root).parts):
            text = _read(candidate)
            if text:
                texts.append(text)
    joined = "\n".join(texts)
    unused = []
    for candidate in sorted(root.rglob("*")):
        if candidate.suffix.lower() in IMAGE_SUFFIXES and not any(part in SKIP_DIRS for part in candidate.relative_to(root).parts):
            if candidate.name not in joined:
                unused.append(candidate.relative_to(root).as_posix())
    return unused[:10]


def run_check(workspace_dir, path=None):
    """Return (report, blocking_count). A page is fine to finish only with zero blocking problems."""
    root = Path(workspace_dir) / "src"
    pages = _find_pages(root, path)
    pages = [page for page in pages if page.is_file()]
    if not pages:
        where = f"'{path}'" if path else "the workspace"
        return f"No .html file was found in {where}; there is no page to check.", 0
    blocking, warnings, checked = [], [], []
    for page in pages:
        page_blocking, page_warnings = _check_page(root, page)
        blocking += page_blocking
        warnings += page_warnings
        checked.append(page.relative_to(root).as_posix())
    for image in _unused_images(root, pages):
        warnings.append(f"{image} exists but no page, script or stylesheet references it")
    lines = [f"Checked {', '.join(checked)}."]
    if blocking:
        lines.append(f"PROBLEMS ({len(blocking)}) - fix every one, then run check_web_page again:")
        lines += [f"  {index}. {problem}" for index, problem in enumerate(blocking, 1)]
    else:
        lines.append("No blocking problems found.")
    if warnings:
        lines.append("Warnings (not blocking):")
        lines += [f"  - {warning}" for warning in dict.fromkeys(warnings)]
    return "\n".join(lines), len(blocking)


def check_web_page(workspace_dir: str, path: str = None) -> str:
    """Statically check a generated page and the scripts it loads, as a browser would load them."""
    return run_check(workspace_dir, path)[0]
