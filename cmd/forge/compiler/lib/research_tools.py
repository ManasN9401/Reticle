"""web_search and fetch_pages: the open-web research tools of the OSINT agent.

Everything here reads the public web and keeps what it read:

* web_search returns ranked results (title, URL, snippet) from one provider and logs the query.
* fetch_pages downloads several pages in parallel, politely (robots.txt, a per-host limit, an
  honest User-Agent), turns each into readable text, and saves every page under research/sources/
  with its URL, retrieval time and a SHA-256 of what was downloaded. research/sources.json indexes
  them so a report can cite "S7" and a reader can check it.

Only public destinations are fetched: the address a name resolves to is checked, the connection is
made to that checked address (so DNS cannot change between the check and the connect), and every
redirect is checked again. Pages are read, never submitted to.
"""
import hashlib
import html
import http.client
import ipaddress
import json
import os
import re
import socket
import ssl
import threading
import time
import urllib.parse
import urllib.robotparser
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from html.parser import HTMLParser

from forge_utils import safe_path

USER_AGENT = "Mozilla/5.0 (compatible; ReticleResearch/1.0; +https://github.com/ManasN9401/Reticle)"
MAX_BODY = 3 * 1024 * 1024
MAX_URLS = 20
MAX_WORKERS = 6
PER_HOST = 2
REQUEST_TIMEOUT = 15
CALL_DEADLINE = 90
MAX_REDIRECTS = 5
MAX_SAVED_CHARS = 200_000
SEARCH_LIMIT_MAX = 20
TEXT_TYPES = ("text/html", "application/xhtml", "text/plain", "text/markdown", "application/json", "text/xml", "application/xml", "application/rss", "application/atom", "text/csv")

# Allowed to be a different value from the default 20,000 (see forge_utils.TOOL_RESULT_CHARS); the
# worker sets that from the model's window, and the replies below are sized from it.
def _reply_budget():
    try:
        import forge_utils
        return int(getattr(forge_utils, "TOOL_RESULT_CHARS", 20000))
    except Exception:  # noqa: BLE001
        return 20000


# ---------------------------------------------------------------------------------------------
# Public fetching
# ---------------------------------------------------------------------------------------------

class FetchError(Exception):
    pass


def _check_public(host, port):
    """The addresses a host resolves to, or FetchError if any is not on the public internet."""
    try:
        infos = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
    except OSError as error:
        raise FetchError(f"{host} could not be resolved ({error})") from None
    if not infos:
        raise FetchError(f"{host} did not resolve")
    for info in infos:
        if not ipaddress.ip_address(info[4][0]).is_global:
            raise FetchError(f"{host} is not on the public internet")
    return infos


def _open(url, headers, allow_private=False):
    """One HTTPS/HTTP GET to a checked address. Returns (status, response headers, body bytes, url)."""
    parts = urllib.parse.urlsplit(url)
    if parts.scheme not in ("http", "https") or not parts.hostname or parts.username or parts.password:
        raise FetchError("only plain http(s) URLs without credentials can be fetched")
    port = parts.port or (443 if parts.scheme == "https" else 80)
    if allow_private:
        infos = socket.getaddrinfo(parts.hostname, port, type=socket.SOCK_STREAM)
    else:
        infos = _check_public(parts.hostname, port)
    address = infos[0][4][:2]
    connection = http.client.HTTPConnection(parts.hostname, port, timeout=REQUEST_TIMEOUT)

    def connect():
        sock = socket.create_connection(address, timeout=REQUEST_TIMEOUT)
        if parts.scheme == "https":
            sock = ssl.create_default_context().wrap_socket(sock, server_hostname=parts.hostname)
        connection.sock = sock
    connection.connect = connect
    try:
        target = urllib.parse.urlunsplit(("", "", parts.path or "/", parts.query, ""))
        connection.request("GET", target, headers=dict({"User-Agent": USER_AGENT, "Accept": "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.5", "Accept-Language": "en", "Accept-Encoding": "identity"}, **headers))
        response = connection.getresponse()
        body = response.read(MAX_BODY + 1)
        return response.status, response, body[:MAX_BODY], len(body) > MAX_BODY
    finally:
        connection.close()


