"""run_page: load a workspace page in a real headless browser and report what happened.

Static checks cannot see what a page does when it runs: a script that throws on its first line,
a texture that 404s, a WebGL canvas that renders nothing, an animation loop that never starts. A
generated site once passed every static check and could not start. This tool loads the page the
way a user's browser would and returns what that browser saw.

Default backend: the Chrome or Edge already installed on the machine, driven over the DevTools
protocol with nothing but the standard library (no package, no browser download). Optional
backend: Playwright, used only when no system browser is found and the package is importable.

The page is LLM-written code, so it is contained: it is served from a loopback server rooted at
the workspace's src folder, the browser profile is thrown away, public subresources may be
fetched (CDN scripts are normal) but nothing may be sent anywhere except the local origin, and
private networks and other local ports are unreachable. Everything is bounded by a deadline and
the browser's process tree is always killed.
"""
import base64
import hashlib
import ipaddress
import json
import mimetypes
import os
import re
import select
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import quote, unquote, urlsplit

from forge_utils import safe_path

DEFAULT_WAIT_MS = 3000
MAX_WAIT_MS = 15000
DEFAULT_DEADLINE_SECONDS = 45
MAX_DEADLINE_SECONDS = 120
MAX_ACTIONS = 20
REPORT_LIMIT = 6000
LIST_LIMIT = 12          # entries listed per category
LINE_LIMIT = 280         # characters kept of one message
_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

PROFILE_PREFIX = "reticle-run-page-"
# A profile older than this belongs to a call that could not delete it (on Windows a killed browser
# tree can hold its files for a few seconds), never to one still running: a call lasts at most
# MAX_DEADLINE_SECONDS.
STALE_PROFILE_SECONDS = 30 * 60

BROWSER_HELP = (
    "No Chromium-based browser was found. Install Google Chrome or Microsoft Edge, or set RETICLE_BROWSER "
    "to the path of a chrome/chromium/msedge executable. (Optional alternative: install the 'playwright' "
    "Python package in this agent's environment.)"
)

_TYPES = {
    ".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8",
    ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
    ".json": "application/json; charset=utf-8", ".map": "application/json; charset=utf-8",
    ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
    ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon",
    ".glb": "model/gltf-binary", ".gltf": "model/gltf+json", ".bin": "application/octet-stream",
    ".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".txt": "text/plain; charset=utf-8",
    ".wasm": "application/wasm", ".mp3": "audio/mpeg", ".wav": "audio/wav", ".mp4": "video/mp4",
}


# ---------------------------------------------------------------------------------------------
# Containment
# ---------------------------------------------------------------------------------------------

_LOOPBACK_NAMES = {"localhost", "localhost.localdomain", "ip6-localhost"}
_dns_cache = {}


def _addresses(host):
    """IP addresses a host name resolves to (cached); [] when it does not resolve."""
    try:
        return [ipaddress.ip_address(host)]
    except ValueError:
        pass
    if host not in _dns_cache:
        try:
            _dns_cache[host] = [ipaddress.ip_address(item[4][0]) for item in socket.getaddrinfo(host, None)]
        except (OSError, ValueError):
            _dns_cache[host] = []
    return _dns_cache[host]


def request_allowed(url, method, origin_port):
    """(allowed, reason) for one request the page makes.

    Allowed: the local origin (any method), data:/blob:/about:, and GET/HEAD/OPTIONS to public
    http(s) hosts, which is what CDN scripts, fonts and textures are. Refused: any other
    method to a remote host (nothing is sent out), any private, link-local or loopback address
    other than the local origin, and every other scheme."""
    parts = urlsplit(url)
    scheme = parts.scheme.lower()
    if scheme in ("data", "blob", "about"):
        return True, ""
    if scheme not in ("http", "https", "ws", "wss"):
        return False, f"scheme {scheme or '(none)'} is not allowed"
    host = (parts.hostname or "").lower()
    if not host:
        return False, "no host"
    port = parts.port or (443 if scheme in ("https", "wss") else 80)
    addresses = _addresses(host)
    is_local = host in _LOOPBACK_NAMES or (addresses and all(a.is_loopback for a in addresses))
    if is_local:
        if port == origin_port and scheme in ("http", "ws"):
            return True, ""
        return False, f"{host}:{port} is another local service"
    if addresses and any(a.is_private or a.is_link_local or a.is_loopback or a.is_reserved or a.is_multicast or a.is_unspecified for a in addresses):
        return False, f"{host} is on a private network"
    if scheme in ("ws", "wss"):
        return False, "web sockets to remote hosts are not allowed"
    if method.upper() not in ("GET", "HEAD", "OPTIONS"):
        return False, f"{method.upper()} to a remote host would send data out"
    return True, ""


# ---------------------------------------------------------------------------------------------
# The site, served read-only from the workspace
# ---------------------------------------------------------------------------------------------

class _SiteHandler(BaseHTTPRequestHandler):
    server_version = "ReticleRunPage"
    protocol_version = "HTTP/1.0"

    def log_message(self, *args):
        pass

    def _resolve(self):
        host = self.headers.get("Host", "")
        port = self.server.server_address[1]
        if host not in (f"127.0.0.1:{port}", f"localhost:{port}"):
            return None
        raw = unquote(urlsplit(self.path).path)
        segments = [s for s in raw.split("/") if s != ""]
        if any(s in (".", "..") or s.startswith(".") or re.search(r"[\\:\0]", s) for s in segments):
            return None
        root = self.server.site_root
        try:
            target = root.joinpath(*segments).resolve()
            target.relative_to(root)
        except (OSError, ValueError):
            return None
        if target.is_dir():
            target = target / "index.html"
        return target if target.is_file() else None

    def _respond(self, head_only):
        target = self._resolve()
        if target is None:
            self.send_response(404)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Content-Length", "9")
            self.end_headers()
            if not head_only:
                self.wfile.write(b"Not found\n")
            return
        data = target.read_bytes()
        kind = _TYPES.get(target.suffix.lower()) or mimetypes.guess_type(str(target))[0] or "application/octet-stream"
        self.send_response(200)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        if not head_only:
            self.wfile.write(data)

    def do_GET(self):
        self._respond(False)

    def do_HEAD(self):
        self._respond(True)


