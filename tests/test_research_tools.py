"""web_search and fetch_pages: the OSINT agent's tools.

The network is not touched: pages come from a local server (with the public-address check lifted
only where a test needs loopback), and search results from saved responses of each provider's API."""
import json
import socket
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import research_tools as rt

PAGE = """<!doctype html><html lang="en"><head><meta charset="utf-8"><title>  Acme   Corp &amp; Sons </title>
<meta name="description" content="Acme makes anvils.">
<meta property="article:published_time" content="2026-03-04T10:00:00Z">
<style>.x{color:red}</style><script>var secret = 'do not show';</script></head>
<body><nav>Home | About</nav><h1>Acme Corp</h1><p>Founded in 1949.</p><p>Based in <a href="/about">Springfield</a> and
<a href="https://other.example/x#frag">elsewhere</a>.</p><noscript>enable js</noscript><ul><li>Anvils</li><li>Rockets</li></ul>
<a href="mailto:a@b.c">mail</a><a href="/about">Springfield again</a></body></html>"""

SERPER = {"organic": [{"title": "Installation | Playwright Python", "link": "https://playwright.dev/python/docs/intro", "snippet": "Playwright was created to accommodate end-to-end testing."},
                      {"title": "playwright · PyPI", "link": "https://pypi.org/project/playwright/", "snippet": "A Python library to automate Chromium."}, {"title": "Third", "link": "https://c.test/", "snippet": ""}]}
BRAVE = {"web": {"results": [{"title": "Brave One", "url": "https://one.test/", "description": "A <strong>bold</strong> &amp; plain description"}, {"title": "Brave Two", "url": "https://two.test/", "description": ""}]}}
SEARXNG = {"results": [{"title": "Sx One", "url": "https://sx.test/1", "content": "found it"}, {"title": "Sx Two", "url": "https://sx.test/2"}]}
WIKI = {"query": {"search": [{"title": "Playwright (software)", "snippet": "<span class=\"searchmatch\">Playwright</span> is an automation library &amp; more"}]}}


class Site(BaseHTTPRequestHandler):
    hits = []

    def log_message(self, *args):
        pass

    def do_GET(self):
        Site.hits.append(self.path)
        routes = {
            "/robots.txt": (200, "text/plain", "User-agent: *\nDisallow: /private\n"),
            "/page": (200, "text/html; charset=utf-8", PAGE),
            "/about": (200, "text/html", "<title>About</title><p>About us</p>"),
            "/private/report": (200, "text/html", "<title>Secret</title><p>no</p>"),
            "/data.json": (200, "application/json", '{"b":2,"a":[1,2]}'),
            "/binary": (200, "application/octet-stream", "\x00\x01"),
            "/missing": (404, "text/html", "<p>gone</p>"),
        }
        if self.path == "/redirect-private":
            self.send_response(302)
            self.send_header("Location", f"http://127.0.0.1:{self.server.private_port}/page")
            self.end_headers()
            return
        status, kind, body = routes.get(self.path, (404, "text/plain", "nope"))
        data = body.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def serve():
    server = ThreadingHTTPServer(("127.0.0.1", 0), Site)
    server.private_port = 0
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def allow_loopback(host, port):
    return socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)