def fetch_url(url, headers=None, allow_private=False):
    """GET a URL following up to MAX_REDIRECTS redirects, each re-checked.
    Returns a dict: status, final_url, content_type, body (bytes), truncated."""
    headers = headers or {}
    for _ in range(MAX_REDIRECTS + 1):
        status, response, body, truncated = _open(url, headers, allow_private)
        if status in (301, 302, 303, 307, 308):
            location = response.getheader("Location")
            if not location:
                raise FetchError(f"HTTP {status} without a Location")
            url = urllib.parse.urljoin(url, location)
            continue
        return {
            "status": status, "final_url": url, "body": body, "truncated": truncated,
            "content_type": (response.getheader("Content-Type") or "").split(";")[0].strip().lower(),
            "charset": _charset(response.getheader("Content-Type") or ""),
        }
    raise FetchError("too many redirects")


def _charset(content_type):
    match = re.search(r"charset=([A-Za-z0-9_\-]+)", content_type, re.I)
    return match.group(1) if match else ""


def _decode(body, charset, html_head=""):
    guess = charset
    if not guess:
        match = re.search(rb"<meta[^>]+charset=[\"']?([A-Za-z0-9_\-]+)", body[:4096], re.I)
        guess = match.group(1).decode("ascii", "ignore") if match else "utf-8"
    try:
        return body.decode(guess, errors="replace")
    except LookupError:
        return body.decode("utf-8", errors="replace")


_robots = {}
_robots_lock = threading.Lock()


def _robots_allows(url):
    parts = urllib.parse.urlsplit(url)
    origin = f"{parts.scheme}://{parts.netloc}"
    with _robots_lock:
        parser = _robots.get(origin)
    if parser is None:
        parser = urllib.robotparser.RobotFileParser()
        try:
            result = fetch_url(origin + "/robots.txt")
            if result["status"] >= 500:
                parser.disallow_all = False
                parser.allow_all = True
            elif result["status"] >= 400:
                parser.allow_all = True
            else:
                parser.parse(_decode(result["body"], result["charset"]).splitlines())
        except (FetchError, OSError):
            parser.allow_all = True
        with _robots_lock:
            _robots[origin] = parser
    return parser.can_fetch(USER_AGENT, url) and parser.can_fetch("*", url)


# ---------------------------------------------------------------------------------------------
# HTML to readable text
# ---------------------------------------------------------------------------------------------

_SKIP = {"script", "style", "noscript", "svg", "template", "iframe", "canvas", "head"}
# A blank line separates paragraphs; list items, table rows and plain containers only break the line.
_PARAGRAPH = {"p", "h1", "h2", "h3", "h4", "h5", "h6", "pre", "blockquote", "table", "ul", "ol", "section", "article"}
_LINE = {"div", "main", "header", "footer", "li", "tr", "br", "dd", "dt", "figcaption"}
def _region_priority(tag, attrs):
    """How specifically an element marks the page's own content: the article body beats <article>
    beats a general <main>, which on many sites also holds the title bar and language menu."""
    if attrs.get("id") in ("mw-content-text", "main-content", "article-body"):
        return 3
    if tag == "article":
        return 2
    if tag == "main" or attrs.get("role") == "main":
        return 1
    return 0