class _Site:
    def __init__(self, root):
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), _SiteHandler)
        self.server.site_root = Path(root).resolve()
        self.server.daemon_threads = True
        self.port = self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, name="reticle-run-page-site", daemon=True)
        self.thread.start()

    def url(self, relative):
        return f"http://127.0.0.1:{self.port}/" + "/".join(quote(part) for part in relative.split("/") if part)

    def close(self):
        try:
            self.server.shutdown()
            self.server.server_close()
        except OSError:
            pass


# ---------------------------------------------------------------------------------------------
# Screenshot statistics (stdlib PNG decoding): is anything on screen?
# ---------------------------------------------------------------------------------------------

def png_stats(data):
    """{"width","height","dominant","distinct"} for an 8-bit, non-interlaced PNG, else None.

    `dominant` is the fraction of pixels with the most common colour; `distinct` counts colours
    after reducing each channel to 5 bits (so noise and gradients do not look like content)."""
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        return None
    position, header, palette, idat = 8, None, None, []
    while position + 8 <= len(data):
        length, kind = struct.unpack(">I4s", data[position:position + 8])
        body = data[position + 8:position + 8 + length]
        position += 12 + length
        if kind == b"IHDR":
            header = struct.unpack(">IIBBBBB", body)
        elif kind == b"PLTE":
            palette = body
        elif kind == b"IDAT":
            idat.append(body)
        elif kind == b"IEND":
            break
    if not header or not idat:
        return None
    width, height, depth, color, _, _, interlace = header
    channels = {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}.get(color)
    if depth != 8 or interlace != 0 or channels is None or width * height > 4_000_000:
        return None
    raw = zlib.decompress(b"".join(idat))
    stride = width * channels
    if len(raw) < height * (stride + 1):
        return None
    previous = bytearray(stride)
    counts = {}
    for row in range(height):
        start = row * (stride + 1)
        filter_type = raw[start]
        line = bytearray(raw[start + 1:start + 1 + stride])
        if filter_type == 1:
            for i in range(channels, stride):
                line[i] = (line[i] + line[i - channels]) & 255
        elif filter_type == 2:
            for i in range(stride):
                line[i] = (line[i] + previous[i]) & 255
        elif filter_type == 3:
            for i in range(stride):
                left = line[i - channels] if i >= channels else 0
                line[i] = (line[i] + ((left + previous[i]) >> 1)) & 255
        elif filter_type == 4:
            for i in range(stride):
                a = line[i - channels] if i >= channels else 0
                b = previous[i]
                c = previous[i - channels] if i >= channels else 0
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                predictor = a if pa <= pb and pa <= pc else (b if pb <= pc else c)
                line[i] = (line[i] + predictor) & 255
        elif filter_type != 0:
            return None
        previous = line
        for x in range(0, width, 2 if width > 400 else 1):
            offset = x * channels
            if color in (2, 6):
                key = (line[offset] >> 3, line[offset + 1] >> 3, line[offset + 2] >> 3)
            elif color == 3 and palette is not None and line[offset] * 3 + 2 < len(palette):
                index = line[offset] * 3
                key = (palette[index] >> 3, palette[index + 1] >> 3, palette[index + 2] >> 3)
            else:
                key = line[offset] >> 3
            counts[key] = counts.get(key, 0) + 1
    total = sum(counts.values())
    if not total:
        return None
    return {"width": width, "height": height, "dominant": max(counts.values()) / total, "distinct": len(counts)}


def blank_verdict(stats):
    if stats is None:
        return "not assessed"
    dominant, distinct = stats["dominant"], stats["distinct"]
    if dominant >= 0.995:
        return f"BLANK: {dominant:.1%} of the screenshot is one colour ({distinct} colours)"
    if dominant >= 0.95:
        return f"mostly empty: {dominant:.1%} is one colour ({distinct} colours)"
    return f"has content: largest colour covers {dominant:.0%}, {distinct} distinct colours"


# ---------------------------------------------------------------------------------------------
# Browser discovery and process control
# ---------------------------------------------------------------------------------------------

def find_chromium():
    explicit = os.environ.get("RETICLE_BROWSER", "").strip()
    if explicit:
        return explicit if os.path.isfile(explicit) else None
    candidates = []
    if sys.platform.startswith("win"):
        for base in filter(None, (os.environ.get("PROGRAMFILES"), os.environ.get("PROGRAMFILES(X86)"), os.environ.get("LOCALAPPDATA"))):
            candidates += [
                os.path.join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
                os.path.join(base, "Google", "Chrome", "Application", "chrome.exe"),
                os.path.join(base, "Chromium", "Application", "chrome.exe"),
            ]
    elif sys.platform == "darwin":
        candidates += [
            "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
            "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
            "/Applications/Chromium.app/Contents/MacOS/Chromium",
        ]
    for candidate in candidates:
        if os.path.isfile(candidate):
            return candidate
    for name in ("google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "microsoft-edge", "msedge"):
        found = shutil.which(name)
        if found:
            return found
    return None


def _playwright_available():
    try:
        import importlib.util
        return importlib.util.find_spec("playwright") is not None
    except (ImportError, ValueError):
        return False


def _backend_forced():
    return os.environ.get("RETICLE_BROWSER_BACKEND", "").strip().lower() in ("cdp", "playwright")


def choose_backend(executable):
    """'playwright', 'cdp' or None. Honors RETICLE_BROWSER_BACKEND.

    Playwright is preferred when its package is importable: it is the more complete and better
    maintained driver (dialogs, frames, popups, actionability waits). The built-in DevTools client
    is the zero-install fallback, so a machine with only Chrome or Edge still works."""
    wanted = os.environ.get("RETICLE_BROWSER_BACKEND", "").strip().lower()
    if wanted == "playwright":
        return "playwright" if _playwright_available() else None
    if wanted == "cdp":
        return "cdp" if executable else None
    if _playwright_available():
        return "playwright"
    return "cdp" if executable else None


def _kill_tree(process):
    if process.poll() is not None:
        return
    try:
        if sys.platform.startswith("win"):
            subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], capture_output=True, timeout=10)
        else:
            import signal
            os.killpg(os.getpgid(process.pid), signal.SIGKILL)
    except (OSError, subprocess.SubprocessError):
        pass
    try:
        process.kill()
    except OSError:
        pass
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        pass


def sweep_stale_profiles(now=None):
    """Delete throwaway browser profiles left behind by earlier calls. Returns how many went."""
    now = time.time() if now is None else now
    removed = 0
    try:
        candidates = list(Path(tempfile.gettempdir()).glob(PROFILE_PREFIX + "*"))
    except OSError:
        return 0
    for entry in candidates:
        try:
            if entry.is_dir() and now - entry.stat().st_mtime > STALE_PROFILE_SECONDS:
                shutil.rmtree(entry, ignore_errors=True)
                removed += 0 if entry.exists() else 1
        except OSError:
            continue
    return removed