class Parsing(unittest.TestCase):
    def test_readable_text_title_description_date_and_links(self):
        title, description, published, text, links = rt.extract_text(PAGE, "http://site.test/page")
        self.assertEqual(title, "Acme Corp & Sons")
        self.assertEqual(description, "Acme makes anvils.")
        self.assertEqual(published, "2026-03-04T10:00:00Z")
        self.assertIn("Founded in 1949.", text)
        self.assertIn("Anvils", text)
        self.assertNotIn("do not show", text)
        self.assertNotIn("color:red", text)
        self.assertNotIn("enable js", text)
        self.assertEqual(links, [("Springfield", "http://site.test/about"), ("elsewhere", "https://other.example/x")])

    def test_the_main_content_wins_over_the_navigation_around_it(self):
        nav = "".join(f"<li><a href='/n{n}'>Menu item {n}</a></li>" for n in range(60))
        article = "<p>" + "The anvil was invented in the Bronze Age. " * 12 + "</p><p>Second paragraph.</p>"
        markup = f"<html><body><header><ul>{nav}</ul></header><article>{article}</article><footer>Terms Privacy</footer></body></html>"
        _, _, _, text, _ = rt.extract_text(markup)
        self.assertTrue(text.startswith("The anvil was invented"), text[:80])
        self.assertNotIn("Menu item", text)
        self.assertNotIn("Terms Privacy", text)
        self.assertIn("Second paragraph.", text)

    def test_a_page_without_clear_main_content_keeps_all_of_its_text(self):
        _, _, _, text, _ = rt.extract_text("<body><div>Short note</div><div>Another line</div></body>")
        self.assertEqual(text, "Short note\nAnother line")

    def test_table_cells_do_not_run_together(self):
        _, _, _, text, _ = rt.extract_text("<body><table><tr><th>Developer</th><td>Microsoft</td></tr><tr><th>License</th><td>Apache</td></tr></table></body>")
        self.assertEqual(text.splitlines(), ["Developer Microsoft", "License Apache"])

    def test_blocks_do_not_double_space_the_text(self):
        _, _, _, text, _ = rt.extract_text("<body><div>a</div><div>b</div><ul><li>c</li><li>d</li></ul><p>e</p><p>f</p></body>")
        self.assertEqual(text, "a\nb\n\nc\nd\n\ne\n\nf")

    def test_each_providers_response_becomes_title_url_and_snippet(self):
        serper = rt.parse_serper(SERPER, 2)
        self.assertEqual([r["url"] for r in serper], ["https://playwright.dev/python/docs/intro", "https://pypi.org/project/playwright/"])
        self.assertEqual(serper[0]["snippet"], "Playwright was created to accommodate end-to-end testing.")
        brave = rt.parse_brave(BRAVE, 5)
        self.assertEqual(brave[0], {"title": "Brave One", "url": "https://one.test/", "snippet": "A bold & plain description"})
        self.assertEqual(rt.parse_searxng(SEARXNG, 5)[1]["snippet"], "")
        wiki = rt.parse_wikipedia(WIKI, 5)
        self.assertEqual(wiki[0]["url"], "https://en.wikipedia.org/wiki/Playwright_%28software%29")
        self.assertEqual(wiki[0]["snippet"], "Playwright is an automation library & more")

    def test_a_consumer_search_page_is_never_a_provider(self):
        self.assertEqual(sorted(rt.PROVIDERS), ["brave", "searxng", "serper", "wikipedia"])

    def test_provider_choice(self):
        self.assertEqual(rt.choose_provider(None, {"BRAVE_API_KEY": "k"}), "brave")
        self.assertEqual(rt.choose_provider(None, {"BRAVE_API_KEY": "k", "SERPER_API_KEY": "s"}), "serper")
        self.assertEqual(rt.choose_provider(None, {"SEARXNG_URL": "http://x"}), "searxng")
        self.assertEqual(rt.choose_provider("wikipedia", {}), "wikipedia")
        self.assertEqual(rt.choose_provider(None, {"RETICLE_SEARCH_PROVIDER": "wikipedia"}), "wikipedia")
        with self.assertRaisesRegex(rt.FetchError, "needs SERPER_API_KEY"):
            rt.choose_provider("serper", {})
        with self.assertRaisesRegex(rt.FetchError, "unknown search provider"):
            rt.choose_provider("altavista", {})

    def test_with_nothing_configured_it_says_what_to_set_and_does_not_fall_back_silently(self):
        with self.assertRaisesRegex(rt.FetchError, r"no search provider is configured.*SERPER_API_KEY.*BRAVE_API_KEY.*SEARXNG_URL.*wikipedia"):
            rt.choose_provider(None, {})