class _Readable(HTMLParser):
    def __init__(self, base_url):
        super().__init__(convert_charrefs=True)
        self.base = base_url
        self.title = ""
        self.meta = {}
        self.parts = []
        self.main_parts = []
        self.links = []
        self.headings = []
        self.dates = []
        self._skip = 0
        self._in_title = False
        self._heading = None
        self._link = None
        self._main = None  # [tag, depth] while inside the first main-content region
        self._main_done = False
        self._newlines = 2  # the start of the text counts as a paragraph break
        self._pre = 0

    def _emit(self, text):
        self.parts.append(text)
        if self._main is not None:
            self.main_parts.append(text)

    def _break(self, tag):
        """A line or paragraph break that merges with the one before it instead of stacking."""
        need = 2 if tag in _PARAGRAPH else 1 if tag in _LINE else 0
        if need and self._newlines < need:
            self._emit("\n" * (need - self._newlines))
            self._newlines = need

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "title":
            self._in_title = True
        if tag == "meta":
            key = (attrs.get("name") or attrs.get("property") or attrs.get("itemprop") or "").lower()
            if key and attrs.get("content"):
                self.meta.setdefault(key, attrs["content"].strip())
        if tag == "time" and attrs.get("datetime"):
            self.dates.append(attrs["datetime"].strip())
        if tag in _SKIP and tag != "head":
            self._skip += 1
        if self._skip:
            return
        if tag == "pre":
            self._pre += 1
        priority = _region_priority(tag, attrs)
        if priority and not self._main_done and (self._main is None or priority > self._main[2]):
            # A more specific region than the one being captured: keep only that one.
            self._main, self.main_parts = [tag, 1, priority], []
        elif self._main is not None and tag == self._main[0]:
            self._main[1] += 1
        self._break(tag)
        if tag in ("h1", "h2", "h3"):
            self._heading = [tag, []]
        if tag == "a" and attrs.get("href"):
            self._link = [attrs["href"], []]

    def handle_endtag(self, tag):
        if tag == "title":
            self._in_title = False
        if tag in _SKIP and tag != "head" and self._skip:
            self._skip -= 1
        if self._skip:
            return
        if tag == "pre" and self._pre:
            self._pre -= 1
        self._break(tag)
        if tag in ("td", "th"):
            self._emit(" ")  # cells of a row must not run together
        if self._main is not None and tag == self._main[0]:
            self._main[1] -= 1
            if self._main[1] == 0:
                self._main, self._main_done = None, True
        if self._heading and tag == self._heading[0]:
            text = " ".join("".join(self._heading[1]).split())
            if text:
                self.headings.append((tag, text))
            self._heading = None
        if tag == "a" and self._link:
            text = " ".join("".join(self._link[1]).split())
            href = urllib.parse.urljoin(self.base, self._link[0])
            if href.startswith(("http://", "https://")) and text:
                self.links.append((text[:120], href.split("#")[0]))
            self._link = None

    def handle_data(self, data):
        if self._in_title:
            self.title += data
        if self._skip:
            return
        if not self._pre:
            data = re.sub(r"\s+", " ", data)
        if data.strip():
            self._newlines = 0
        self._emit(data)
        if self._heading is not None:
            self._heading[1].append(data)
        if self._link is not None:
            self._link[1].append(data)


def _tidy(raw):
    """Collapse whitespace; keep at most one blank line, and none at the ends."""
    out, blank = [], False
    for line in (" ".join(line.split()) for line in raw.split("\n")):
        if not line:
            blank = bool(out)
            continue
        if blank:
            out.append("")
            blank = False
        out.append(line)
    return "\n".join(out).strip()


def extract_text(markup, base_url=""):
    """(title, description, published, text, links) from HTML. The text is the page's main content
    when it has a clearly marked one, otherwise the whole body."""
    reader = _Readable(base_url)
    try:
        reader.feed(markup)
        reader.close()
    except Exception:  # noqa: BLE001 - malformed markup is common; keep what was read
        pass
    whole, main = _tidy("".join(reader.parts)), _tidy("".join(reader.main_parts))
    text = main if len(main) >= 300 and len(main) >= len(whole) * 0.2 else whole
    title = " ".join(html.unescape(reader.title).split()) or reader.meta.get("og:title", "")
    description = reader.meta.get("description") or reader.meta.get("og:description", "")
    published = next((reader.meta[key] for key in ("article:published_time", "og:published_time", "date", "datepublished", "pubdate", "dc.date") if key in reader.meta), "") or (reader.dates[0] if reader.dates else "")
    seen, links = set(), []
    for label, href in reader.links:
        if href not in seen:
            seen.add(href)
            links.append((label, href))
    return title.strip(), description.strip(), published.strip(), text, links


def _readable(result):
    """(title, description, published, text, links) for any fetched body."""
    kind = result["content_type"]
    if not kind or kind.startswith(TEXT_TYPES):
        text = _decode(result["body"], result["charset"])
        if kind in ("", "text/html") or kind.startswith("application/xhtml") or text.lstrip()[:200].lower().startswith(("<!doctype html", "<html")):
            return extract_text(text, result["final_url"])
        if kind == "application/json":
            try:
                text = json.dumps(json.loads(text), indent=1, ensure_ascii=False)
            except ValueError:
                pass
        return "", "", "", text.strip(), []
    if kind == "application/pdf":
        try:
            import io
            from pypdf import PdfReader  # optional
            reader = PdfReader(io.BytesIO(result["body"]))
            return (reader.metadata.title if reader.metadata and reader.metadata.title else "", "", "", "\n\n".join((page.extract_text() or "") for page in reader.pages[:60]).strip(), [])
        except Exception:  # noqa: BLE001
            raise FetchError("this is a PDF and no PDF reader is installed (pypdf)") from None
    raise FetchError(f"{kind or 'unknown'} is not a text document")


