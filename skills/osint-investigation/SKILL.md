---
name: osint-investigation
description: Open-source research that can be checked: search the public web, read sources in parallel, cross-check claims and report with citations.
---

# OSINT investigation

You gather and assess information from public sources. Your tools are `web_search` (find pages) and `fetch_pages` (read up to 20 pages in parallel and keep them). Every page you read is saved under `research/sources/` and indexed in `research/sources.json` as S1, S2, ... with its URL, retrieval time and a hash. A reader must be able to check any claim in your report against one of those sources.

## Scope (decide this before searching)

- **Passive and public only.** Public web pages, public records, published documents, archives. Nothing behind a login, paywall or access control; no scanning, probing or testing of systems; no leaked or breached data; no credentials, ever.
- **Organisations, topics, products, domains, events and the public roles of public figures** are normal subjects. For a **private individual**, proceed only if the task states a legitimate purpose (for example due diligence on a business counterpart) and keep to professional, public information. Do not compile home addresses, phone numbers, family details, movements, or anything that would let someone be found or harassed. If the task reads as locating or profiling a private person, stop and say what you would need to continue.
- Say what you will and will not do before you start when a request is near these lines.

## Method

1. **Questions first.** Write the 3-6 concrete questions the report must answer. Add them to your notes file.
2. **Search broadly, then narrowly.** Vary wording; use quotes for exact phrases, `site:` to target a domain, `filetype:pdf` for documents. Run several different queries before concluding anything is not online.
3. **Shortlist by source quality**: primary sources (filings, official sites, original documents) before news, news before blogs, forums last. Prefer pages with a date.
4. **Read in batches:** `fetch_pages` with the shortlist (up to 20). The reply shows the start of each page; use `read_file` on the saved source for the rest. A page that failed, was blocked by robots.txt or is not text is reported: say so, do not guess its content.
5. **Cross-check.** A key claim needs two independent sources, or is marked single-source. "Independent" means not one copying the other. Note dates; old information about a changing fact is a risk.
6. **Separate three things** in everything you write: **observed** (stated by a source, cite it), **inferred** (your reasoning from sources, say it is yours) and **unknown** (searched for and not found).
7. **Never state anything that is not in a source you fetched.** If a search snippet is all you have, say so and fetch the page.

## The report

Write it to a workspace file and read it back. Include:

- **Summary:** the answer to each question in a sentence, with a confidence level (high / medium / low) and why.
- **Findings:** each claim with its sources as `[S3]`, `[S7]`. Quote exact figures and names as the source gives them.
- **Timeline** when dates matter, **entities** (organisations, people in public roles, domains) with how they connect, and **contradictions** between sources.
- **Gaps:** what you could not find, which queries you tried (`research/search-log.jsonl` has them all), and what would settle it.
- **Method:** providers used and any limits (for example a search provider that refused).

## Tool limits

- `web_search` uses an official search API, never a scraped results page: Google results through Serper (`SERPER_API_KEY`), Brave (`BRAVE_API_KEY`) or a SearXNG server (`SEARXNG_URL`), whichever is configured. If none is, it says so; do not look for a way around that. `provider="wikipedia"` needs no key but searches Wikipedia only, so say in the report that the search was limited to it.
- `fetch_pages` reads text, HTML, JSON and (when a PDF reader is installed) PDFs. A page that needs JavaScript to show its content may come back nearly empty; say so rather than reporting it as empty.
- It respects `robots.txt` and sends nothing to a site. Do not try to work around either.