class Searching(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        (Path(self.temp.name) / "src").mkdir()

    def test_results_are_listed_and_every_query_is_logged(self):
        fake = lambda query, limit, env: [{"title": "One", "url": "https://a.test/1", "snippet": "first"}, {"title": "Two", "url": "https://b.test/2", "snippet": ""}][:limit]
        with patch.dict(rt.PROVIDERS, {"serper": fake}):
            reply = rt.web_search("acme anvils", self.temp.name, limit=5, env={"SERPER_API_KEY": "k"})
            rt.web_search("second query", self.temp.name, env={"SERPER_API_KEY": "k"})
        self.assertIn("2 result(s) from serper", reply)
        self.assertIn("1. One\n   https://a.test/1\n   first", reply)
        log = [json.loads(line) for line in (Path(self.temp.name) / "src/research/search-log.jsonl").read_text(encoding="utf-8").splitlines()]
        self.assertEqual([entry["query"] for entry in log], ["acme anvils", "second query"])
        self.assertEqual(log[0]["urls"], ["https://a.test/1", "https://b.test/2"])

    def test_no_configuration_is_an_error_the_agent_can_act_on(self):
        reply = rt.web_search("x", self.temp.name, env={})
        self.assertTrue(reply.startswith("Error: search failed: no search provider is configured"), reply)

    def test_a_provider_that_fails_says_why(self):
        def refuse(query, limit, env):
            raise rt.FetchError("the search API answered HTTP 429")
        with patch.dict(rt.PROVIDERS, {"brave": refuse}):
            reply = rt.web_search("x", self.temp.name, env={"BRAVE_API_KEY": "k"})
        self.assertEqual(reply, "Error: search failed: the search API answered HTTP 429")

    def test_bad_arguments(self):
        self.assertIn("non-empty", rt.web_search("  ", self.temp.name, env={}))
        self.assertIn("unknown search provider", rt.web_search("x", self.temp.name, provider="nope", env={}))

    def test_no_results_is_said_not_returned_empty(self):
        with patch.dict(rt.PROVIDERS, {"wikipedia": lambda q, l, e: []}):
            self.assertIn("No results", rt.web_search("zzz", self.temp.name, provider="wikipedia", env={}))


class Fetching(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = serve()
        cls.base = f"http://127.0.0.1:{cls.server.server_address[1]}"
        cls.other = serve()
        cls.server.private_port = cls.other.server_address[1]

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.other.shutdown()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        (Path(self.temp.name) / "src").mkdir()
        rt._robots.clear()
        Site.hits.clear()
        self.public = patch.object(rt, "_check_public", side_effect=allow_loopback)

    def research(self, *parts):
        return Path(self.temp.name, "src", "research", *parts)

    def test_private_and_local_addresses_are_refused_by_default(self):
        reply = rt.fetch_pages([self.base + "/page", "http://10.0.0.5/", "http://localhost:1/"], self.temp.name)
        self.assertIn("Fetched 0 of 3", reply)
        self.assertEqual(reply.count("is not on the public internet"), 3)
        self.assertEqual(Site.hits, [])

    def test_pages_are_read_saved_indexed_and_summarised(self):
        with self.public:
            reply = rt.fetch_pages([self.base + "/page", self.base + "/data.json", self.base + "/missing", self.base + "/binary"], self.temp.name, max_chars_each=200)
        self.assertIn("Fetched 2 of 4 page(s), 2 failed", reply)
        self.assertIn("S1 " + self.base + "/page", reply)
        self.assertIn("title: Acme Corp & Sons", reply)
        self.assertIn("published: 2026-03-04T10:00:00Z", reply)
        self.assertIn("HTTP 404", reply)
        self.assertIn("application/octet-stream is not a text document", reply)
        index = json.loads(self.research("sources.json").read_text(encoding="utf-8"))
        self.assertEqual([item["id"] for item in index], ["S1", "S2"])
        saved = (Path(self.temp.name) / "src" / index[0]["path"]).read_text(encoding="utf-8")
        self.assertIn(f"sha256: {index[0]['sha256']}", saved)
        self.assertIn("retrieved_at: 20", saved)
        self.assertIn("Founded in 1949.", saved)
        self.assertEqual(json.loads((Path(self.temp.name) / "src" / index[1]["path"]).read_text(encoding="utf-8").split("---\n\n", 1)[1]), {"b": 2, "a": [1, 2]})

    def test_fetching_the_same_unchanged_page_again_keeps_its_id(self):
        with self.public:
            rt.fetch_pages([self.base + "/page"], self.temp.name)
            reply = rt.fetch_pages([self.base + "/page", self.base + "/about"], self.temp.name)
        index = json.loads(self.research("sources.json").read_text(encoding="utf-8"))
        self.assertEqual([item["id"] for item in index], ["S1", "S2"])
        self.assertIn("S1 " + self.base + "/page", reply)
        self.assertEqual(len(list(self.research("sources").iterdir())), 2)

    def test_robots_txt_is_respected_and_said_so(self):
        with self.public:
            reply = rt.fetch_pages([self.base + "/private/report", self.base + "/page"], self.temp.name)
        self.assertIn("robots.txt asks automated clients not to fetch this page", reply)
        self.assertNotIn("/private/report", [h for h in Site.hits if h != "/robots.txt"])
        with self.public:
            override = rt.fetch_pages([self.base + "/private/report"], self.temp.name, respect_robots=False)
        self.assertIn("Fetched 1 of 1", override)

    def test_a_redirect_to_a_private_address_is_refused_at_the_hop(self):
        private_port = self.server.private_port

        def only_the_first_server(host, port, **kwargs):
            if port == private_port:
                raise rt.FetchError(f"{host} is not on the public internet")
            return allow_loopback(host, port)
        with patch.object(rt, "_check_public", side_effect=only_the_first_server):
            reply = rt.fetch_pages([self.base + "/redirect-private"], self.temp.name, respect_robots=False)
        self.assertIn("not on the public internet", reply)
        self.assertIn("Fetched 0 of 1", reply)

    def test_many_pages_are_fetched_in_parallel_and_each_is_cited(self):
        with self.public:
            urls = [self.base + "/page", self.base + "/about", self.base + "/data.json"] * 3
            reply = rt.fetch_pages(urls, self.temp.name)
        self.assertIn("Fetched 3 of 3", reply, "duplicate URLs are fetched once")
        self.assertEqual(len(json.loads(self.research("sources.json").read_text(encoding="utf-8"))), 3)

    def test_the_reply_is_sized_for_the_context_it_goes_into(self):
        with self.public, patch.object(rt, "_reply_budget", return_value=2400):
            reply = rt.fetch_pages([self.base + "/page"] * 1 + [self.base + "/about", self.base + "/data.json"], self.temp.name, max_chars_each=100000)
        self.assertLess(len(reply), 3200)

    def test_argument_errors(self):
        self.assertIn("non-empty list", rt.fetch_pages([], self.temp.name))
        self.assertIn("at most 20", rt.fetch_pages([f"https://a.test/{n}" for n in range(21)], self.temp.name))
        self.assertIn("Fetched 0 of 1", rt.fetch_pages(["ftp://a.test/file"], self.temp.name))
        self.assertIn("Fetched 0 of 1", rt.fetch_pages(["https://user:pw@a.test/"], self.temp.name))


if __name__ == "__main__":
    unittest.main()