# ---------------------------------------------------------------------------------------------
# fetch_pages
# ---------------------------------------------------------------------------------------------

_host_limits = {}
_host_lock = threading.Lock()


def _host_semaphore(host):
    with _host_lock:
        return _host_limits.setdefault(host, threading.BoundedSemaphore(PER_HOST))


def _slug(text, limit=48):
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")[:limit] or "page"


def _fetch_one(url, respect_robots):
    parts = urllib.parse.urlsplit(url)
    if parts.scheme not in ("http", "https") or not parts.hostname:
        raise FetchError("not an http(s) URL")
    with _host_semaphore(parts.hostname):
        if respect_robots and not _robots_allows(url):
            raise FetchError("robots.txt asks automated clients not to fetch this page")
        result = fetch_url(url)
    if result["status"] >= 400:
        raise FetchError(f"HTTP {result['status']}")
    title, description, published, text, links = _readable(result)
    return result, title, description, published, text, links


def _load_index(workspace_dir):
    path = safe_path(workspace_dir, "research/sources.json")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        return path, data if isinstance(data, list) else []
    except (OSError, ValueError):
        return path, []


def fetch_pages(urls, workspace_dir, max_chars_each=None, respect_robots=True):
    """Download several public pages in parallel, save each as a source, and summarise them."""
    if isinstance(urls, str):
        urls = [urls]
    if not isinstance(urls, list) or not urls or not all(isinstance(u, str) and u.strip() for u in urls):
        return "Error: urls must be a non-empty list of http(s) URLs."
    seen, unique = set(), []
    for url in urls:
        url = url.strip()
        if url not in seen:
            seen.add(url)
            unique.append(url)
    if len(unique) > MAX_URLS:
        return f"Error: at most {MAX_URLS} URLs per call (got {len(unique)}); split the list."
    budget = _reply_budget()
    each = int(max_chars_each) if isinstance(max_chars_each, (int, float)) and max_chars_each > 0 else 4000
    each = max(300, min(each, max(300, budget // len(unique) - 450)))

    started = time.monotonic()
    outcomes = {}

    def work(url):
        if time.monotonic() - started > CALL_DEADLINE:
            raise FetchError("the time limit for this call was reached before this page was started")
        return _fetch_one(url, respect_robots)

    with ThreadPoolExecutor(max_workers=min(MAX_WORKERS, len(unique))) as pool:
        futures = {url: pool.submit(work, url) for url in unique}
        for url, future in futures.items():
            try:
                outcomes[url] = future.result(timeout=max(1, CALL_DEADLINE + 30 - (time.monotonic() - started)))
            except FetchError as error:
                outcomes[url] = error
            except Exception as error:  # noqa: BLE001 - one bad page must not lose the others
                outcomes[url] = FetchError(f"{type(error).__name__}: {error}")

    index_path, index = _load_index(workspace_dir)
    next_id = max([int(str(item.get("id", "S0"))[1:] or 0) for item in index if str(item.get("id", "")).startswith("S")] + [0]) + 1
    retrieved = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    lines, saved, failed = [], 0, 0
    for url in unique:
        outcome = outcomes[url]
        if isinstance(outcome, FetchError):
            failed += 1
            lines.append(f"FAILED {url}\n  {outcome}")
            continue
        result, title, description, published, text, links = outcome
        digest = hashlib.sha256(result["body"]).hexdigest()
        existing = next((item for item in index if item.get("sha256") == digest and item.get("url") == url), None)
        source_id = existing["id"] if existing else f"S{next_id}"
        relative = existing["path"] if existing else f"research/sources/{source_id}-{_slug(urllib.parse.urlsplit(result['final_url']).hostname or 'page')}-{_slug(title or urllib.parse.urlsplit(result['final_url']).path)}.md"
        if not existing:
            next_id += 1
            target = safe_path(workspace_dir, relative)
            target.parent.mkdir(parents=True, exist_ok=True)
            front = (
                f"---\nid: {source_id}\nurl: {url}\nfinal_url: {result['final_url']}\nretrieved_at: {retrieved}\n"
                f"http_status: {result['status']}\ncontent_type: {result['content_type'] or 'unknown'}\n"
                f"sha256: {digest}\ntitle: {json.dumps(title, ensure_ascii=False)}\n"
                f"published: {json.dumps(published)}\ntruncated: {str(result['truncated']).lower()}\n---\n\n"
            )
            target.write_text(front + (f"# {title}\n\n" if title else "") + text[:MAX_SAVED_CHARS], encoding="utf-8", newline="\n")
            index.append({"id": source_id, "url": url, "final_url": result["final_url"], "title": title, "retrieved_at": retrieved,
                          "published": published, "sha256": digest, "path": relative, "chars": len(text)})
        saved += 1
        shown = text[:each]
        more = f" [{len(text) - each} more characters in {relative}]" if len(text) > each else ""
        header = f"{source_id} {result['final_url']}" + (f"  (redirected from {url})" if result["final_url"] != url else "")
        meta = [f"title: {title}" if title else "", f"published: {published}" if published else "",
                f"description: {description[:200]}" if description else "", f"saved: {relative}"]
        lines.append(header + "\n  " + "\n  ".join(m for m in meta if m) + "\n" + "\n".join("  | " + l for l in shown.splitlines()[:200]) + more)
    try:
        index_path.parent.mkdir(parents=True, exist_ok=True)
        index_path.write_text(json.dumps(index, indent=1, ensure_ascii=False), encoding="utf-8", newline="\n")
    except OSError as error:
        lines.append(f"(could not update research/sources.json: {error})")
    head = f"Fetched {saved} of {len(unique)} page(s)" + (f", {failed} failed" if failed else "") + ". Cite sources by id (S#); the index is research/sources.json.\n"
    return head + "\n\n".join(lines)


# ---------------------------------------------------------------------------------------------
# web_search
# ---------------------------------------------------------------------------------------------

def _search_json(url, headers, parse, body=None):
    headers = dict(headers)
    if body is not None:
        # POST (Serper): http.client directly; the destination is a fixed public API host.
        parts = urllib.parse.urlsplit(url)
        connection = http.client.HTTPSConnection(parts.hostname, timeout=REQUEST_TIMEOUT)
        try:
            connection.request("POST", parts.path, body=json.dumps(body), headers=dict({"Content-Type": "application/json", "User-Agent": USER_AGENT}, **headers))
            response = connection.getresponse()
            payload = response.read(MAX_BODY)
            status = response.status
        finally:
            connection.close()
    else:
        result = fetch_url(url, headers)
        payload, status = result["body"], result["status"]
    if status >= 400:
        raise FetchError(f"the search API answered HTTP {status}")
    return parse(json.loads(payload.decode("utf-8", errors="replace")))


def parse_serper(data, limit):
    return [{"title": r.get("title", ""), "url": r.get("link", ""), "snippet": r.get("snippet", "")} for r in data.get("organic", [])][:limit]


def parse_brave(data, limit):
    return [{"title": r.get("title", ""), "url": r.get("url", ""), "snippet": html.unescape(re.sub(r"<[^>]+>", "", r.get("description", "")))}
            for r in (data.get("web") or {}).get("results", [])][:limit]


def parse_searxng(data, limit):
    return [{"title": r.get("title", ""), "url": r.get("url", ""), "snippet": r.get("content", "")} for r in data.get("results", [])][:limit]


def parse_wikipedia(data, limit):
    found = []
    for item in (data.get("query") or {}).get("search", []):
        title = item.get("title", "")
        found.append({"title": title + " - Wikipedia", "url": "https://en.wikipedia.org/wiki/" + urllib.parse.quote(title.replace(" ", "_")),
                      "snippet": html.unescape(re.sub(r"<[^>]+>", "", item.get("snippet", "")))})
    return found[:limit]


def _search_serper(query, limit, env):
    """Google results through the Serper API (SERPER_API_KEY)."""
    return _search_json("https://google.serper.dev/search", {"X-API-KEY": env.get("SERPER_API_KEY")}, lambda data: parse_serper(data, limit), body={"q": query, "num": limit})


def _search_brave(query, limit, env):
    url = "https://api.search.brave.com/res/v1/web/search?" + urllib.parse.urlencode({"q": query, "count": limit})
    return _search_json(url, {"X-Subscription-Token": env.get("BRAVE_API_KEY"), "Accept": "application/json"}, lambda data: parse_brave(data, limit))


def _search_searxng(query, limit, env):
    base = env.get("SEARXNG_URL", "").rstrip("/")
    result = fetch_url(base + "/search?" + urllib.parse.urlencode({"q": query, "format": "json"}), allow_private=True)
    if result["status"] >= 400:
        raise FetchError(f"SearXNG answered HTTP {result['status']}")
    return parse_searxng(json.loads(result["body"].decode("utf-8", errors="replace")), limit)


def _search_wikipedia(query, limit, env):
    """Wikipedia's own search API: keyless, stable and meant for programmatic use. Narrower than the web."""
    url = "https://en.wikipedia.org/w/api.php?" + urllib.parse.urlencode({"action": "query", "list": "search", "srsearch": query, "srlimit": limit, "format": "json", "utf8": 1})
    return _search_json(url, {}, lambda data: parse_wikipedia(data, limit))


PROVIDERS = {"serper": _search_serper, "brave": _search_brave, "searxng": _search_searxng, "wikipedia": _search_wikipedia}
NEEDS = {"serper": "SERPER_API_KEY", "brave": "BRAVE_API_KEY", "searxng": "SEARXNG_URL"}


def choose_provider(requested, env):
    """The provider to use: the one asked for, else RETICLE_SEARCH_PROVIDER, else the first with
    credentials (Google through Serper, then Brave, then a SearXNG server). With none configured this
    is an error that says what to set: a consumer search page is never scraped, and a narrower
    source (Wikipedia) is used only when asked for by name, so the agent knows what it is searching."""
    name = (requested or env.get("RETICLE_SEARCH_PROVIDER") or "").strip().lower()
    if name:
        if name not in PROVIDERS:
            raise FetchError(f"unknown search provider {name!r}; use one of {', '.join(sorted(PROVIDERS))}")
        if name in NEEDS and not env.get(NEEDS[name]):
            raise FetchError(f"the {name} provider needs {NEEDS[name]} in the environment")
        return name
    for name in ("serper", "brave", "searxng"):
        if env.get(NEEDS[name]):
            return name
    raise FetchError("no search provider is configured. Set SERPER_API_KEY (Google results), BRAVE_API_KEY or SEARXNG_URL for general web "
                     "search, or pass provider=\"wikipedia\" for encyclopedic questions, which needs no key")


def web_search(query, workspace_dir, limit=8, provider=None, env=None):
    env = os.environ if env is None else env
    try:
        if not isinstance(query, str) or not query.strip():
            raise FetchError("query must be non-empty text")
        count = max(1, min(int(limit) if isinstance(limit, (int, float)) and not isinstance(limit, bool) else 8, SEARCH_LIMIT_MAX))
        name = choose_provider(provider, env)
        results = PROVIDERS[name](query.strip(), count, env)
    except FetchError as error:
        return f"Error: search failed: {error}"
    except (ValueError, OSError, http.client.HTTPException) as error:
        return f"Error: search failed: {type(error).__name__}: {error}"
    try:
        log = safe_path(workspace_dir, "research/search-log.jsonl")
        log.parent.mkdir(parents=True, exist_ok=True)
        with open(log, "a", encoding="utf-8", newline="\n") as stream:
            stream.write(json.dumps({"at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"), "provider": name, "query": query.strip(), "urls": [r["url"] for r in results]}, ensure_ascii=False) + "\n")
    except OSError:
        pass
    if not results:
        return f"No results from {name} for {query.strip()!r}. Try different words, fewer quoted terms, or another provider."
    lines = [f"{len(results)} result(s) from {name} for {query.strip()!r} (logged in research/search-log.jsonl):"]
    for number, item in enumerate(results, 1):
        lines.append(f"{number}. {item['title']}\n   {item['url']}" + (f"\n   {item['snippet'][:300]}" if item["snippet"] else ""))
    return "\n".join(lines)