def _launch(executable, profile, width, height):
    arguments = [
        executable, "--headless=new", "--remote-debugging-port=0", f"--user-data-dir={profile}",
        "--no-first-run", "--no-default-browser-check", "--disable-extensions", "--disable-sync",
        "--disable-background-networking", "--disable-component-update", "--disable-default-apps",
        "--mute-audio", "--hide-scrollbars", "--disable-dev-shm-usage",
        "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist",
        f"--window-size={width},{height}", "about:blank",
    ]
    if hasattr(os, "geteuid") and os.geteuid() == 0:
        arguments.insert(1, "--no-sandbox")
    options = {"stdout": subprocess.DEVNULL, "stderr": subprocess.DEVNULL, "stdin": subprocess.DEVNULL}
    if sys.platform.startswith("win"):
        options["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0) | getattr(subprocess, "CREATE_NO_WINDOW", 0)
    else:
        options["start_new_session"] = True
    return subprocess.Popen(arguments, **options)


# ---------------------------------------------------------------------------------------------
# WebSocket client (RFC 6455, just enough for the DevTools protocol)
# ---------------------------------------------------------------------------------------------

class WebSocketError(Exception):
    pass


class WebSocket:
    def __init__(self, host, port, path, timeout=10.0):
        self.sock = socket.create_connection((host, port), timeout=timeout)
        try:
            self._handshake(host, port, path, timeout)
        except BaseException:
            self.sock.close()
            raise

    def _handshake(self, host, port, path, timeout):
        self.sock.settimeout(timeout)
        key = base64.b64encode(os.urandom(16)).decode()
        request = (
            f"GET {path} HTTP/1.1\r\nHost: {host}:{port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
        )
        self.sock.sendall(request.encode())
        buffer = b""
        while b"\r\n\r\n" not in buffer:
            chunk = self.sock.recv(4096)
            if not chunk:
                raise WebSocketError("connection closed during the handshake")
            buffer += chunk
            if len(buffer) > 65536:
                raise WebSocketError("handshake response too large")
        header, _, self._buffer = buffer.partition(b"\r\n\r\n")
        lines = header.decode("latin-1").split("\r\n")
        if " 101 " not in lines[0] + " ":
            raise WebSocketError(f"handshake refused: {lines[0]}")
        accept = next((line.split(":", 1)[1].strip() for line in lines[1:] if line.lower().startswith("sec-websocket-accept")), "")
        expected = base64.b64encode(hashlib.sha1((key + _GUID).encode()).digest()).decode()
        if accept != expected:
            raise WebSocketError("handshake accept key mismatch")

    def _frame(self, opcode, payload):
        mask = os.urandom(4)
        length = len(payload)
        head = bytes([0x80 | opcode])
        if length < 126:
            head += bytes([0x80 | length])
        elif length < 65536:
            head += bytes([0x80 | 126]) + struct.pack(">H", length)
        else:
            head += bytes([0x80 | 127]) + struct.pack(">Q", length)
        masked = bytes(byte ^ mask[index % 4] for index, byte in enumerate(payload))
        self.sock.sendall(head + mask + masked)

    def send_text(self, text):
        self._frame(0x1, text.encode("utf-8"))

    def _read(self, count):
        while len(self._buffer) < count:
            chunk = self.sock.recv(max(65536, count - len(self._buffer)))
            if not chunk:
                raise WebSocketError("connection closed")
            self._buffer += chunk
        data, self._buffer = self._buffer[:count], self._buffer[count:]
        return data

    def recv(self, timeout):
        """The next text message, or None if nothing arrives within `timeout` seconds."""
        message = b""
        while True:
            if not self._buffer:
                ready, _, _ = select.select([self.sock], [], [], max(0.0, timeout))
                if not ready:
                    return None
            first, second = self._read(2)
            opcode, length = first & 0x0F, second & 0x7F
            final = bool(first & 0x80)
            if length == 126:
                length = struct.unpack(">H", self._read(2))[0]
            elif length == 127:
                length = struct.unpack(">Q", self._read(8))[0]
            mask = self._read(4) if second & 0x80 else None
            payload = self._read(length) if length else b""
            if mask:
                payload = bytes(byte ^ mask[index % 4] for index, byte in enumerate(payload))
            if opcode == 0x9:
                self._frame(0xA, payload)
                continue
            if opcode == 0xA:
                continue
            if opcode == 0x8:
                raise WebSocketError("connection closed by the browser")
            message += payload
            if final:
                return message.decode("utf-8", errors="replace")
            # a continuation follows; keep reading with the same patience
            timeout = 10.0

    def close(self):
        try:
            self._frame(0x8, b"")
        except OSError:
            pass
        try:
            self.sock.close()
        except OSError:
            pass


# ---------------------------------------------------------------------------------------------
# What the page is asked, and what is collected
# ---------------------------------------------------------------------------------------------

# Installed before any page script runs. It counts animation frames and remembers which context
# each canvas was given, so the report never has to create one (asking an empty canvas for a WebGL
# context would make a page that drew nothing look as if it had a renderer).
_FRAME_COUNTER = """
(() => {
  window.__reticleFrames = 0;
  const raf = window.requestAnimationFrame.bind(window);
  window.requestAnimationFrame = (callback) => raf((time) => { window.__reticleFrames += 1; return callback(time); });
  window.__reticleContexts = new WeakMap();
  const getContext = HTMLCanvasElement.prototype.getContext;
  HTMLCanvasElement.prototype.getContext = function (type, ...rest) {
    const context = getContext.call(this, type, ...rest);
    if (context && !window.__reticleContexts.has(this)) window.__reticleContexts.set(this, { type: String(type), context });
    return context;
  };
})();
"""

_OBSERVE = """
(() => {
  const canvases = Array.from(document.querySelectorAll('canvas')).slice(0, 6).map((canvas) => {
    const entry = window.__reticleContexts && window.__reticleContexts.get(canvas);
    let lost = false;
    try { lost = !!(entry && entry.context.isContextLost && entry.context.isContextLost()); } catch (error) { lost = false; }
    return { width: canvas.width, height: canvas.height, context: entry ? entry.type : 'none', lost };
  });
  return JSON.stringify({
    title: document.title,
    readyState: document.readyState,
    canvases,
    frames: window.__reticleFrames || 0,
    scripts: document.scripts.length,
    text: ((document.body && document.body.innerText) || '').trim().replace(/\\s+/g, ' ').slice(0, 240),
  });
})()
"""

_KEYS = {
    "ArrowLeft": (37, "ArrowLeft"), "ArrowUp": (38, "ArrowUp"), "ArrowRight": (39, "ArrowRight"),
    "ArrowDown": (40, "ArrowDown"), "Enter": (13, "Enter"), "Escape": (27, "Escape"), "Tab": (9, "Tab"),
    "Space": (32, "Space"), " ": (32, "Space"), "Backspace": (8, "Backspace"),
}


def _key_event(name):
    if name in _KEYS:
        code, label = _KEYS[name]
        return {"key": " " if name in ("Space", " ") else name, "code": label, "windowsVirtualKeyCode": code}
    if len(name) == 1 and name.isalnum():
        upper = name.upper()
        return {"key": name, "code": ("Key" + upper) if name.isalpha() else ("Digit" + name), "windowsVirtualKeyCode": ord(upper)}
    raise ValueError(f"unsupported key: {name!r}")


_ERROR_HINTS = {
    "net::ERR_BLOCKED_BY_ORB": "net::ERR_BLOCKED_BY_ORB (the server sent something that is not a script or image, usually an HTML 404: the URL is wrong)",
    "net::ERR_NAME_NOT_RESOLVED": "net::ERR_NAME_NOT_RESOLVED (the host does not exist or there is no network)",
    "net::ERR_CONNECTION_REFUSED": "net::ERR_CONNECTION_REFUSED (nothing is listening there)",
    "net::ERR_ABORTED": "net::ERR_ABORTED (cancelled by the page)",
}


def _explain_error(text):
    return _ERROR_HINTS.get(text, text)


def _new_observation():
    return {
        "console": [], "exceptions": [], "log": [], "failed": [], "http_errors": [], "blocked": [],
        "requests": 0, "navigation_error": "", "loaded": False, "load_ms": None,
        "page": {}, "frames_during_wait": 0, "wait_seconds": 0.0, "eval": [], "action_errors": [],
        "unresponsive": False, "dialogs": [],
    }


def _short(text, limit=LINE_LIMIT):
    text = " ".join(str(text).split())
    return text if len(text) <= limit else text[:limit - 1] + "…"


def _where(url, line):
    if not url:
        return ""
    name = url.rsplit("/", 1)[-1] or url
    return f" ({name}:{int(line) + 1})" if isinstance(line, (int, float)) else f" ({name})"


# ---------------------------------------------------------------------------------------------
# Backend 1: the system browser over the DevTools protocol
# ---------------------------------------------------------------------------------------------

class _Cdp:
    def __init__(self, websocket, origin_port, observation, deadline):
        self.ws = websocket
        self.origin_port = origin_port
        self.obs = observation
        self.deadline = deadline
        self.next_id = 0
        self.session = None
        self.request_urls = {}
        self.seen_log = set()

    def send(self, method, params=None, session=True):
        self.next_id += 1
        message = {"id": self.next_id, "method": method, "params": params or {}}
        if session and self.session:
            message["sessionId"] = self.session
        self.ws.send_text(json.dumps(message))
        return self.next_id

    def call(self, method, params=None, session=True, timeout=10.0):
        # Nothing may run past the overall deadline: a page stuck in a loop never answers, and each
        # unanswered call would otherwise wait out its own timeout one after another.
        left = self.deadline - time.monotonic()
        if left <= 0:
            raise TimeoutError("the time limit was reached")
        timeout = min(timeout, left)
        wanted = self.send(method, params, session)
        deadline = time.monotonic() + timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError(f"{method} did not answer within {timeout:.0f}s")
            raw = self.ws.recv(remaining)
            if raw is None:
                continue
            message = json.loads(raw)
            if message.get("id") == wanted:
                if "error" in message:
                    raise WebSocketError(f"{method}: {message['error'].get('message', message['error'])}")
                return message.get("result", {})
            self.dispatch(message)

    def pump(self, seconds):
        end = min(time.monotonic() + seconds, self.deadline)
        while True:
            remaining = end - time.monotonic()
            if remaining <= 0:
                return
            raw = self.ws.recv(min(remaining, 0.25))
            if raw is not None:
                self.dispatch(json.loads(raw))

    def dispatch(self, message):
        method, params = message.get("method"), message.get("params", {})
        if not method:
            return
        obs = self.obs
        if method == "Fetch.requestPaused":
            request = params.get("request", {})
            allowed, reason = request_allowed(request.get("url", ""), request.get("method", "GET"), self.origin_port)
            if allowed:
                self.send("Fetch.continueRequest", {"requestId": params["requestId"]})
            else:
                obs["blocked"].append(f"{request.get('method', 'GET')} {_short(request.get('url', ''), 160)}: {reason}")
                self.send("Fetch.failRequest", {"requestId": params["requestId"], "errorReason": "BlockedByClient"})
        elif method == "Page.javascriptDialogOpening":
            # alert/confirm/prompt block the page until answered. Nobody is there to answer, and
            # an unanswered dialog looked exactly like an endless loop. Accept it and say so.
            obs["dialogs"].append(_short(f"{params.get('type', 'dialog')}: {params.get('message', '')}", 200))
            self.send("Page.handleJavaScriptDialog", {"accept": True, "promptText": params.get("defaultPrompt", "")})
        elif method == "Runtime.consoleAPICalled":
            parts = []
            for argument in params.get("args", []):
                parts.append(str(argument.get("value", argument.get("description", argument.get("type", "")))))
            frames = (params.get("stackTrace") or {}).get("callFrames") or [{}]
            frame = frames[0]
            obs["console"].append((params.get("type", "log"), _short(" ".join(parts)) + _where(frame.get("url"), frame.get("lineNumber"))))
        elif method == "Runtime.exceptionThrown":
            details = params.get("exceptionDetails", {})
            description = (details.get("exception") or {}).get("description") or details.get("text", "exception")
            obs["exceptions"].append(_short(description.splitlines()[0]) + _where(details.get("url"), details.get("lineNumber")))
        elif method == "Log.entryAdded":
            entry = params.get("entry", {})
            text = _short(entry.get("text", ""))
            key = (entry.get("level"), text, entry.get("url"))
            if "favicon.ico" in (entry.get("url") or "") + text:
                return  # browsers ask for one unprompted; a page without one is not broken
            if key not in self.seen_log and entry.get("level") in ("error", "warning"):
                self.seen_log.add(key)
                obs["log"].append((entry.get("level"), text + _where(entry.get("url"), entry.get("lineNumber"))))
        elif method == "Network.requestWillBeSent":
            obs["requests"] += 1
            self.request_urls[params.get("requestId")] = params.get("request", {}).get("url", "")
        elif method == "Network.responseReceived":
            response = params.get("response", {})
            if response.get("status", 0) >= 400 and not response.get("url", "").endswith("/favicon.ico"):
                obs["http_errors"].append(f"{response.get('status')} {_short(response.get('url', ''), 200)}")
        elif method == "Network.loadingFailed":
            if params.get("blockedReason") != "inspector" and params.get("errorText") != "net::ERR_BLOCKED_BY_CLIENT":
                url = self.request_urls.get(params.get("requestId"), "")
                if params.get("canceled") and not params.get("errorText"):
                    return
                obs["failed"].append(f"{_explain_error(params.get('errorText', 'failed'))} {_short(url, 200)}")
        elif method == "Page.loadEventFired":
            obs["loaded"] = True


def _cdp_session(executable, options, site, observation, deadline):
    """Run the page in the system browser. Returns the full-size screenshot bytes (or b'')."""
    profile = tempfile.mkdtemp(prefix=PROFILE_PREFIX)
    process = None
    websocket = None
    try:
        process = _launch(executable, profile, options["width"], options["height"])
        port_file = Path(profile) / "DevToolsActivePort"
        started = time.monotonic()
        while not port_file.is_file() or len(port_file.read_text().split()) < 2:
            if process.poll() is not None:
                raise WebSocketError("the browser exited before it started listening")
            if time.monotonic() - started > 20:
                raise TimeoutError("the browser did not start within 20 seconds")
            time.sleep(0.1)
        lines = port_file.read_text().split()
        websocket = WebSocket("127.0.0.1", int(lines[0]), lines[1], timeout=10.0)
        cdp = _Cdp(websocket, site.port, observation, deadline)
        target = cdp.call("Target.createTarget", {"url": "about:blank"}, session=False)["targetId"]
        cdp.session = cdp.call("Target.attachToTarget", {"targetId": target, "flatten": True}, session=False)["sessionId"]
        for method, params in (
            ("Page.enable", {}), ("Runtime.enable", {}), ("Log.enable", {}), ("Network.enable", {}),
            ("Fetch.enable", {"patterns": [{"urlPattern": "*"}]}),
            ("Emulation.setDeviceMetricsOverride", {"width": options["width"], "height": options["height"], "deviceScaleFactor": 1, "mobile": False}),
            ("Page.addScriptToEvaluateOnNewDocument", {"source": _FRAME_COUNTER}),
        ):
            cdp.call(method, params)

        navigated = time.monotonic()
        result = cdp.call("Page.navigate", {"url": site.url(options["page"])})
        if result.get("errorText"):
            observation["navigation_error"] = result["errorText"]
        limit = min(20.0, max(1.0, deadline - time.monotonic() - 5))
        while not observation["loaded"] and time.monotonic() - navigated < limit and not observation["navigation_error"]:
            cdp.pump(0.2)
        observation["load_ms"] = int((time.monotonic() - navigated) * 1000) if observation["loaded"] else None

        def evaluate(expression, timeout=10.0):
            try:
                reply = cdp.call("Runtime.evaluate", {"expression": expression, "returnByValue": True, "awaitPromise": True}, timeout=timeout)
            except TimeoutError:
                # The page's own JavaScript is not yielding to the browser: an endless loop.
                observation["unresponsive"] = True
                raise
            if "exceptionDetails" in reply:
                details = reply["exceptionDetails"]
                raise ValueError(_short((details.get("exception") or {}).get("description") or details.get("text", "evaluation failed")))
            return reply.get("result", {}).get("value")

        def frames():
            try:
                return int(evaluate("window.__reticleFrames || 0") or 0)
            except (ValueError, TimeoutError, WebSocketError):
                return 0

        before, window_start = frames(), time.monotonic()
        cdp.pump(min(options["wait_ms"] / 1000.0, max(0.0, deadline - time.monotonic() - 8)))
        observation["wait_seconds"] = time.monotonic() - window_start
        observation["frames_during_wait"] = max(0, frames() - before)

        for action in options["actions"]:
            if time.monotonic() > deadline - 6:
                observation["action_errors"].append("stopped: out of time")
                break
            try:
                _cdp_action(cdp, evaluate, action)
            except (ValueError, TimeoutError, WebSocketError) as error:
                observation["action_errors"].append(f"{json.dumps(action)}: {_short(error)}")
        cdp.pump(0.3)

        try:
            observation["page"] = json.loads(evaluate(_OBSERVE) or "{}")
        except (ValueError, TimeoutError, WebSocketError) as error:
            observation["page"] = {"error": _short(error)}

        full = b""
        if options["screenshot"]:
            try:
                reply = cdp.call("Page.captureScreenshot", {"format": "png"}, timeout=15.0)
                full = base64.b64decode(reply.get("data", ""))
            except (ValueError, TimeoutError, WebSocketError) as error:
                observation["action_errors"].append(f"screenshot failed: {_short(error)}")
        # A small capture is what the blank-screen check reads: decoding 1280x720 in Python is slow.
        try:
            reply = cdp.call("Page.captureScreenshot", {
                "format": "png",
                "clip": {"x": 0, "y": 0, "width": options["width"], "height": options["height"], "scale": 0.2},
            }, timeout=15.0)
            observation["stats"] = png_stats(base64.b64decode(reply.get("data", "")))
        except (ValueError, TimeoutError, WebSocketError, zlib.error):
            observation["stats"] = None
        return full
    finally:
        if websocket is not None:
            try:
                websocket.close()
            except OSError:
                pass
        if process is not None:
            _kill_tree(process)
        for _ in range(15):
            try:
                shutil.rmtree(profile)
                break
            except OSError:
                time.sleep(0.3)


def _cdp_action(cdp, evaluate, action):
    if not isinstance(action, dict) or len(action) < 1:
        raise ValueError("an action is an object such as {\"click\": \"#enter\"}")
    if "click" in action:
        selector = action["click"]
        point = evaluate(
            "(() => { const e = document.querySelector(" + json.dumps(str(selector)) + "); if (!e) return null;"
            "e.scrollIntoView({block:'center'}); const r = e.getBoundingClientRect();"
            "return [r.left + r.width / 2, r.top + r.height / 2]; })()"
        )
        if not point:
            raise ValueError(f"no element matches {selector!r}")
        _mouse_click(cdp, point[0], point[1])
    elif "click_at" in action:
        x, y = action["click_at"]
        _mouse_click(cdp, float(x), float(y))
    elif "press" in action:
        event = _key_event(str(action["press"]))
        cdp.call("Input.dispatchKeyEvent", dict(event, type="keyDown", text=event["key"] if len(event["key"]) == 1 else ""))
        cdp.pump(min(max(int(action.get("hold_ms", 60)), 0), 3000) / 1000.0)
        cdp.call("Input.dispatchKeyEvent", dict(event, type="keyUp"))
    elif "scroll" in action:
        cdp.call("Input.dispatchMouseEvent", {"type": "mouseWheel", "x": 100, "y": 100, "deltaX": 0, "deltaY": float(action["scroll"])})
    elif "wait_ms" in action:
        cdp.pump(min(max(int(action["wait_ms"]), 0), 5000) / 1000.0)
    elif "eval" in action:
        value = evaluate(str(action["eval"]))
        cdp.obs["eval"].append((str(action["eval"]), _short(json.dumps(value, default=str), 400)))
    else:
        raise ValueError("unknown action; use click, click_at, press, scroll, wait_ms or eval")
    cdp.pump(0.15)


def _mouse_click(cdp, x, y):
    for kind in ("mouseMoved", "mousePressed", "mouseReleased"):
        cdp.call("Input.dispatchMouseEvent", {"type": kind, "x": x, "y": y, "button": "left" if kind != "mouseMoved" else "none", "clickCount": 1})


# ---------------------------------------------------------------------------------------------
# Backend 2 (optional): Playwright
# ---------------------------------------------------------------------------------------------

def _playwright_inprocess(executable, options, site_port, page_url, observation, deadline, snapshot=None):
    """The Playwright adapter, run in the calling process. Playwright's calls have no timeout of
    their own (a page stuck in a loop never answers page.evaluate), so production use goes through
    _playwright_session, which runs this in a child process the parent can kill at the deadline.
    `snapshot` is called after each stage so that what was learned survives such a kill."""
    from playwright.sync_api import sync_playwright  # optional dependency
    checkpoint = snapshot or (lambda: None)
    with sync_playwright() as driver:
        launch = {"headless": True, "args": ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"]}
        if executable:
            launch["executable_path"] = executable
        browser = driver.chromium.launch(**launch)
        try:
            context = browser.new_context(viewport={"width": options["width"], "height": options["height"]})

            blocked_urls = set()

            def guard(route):
                request = route.request
                allowed, reason = request_allowed(request.url, request.method, site_port)
                if allowed:
                    route.continue_()
                else:
                    blocked_urls.add(request.url)
                    observation["blocked"].append(f"{request.method} {_short(request.url, 160)}: {reason}")
                    route.abort()

            def console(message):
                # A request this sandbox aborted also surfaces as a console error; it is already listed
                # as blocked, and counting it again would make the page look worse than it is.
                location = message.location or {}
                if location.get("url") in blocked_urls:
                    return
                observation["console"].append((message.type, _short(message.text) + _where(location.get("url"), location.get("lineNumber"))))

            def failed(request):
                if request.url in blocked_urls:
                    return
                observation["failed"].append(f"{_explain_error(str(request.failure or 'failed'))} {_short(request.url, 200)}")

            def page_error(error):
                name, message = getattr(error, "name", ""), getattr(error, "message", "") or str(error)
                text = f"{name}: {message}" if name and name not in message else message
                observation["exceptions"].append(_short((text.splitlines() or ["page error"])[0]))

            context.route("**/*", guard)
            context.add_init_script(_FRAME_COUNTER)
            page = context.new_page()
            page.on("console", console)
            page.on("dialog", lambda dialog: (observation["dialogs"].append(_short(f"{dialog.type}: {dialog.message}", 200)), dialog.accept()))
            page.on("pageerror", page_error)
            page.on("requestfailed", failed)
            page.on("response", lambda response: observation["http_errors"].append(f"{response.status} {_short(response.url, 200)}") if response.status >= 400 else None)
            page.on("request", lambda request: observation.__setitem__("requests", observation["requests"] + 1))
            started = time.monotonic()
            try:
                page.goto(page_url, wait_until="load", timeout=int(max(1.0, min(20.0, deadline - time.monotonic() - 5)) * 1000))
                observation["loaded"] = True
                observation["load_ms"] = int((time.monotonic() - started) * 1000)
            except Exception as error:  # noqa: BLE001 - playwright raises its own hierarchy
                observation["navigation_error"] = _short(error)
            checkpoint()

            def frames():
                try:
                    return int(page.evaluate("window.__reticleFrames || 0") or 0)
                except Exception:  # noqa: BLE001
                    return 0

            before, window_start = frames(), time.monotonic()
            page.wait_for_timeout(int(min(options["wait_ms"], max(0.0, deadline - time.monotonic() - 8) * 1000)))
            observation["wait_seconds"] = time.monotonic() - window_start
            checkpoint()
            observation["frames_during_wait"] = max(0, frames() - before)
            checkpoint()
            for action in options["actions"]:
                if time.monotonic() > deadline - 6:
                    observation["action_errors"].append("stopped: out of time")
                    break
                try:
                    if "click" in action:
                        page.click(str(action["click"]), timeout=3000)
                    elif "click_at" in action:
                        page.mouse.click(float(action["click_at"][0]), float(action["click_at"][1]))
                    elif "press" in action:
                        page.keyboard.down(str(action["press"]))
                        page.wait_for_timeout(min(max(int(action.get("hold_ms", 60)), 0), 3000))
                        page.keyboard.up(str(action["press"]))
                    elif "scroll" in action:
                        page.mouse.wheel(0, float(action["scroll"]))
                    elif "wait_ms" in action:
                        page.wait_for_timeout(min(max(int(action["wait_ms"]), 0), 5000))
                    elif "eval" in action:
                        observation["eval"].append((str(action["eval"]), _short(json.dumps(page.evaluate(str(action["eval"])), default=str), 400)))
                    else:
                        raise ValueError("unknown action; use click, click_at, press, scroll, wait_ms or eval")
                except Exception as error:  # noqa: BLE001
                    text = _short(error)
                    if "click" in action and "Timeout" in text and "waiting for" in text:
                        # Say it the way the other backend does, so a report reads the same either way.
                        text = f"no element matches {str(action['click'])!r} (waited 3 s)"
                    observation["action_errors"].append(f"{json.dumps(action)}: {text}")
            checkpoint()
            try:
                observation["page"] = json.loads(page.evaluate(_OBSERVE) or "{}")
            except Exception as error:  # noqa: BLE001
                observation["page"] = {"error": _short(error)}
            checkpoint()
            full = b""
            try:
                full = page.screenshot(type="png")
            except Exception as error:  # noqa: BLE001
                observation["action_errors"].append(f"screenshot failed: {_short(error)}")
            try:
                observation["stats"] = png_stats(full) if full else None
            except (ValueError, zlib.error):
                observation["stats"] = None
            return full
        finally:
            browser.close()


def _child_command(progress_path, result_path, shot_path):
    return [sys.executable, "-B", str(Path(__file__).resolve()), "--playwright-child", str(progress_path), str(result_path), str(shot_path)]


def _read_json(path):
    try:
        return json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None


def _playwright_session(executable, options, site, observation, deadline):
    """Run the Playwright adapter in a supervised child process.

    The parent enforces the deadline by killing the child's whole process tree (driver and
    browser included), so a page whose JavaScript never yields cannot hang the worker (it ends at
    most three seconds past the deadline). A child also keeps Playwright's event loop out of the
    worker process. Whatever the child had checkpointed is reported, marked unresponsive."""
    budget = max(8.0, deadline - time.monotonic())
    request = {
        "executable": executable, "options": options, "site_port": site.port,
        "page_url": site.url(options["page"]), "budget": budget,
    }
    folder = tempfile.mkdtemp(prefix=PROFILE_PREFIX)
    try:
        progress, result, shot = (Path(folder) / name for name in ("progress.json", "result.json", "shot.png"))
        launch = {"stdin": subprocess.PIPE, "stdout": subprocess.DEVNULL, "stderr": subprocess.PIPE}
        if sys.platform.startswith("win"):
            launch["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0) | getattr(subprocess, "CREATE_NO_WINDOW", 0)
        else:
            launch["start_new_session"] = True
        process = subprocess.Popen(_child_command(progress, result, shot), **launch)
        timed_out, stderr = False, b""
        try:
            # The child works to `budget` itself; the extra seconds let one that is just finishing
            # (closing the browser, writing the screenshot) do so before it is treated as hung.
            _, stderr = process.communicate(json.dumps(request).encode("utf-8"), timeout=budget + 3.0)
        except subprocess.TimeoutExpired:
            timed_out = True
            _kill_tree(process)
            try:
                _, stderr = process.communicate(timeout=5)
            except (subprocess.TimeoutExpired, OSError, ValueError):
                stderr = b""
        finished = _read_json(result)
        if finished and finished.get("error"):
            raise RuntimeError(finished["error"])
        data = finished["observation"] if finished and "observation" in finished else _read_json(progress)
        if data is None:
            tail = stderr.decode("utf-8", errors="replace").strip().splitlines()[-1:] or ["no output"]
            raise RuntimeError(f"the Playwright process produced no result ({tail[0][:200]})")
        observation.update(data)
        if timed_out:
            observation["unresponsive"] = True
        return shot.read_bytes() if shot.is_file() else b""
    finally:
        for _ in range(15):
            try:
                shutil.rmtree(folder)
                break
            except OSError:
                time.sleep(0.3)


def _playwright_child(progress_path, result_path, shot_path):
    """Entry point of the supervised child (python browser_tools.py --playwright-child ...)."""
    request = json.loads(sys.stdin.read())
    observation = _new_observation()

    def write(path, payload):
        temporary = str(path) + ".tmp"
        with open(temporary, "w", encoding="utf-8") as stream:
            json.dump(payload, stream, default=str)
        os.replace(temporary, path)

    try:
        full = _playwright_inprocess(
            request["executable"], request["options"], request["site_port"], request["page_url"],
            observation, time.monotonic() + float(request["budget"]), lambda: write(progress_path, observation),
        )
        if full:
            Path(shot_path).write_bytes(full)
        write(result_path, {"observation": observation})
        return 0
    except BaseException as error:  # noqa: BLE001 - the parent decides what a failure means
        write(result_path, {"error": _short(f"{type(error).__name__}: {error}", 400)})
        return 1


# ---------------------------------------------------------------------------------------------
# The report
# ---------------------------------------------------------------------------------------------

def _listing(title, items, formatter=lambda item: item):
    if not items:
        return []
    unique = list(dict.fromkeys(formatter(item) for item in items))
    lines = [f"{title} ({len(unique)}):"]
    lines += [f"  - {item}" for item in unique[:LIST_LIMIT]]
    if len(unique) > LIST_LIMIT:
        lines.append(f"  - ... and {len(unique) - LIST_LIMIT} more")
    return lines


def format_report(page_name, observation, screenshot_path, backend):
    obs = observation
    page = obs.get("page") or {}
    problems = (
        len(obs["exceptions"]) + len(obs["failed"]) + len(obs["http_errors"])
        + sum(1 for kind, _ in obs["console"] if kind in ("error", "assert"))
        + (1 if obs["navigation_error"] or not obs["loaded"] else 0)
        + (1 if obs["unresponsive"] else 0)
    )
    stats = obs.get("stats")
    blank = stats is not None and stats["dominant"] >= 0.995
    lines = [f"Ran {page_name} in a headless browser ({backend}); viewport as requested, waited {obs['wait_seconds']:.1f}s."]
    if obs["navigation_error"]:
        lines.append(f"LOAD FAILED: {obs['navigation_error']}")
    elif not obs["loaded"]:
        lines.append("LOAD INCOMPLETE: the load event did not fire in time (a script or request is hanging).")
    else:
        lines.append(f"Loaded in {obs['load_ms']} ms; {obs['requests']} requests.")
    if page.get("title") is not None and not page.get("error"):
        lines.append(f"Title: {page.get('title') or '(empty)'}")
    if obs["unresponsive"]:
        lines.append("PAGE UNRESPONSIVE: its JavaScript stopped yielding to the browser (an endless loop or a very long synchronous task); later steps were skipped.")
    lines += _listing("UNCAUGHT EXCEPTIONS", obs["exceptions"])
    errors = [text for kind, text in obs["console"] if kind in ("error", "assert")]
    warnings = [text for kind, text in obs["console"] if kind == "warning"]
    lines += _listing("Console errors", errors)
    lines += _listing("FAILED REQUESTS", obs["failed"])
    lines += _listing("HTTP errors", obs["http_errors"])
    lines += _listing("Browser log", [f"{level}: {text}" for level, text in obs["log"]])
    lines += _listing("Dialogs the page opened (accepted automatically; a visitor would have to dismiss each one)", obs["dialogs"])
    lines += _listing("Console warnings", warnings)
    lines += _listing("Blocked by the sandbox (not a page bug unless the page needs it)", obs["blocked"])
    canvases = page.get("canvases") or []
    if canvases:
        lines.append("Canvases: " + "; ".join(
            f"{c['width']}x{c['height']} "
            + ("no context was ever requested (nothing draws to it)" if c["context"] == "none" else c["context"])
            + (" CONTEXT LOST" if c.get("lost") else "")
            for c in canvases))
    elif page and not page.get("error"):
        lines.append("Canvases: none")
    seconds = max(obs["wait_seconds"], 0.001)
    if obs["loaded"]:
        if obs["frames_during_wait"] == 0:
            lines.append("Animation: NO requestAnimationFrame callbacks ran during the wait (nothing is animating; a render loop may not have started).")
        else:
            lines.append(f"Animation: {obs['frames_during_wait']} animation frames in {seconds:.1f}s (~{obs['frames_during_wait'] / seconds:.0f}/s; software rendering is slower than a GPU).")
    lines.append(f"Screenshot content: {blank_verdict(stats)}")
    if page.get("text"):
        lines.append(f"Visible text: {_short(page['text'], 200)}")
    for expression, value in obs["eval"]:
        lines.append(f"eval {_short(expression, 80)} -> {value}")
    lines += _listing("Action problems", obs["action_errors"])
    if screenshot_path:
        lines.append(f"Screenshot: {screenshot_path}")
    verdict = "NO PROBLEMS FOUND" if problems == 0 and not blank else f"{problems} problem(s)" + (", screen is blank" if blank else "")
    lines.append(f"Result: {verdict}.")
    report = "\n".join(lines)
    return report if len(report) <= REPORT_LIMIT else report[:REPORT_LIMIT - 40] + "\n... (report truncated)"


# ---------------------------------------------------------------------------------------------
# The tool
# ---------------------------------------------------------------------------------------------

def _clamped_int(value, default, low, high):
    try:
        return max(low, min(high, int(value)))
    except (TypeError, ValueError):
        return default


def _screenshot_target(workspace_dir, page_name):
    """A workspace-relative path in run_page/ that does not exist yet."""
    stem = re.sub(r"[^A-Za-z0-9_-]+", "-", Path(page_name).stem).strip("-") or "page"
    for number in range(1, 1000):
        relative = f"run_page/{stem}-{number}.png"
        if not safe_path(workspace_dir, relative).exists():
            return relative
    raise ValueError("run_page/ holds too many screenshots; delete some")


def run_page(workspace_dir, path=None, wait_ms=DEFAULT_WAIT_MS, viewport=None, screenshot=True, actions=None, deadline_seconds=None):
    """Load a workspace page in a headless browser; return a report of what happened."""
    import web_tools
    root = safe_path(workspace_dir, ".")
    if path:
        page = Path(str(path).lstrip("/"))
        if not (root / page).is_file():
            return f"Error: {path} does not exist in the workspace."
    else:
        pages = [p for p in web_tools._find_pages(root, None) if p.is_file()]
        if not pages:
            return "Error: no .html page was found in the workspace to run."
        page = pages[0].relative_to(root)
    page_name = page.as_posix()

    viewport = viewport if isinstance(viewport, dict) else {}
    options = {
        "page": page_name,
        "wait_ms": _clamped_int(wait_ms, DEFAULT_WAIT_MS, 0, MAX_WAIT_MS),
        "width": _clamped_int(viewport.get("width"), 1280, 200, 3840),
        "height": _clamped_int(viewport.get("height"), 720, 200, 2160),
        "screenshot": bool(screenshot),
        "actions": [],
    }
    if actions is not None:
        if not isinstance(actions, list):
            return "Error: actions must be a list of objects such as {\"click\": \"#enter\"}."
        if len(actions) > MAX_ACTIONS:
            return f"Error: at most {MAX_ACTIONS} actions per run_page call."
        options["actions"] = actions
    budget = _clamped_int(deadline_seconds, DEFAULT_DEADLINE_SECONDS, 5, MAX_DEADLINE_SECONDS)
    deadline = time.monotonic() + budget

    executable = find_chromium()
    backend = choose_backend(executable)
    if backend is None:
        return "Error: " + BROWSER_HELP

    sweep_stale_profiles()
    observation = _new_observation()
    label = "playwright"
    site = _Site(root)
    try:
        try:
            if backend == "cdp":
                label = "system browser"
                image = _cdp_session(executable, options, site, observation, deadline)
            else:
                try:
                    image = _playwright_session(executable, options, site, observation, deadline)
                except Exception as error:  # noqa: BLE001 - playwright raises its own hierarchy
                    # Playwright was only the preferred engine. If it cannot start (its own browser
                    # is not installed, a driver problem) and a system browser exists, use that.
                    if _backend_forced() or not executable:
                        raise
                    observation = _new_observation()
                    label = f"system browser; Playwright was unavailable: {_short(error, 120)}"
                    image = _cdp_session(executable, options, site, observation, deadline)
        except Exception as error:  # noqa: BLE001 - a browser failure is reported, not raised
            return f"Error: the browser could not run the page ({backend}): {_short(error, 400)}"
    finally:
        site.close()

    shot = ""
    if image:
        target = _screenshot_target(workspace_dir, page_name)
        destination = safe_path(workspace_dir, target)
        destination.parent.mkdir(parents=True, exist_ok=True)
        with open(destination, "xb") as stream:
            stream.write(image)
        shot = target
    return format_report(page_name, observation, shot, label)


if __name__ == "__main__" and len(sys.argv) == 5 and sys.argv[1] == "--playwright-child":
    sys.exit(_playwright_child(sys.argv[2], sys.argv[3], sys.argv[4]))
