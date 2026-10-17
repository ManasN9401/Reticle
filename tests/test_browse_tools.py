"""browse_web: the browser agent's tool.

The sandbox rules are tested without a browser; the browsing itself is tested with the real
Playwright package against a local site, which the tool treats as an allowed origin through a
test-only parameter. Those tests are skipped when Playwright or a Chromium-based browser is missing."""
import sys
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import browser_tools as bt
import browse_tools as bw

HOME = """<!doctype html><title>Shop</title><meta name="description" content="A tiny shop">
<nav>Menu One Two Three</nav>
<main><h1>Widgets</h1><p>%s</p>
<ul id="items"><li class="item"><h2>Alpha</h2><a href="/item/alpha">details</a><span class="price">$5</span></li>
<li class="item"><h2>Beta</h2><a href="/item/beta">details</a><span class="price">$7</span></li></ul>
<table><tr><th>Name</th><th>Stock</th></tr><tr><td>Alpha</td><td>3</td></tr><tr><td>Beta</td><td>0</td></tr></table>
<input name="q" id="q"><button id="go" onclick="document.getElementById('out').textContent='searched: '+document.getElementById('q').value">Search</button>
<div id="out"></div><button id="later" onclick="setTimeout(()=>{const d=document.createElement('div');d.id='late';d.textContent='arrived';document.body.appendChild(d)},300)">Load</button>
<button id="pop" onclick="window.open('/item/alpha')">Popup</button><button id="dlg" onclick="alert('hello')">Alert</button></main>
<script>fetch('/api/ping',{method:'POST',body:'x'}).catch(()=>{}); fetch('http://127.0.0.1:%d/leak',{method:'POST',body:'secret'}).catch(()=>{});</script>""" % ("Welcome. " * 60, 1)

ITEM = '<!doctype html><title>Item</title><main><h1>An item</h1><p>%s</p></main>' % ("Details. " * 80)


class Site(BaseHTTPRequestHandler):
    posts = []
    leaks = []

    def log_message(self, *args):
        pass

    def do_GET(self):
        if self.path == "/":
            return self.reply(200, HOME)
        if self.path.startswith("/item/"):
            return self.reply(200, ITEM)
        if self.path == "/wall":
            return self.reply(403, "<html><body>Access denied. Please verify you are human.</body></html>")
        if self.path == "/slow":
            return self.reply(200, "<script>while(true){}</script>")
        if self.path == "/leak":
            Site.leaks.append("GET")
        self.reply(404, "no")

    def do_POST(self):
        (Site.leaks if self.path == "/leak" else Site.posts).append(self.path)
        self.reply(200, "ok")

    def reply(self, status, body):
        data = body.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


class Sandbox(unittest.TestCase):
    """Rules that need no browser."""

    def test_private_local_and_odd_urls_are_refused_before_anything_starts(self):
        for url in ("http://127.0.0.1:8080/", "http://localhost/", "http://192.168.1.1/", "http://10.0.0.1/x",
                    "file:///etc/passwd", "ftp://example.com/", "javascript:alert(1)", "https://user:pw@example.com/", "", None, 5):
            reply = bw.browse_web(tempfile.gettempdir(), url)
            self.assertTrue(reply.startswith("Error:"), (url, reply))

    def test_action_and_extract_arguments_are_validated(self):
        with patch.object(bt, "_playwright_available", return_value=True):
            self.assertIn("list of objects", bw.browse_web(tempfile.gettempdir(), "https://example.com/", actions="click"))
            self.assertIn("at most 30 actions", bw.browse_web(tempfile.gettempdir(), "https://example.com/", actions=[{"wait_ms": 1}] * 31))
            self.assertIn("extract must be an object", bw.browse_web(tempfile.gettempdir(), "https://example.com/", extract="links"))

    def test_without_playwright_it_says_how_to_get_it(self):
        with patch.object(bt, "_playwright_available", return_value=False):
            self.assertIn("browser.run", bw.browse_web(tempfile.gettempdir(), "https://example.com/"))

    def test_submission_is_possible_only_to_the_site_being_browsed(self):
        self.assertFalse(bt.request_allowed("https://shop.example.com/cart", "POST", -1)[0])
        self.assertTrue(bt.request_allowed("https://shop.example.com/cart", "POST", -1, "example.com")[0])
        self.assertTrue(bt.request_allowed("https://example.com/cart", "POST", -1, "example.com")[0])
        self.assertFalse(bt.request_allowed("https://evil.test/collect", "POST", -1, "example.com")[0])
        self.assertFalse(bt.request_allowed("https://notexample.com/x", "POST", -1, "example.com")[0], "a suffix is not a subdomain")
        self.assertFalse(bt.request_allowed("http://192.168.0.1/", "POST", -1, "192.168.0.1")[0], "a private address stays private")
        self.assertFalse(bt.request_allowed("http://127.0.0.1:9/", "GET", -1, "127.0.0.1")[0])
        self.assertTrue(bt.request_allowed("https://cdn.test/lib.js", "GET", -1)[0])


BROWSER = bt.find_chromium()


@unittest.skipUnless(BROWSER and bt._playwright_available(), "needs Playwright and a Chromium-based browser")
class Browsing(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Site)
        cls.port = cls.server.server_address[1]
        threading.Thread(target=cls.server.serve_forever, daemon=True).start()
        cls.url = f"http://127.0.0.1:{cls.port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        (Path(self.temp.name) / "src").mkdir()
        Site.posts.clear()
        Site.leaks.clear()

    def browse(self, path="/", **options):
        return bw.browse_web(self.temp.name, self.url + path, _local_port=self.port, deadline_seconds=40, **options)

    def saved(self, name):
        return (Path(self.temp.name) / "src" / name).read_text(encoding="utf-8")

    def test_it_reads_the_main_content_headings_links_and_tables(self):
        reply = self.browse(extract={"tables": True})
        self.assertIn("(HTTP 200)", reply)
        self.assertIn("Title: Shop", reply)
        self.assertIn("Description: A tiny shop", reply)
        self.assertIn("Headings: h1 Widgets", reply)
        self.assertIn("main content", reply)
        self.assertNotIn("Menu One Two Three", reply, "navigation around the main content is left out")
        self.assertIn("details -> " + self.url + "/item/alpha", reply)
        self.assertIn("Table 1 (3 rows):", reply)
        self.assertIn("Alpha | 3", reply)
        self.assertIn("Saved: browse/", reply)
        self.assertIn("Welcome.", self.saved(reply.split("Saved: ")[1].split()[0]))

    def test_selectors_pick_out_repeated_structures_and_attributes(self):
        reply = self.browse(extract={"links": False, "selectors": {"names": ".item h2", "prices": ".price", "urls": {"css": ".item a", "attr": "href"}, "bad": "[["}})
        self.assertIn('Selected names (2): ["Alpha", "Beta"]', reply)
        self.assertIn('Selected prices (2): ["$5", "$7"]', reply)
        self.assertIn('["/item/alpha", "/item/beta"]', reply)
        self.assertIn("invalid selector", reply)

    def test_actions_click_fill_wait_and_navigate(self):
        reply = self.browse(actions=[
            {"fill": ["#q", "anvils"]}, {"click": "#go"},
            {"eval": "document.getElementById('out').textContent"},
            {"click": "#later"}, {"wait_for": "#late"}, {"eval": "document.getElementById('late').textContent"},
            {"click": "text=details"}, {"eval": "location.pathname"},
        ])
        self.assertIn('-> "searched: anvils"', reply)
        self.assertIn('-> "arrived"', reply)
        self.assertIn('-> "/item/alpha"', reply)
        self.assertIn("An item", reply)
        self.assertNotIn("Action problems", reply)

    def test_a_failing_action_is_reported_and_the_rest_still_run(self):
        reply = self.browse(actions=[{"click": "#nothing-here"}, {"eval": "1+1"}, {"fly": "away"}])
        self.assertIn("Action problems:", reply)
        self.assertIn("no clickable element matches '#nothing-here'", reply)
        self.assertIn("unknown action", reply)
        self.assertIn("-> 2", reply)

    def test_nothing_is_sent_out_by_default(self):
        reply = self.browse()
        self.assertIn("Blocked by the sandbox", reply)
        self.assertEqual(Site.leaks, [], "the page's POST to another local service must not arrive")

    def test_same_site_posts_need_allow_submit(self):
        self.browse()
        self.assertEqual(Site.posts, [], "a read-only visit sends no POST at all, even to the site being browsed")
        reply = self.browse(allow_submit=True)
        self.assertEqual(Site.posts, ["/api/ping"], "allow_submit lets the page post to its own site")
        self.assertEqual(Site.leaks, [], "...and still not to anything else")
        self.assertIn("Blocked by the sandbox", reply)

    def test_popups_downloads_and_dialogs_are_contained(self):
        reply = self.browse(actions=[{"click": "#pop"}, {"wait_ms": 400}, {"click": "#dlg"}])
        self.assertIn("1 popup window(s) were closed", reply)
        self.assertIn("Dialogs dismissed: alert: hello", reply)

    def test_an_access_wall_is_recognised_and_not_worked_around(self):
        reply = self.browse("/wall")
        self.assertIn("(HTTP 403)", reply)
        self.assertIn("ACCESS WALL", reply)
        self.assertIn("Do not try to get around it", reply)

    def test_screenshots_are_saved_when_asked(self):
        reply = self.browse(screenshot=True)
        name = reply.split("Screenshot: ")[1].split()[0]
        self.assertTrue((Path(self.temp.name) / "src" / name).read_bytes().startswith(b"\x89PNG"))

    def test_a_page_that_never_yields_cannot_outlast_the_deadline(self):
        started = time.monotonic()
        reply = bw.browse_web(self.temp.name, self.url + "/slow", _local_port=self.port, deadline_seconds=15)
        self.assertLess(time.monotonic() - started, 40)
        self.assertTrue(reply.startswith("Error: the page did not finish within") or "time limit" in reply, reply)

    def test_each_visit_gets_its_own_saved_file(self):
        first, second = self.browse(), self.browse()
        self.assertNotEqual(first.split("Saved: ")[1].split()[0], second.split("Saved: ")[1].split()[0])


if __name__ == "__main__":
    unittest.main()
