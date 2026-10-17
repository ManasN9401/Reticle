---
name: browser-automation
description: How to read and extract information from websites with browse_web, a real headless browser, honestly and within its limits.
---

# Browser automation

`browse_web` opens a public website in a real browser (Playwright), runs its JavaScript, and returns the page's text, headings, links, tables and anything you pick with CSS selectors. Everything it reads is also saved under `browse/` for other agents.

## Method

1. **Look first.** Call `browse_web` with only the `url`. Read the title, headings and text before deciding what to do. Most pages need nothing more.
2. **Extract, don't scroll through prose.** For repeated structures (prices, results, rows) pass `extract.selectors`, for example `{"selectors": {"names": ".item h2", "links": {"css": ".item a", "attr": "href"}}, "tables": true}`. Check the count it reports against what you expect.
3. **Drive the page with `actions`, in order:** `{"click": "text=Next"}`, `{"fill": ["input[name=q]", "query"]}`, `{"press": "Enter"}`, `{"select": ["#sort", "price"]}`, `{"scroll": 1200}`, `{"wait_for": ".results"}`, `{"goto": url}`. After an action that loads content, add `wait_for` for the thing you need instead of guessing a delay.
4. **Paginate deliberately.** One call per page, stop when the data you need is complete or the pages repeat. Never loop through hundreds of pages.
5. **Prefer stable selectors:** ids, `name` attributes, `aria-label`, visible text (`text=Label`). Avoid long generated class chains.
6. **Verify what you report.** Quote values exactly as the page shows them, with the page URL and the time you read it. If a number is not on the page, say so; do not fill it in.

## Limits you must respect

- **Public websites only.** Private and local addresses are refused. By default the browser is read-only: only GET requests leave it. Set `allow_submit` to `true` only when the user asked you to submit a form (for example a site's own search), and then only to the site you are on.
- **Walls and checks.** If the result says `ACCESS WALL`, or shows a CAPTCHA, a consent or "verify you are human" page, a login you were not given, or HTTP 401/403/429: stop. Do not try to get around it, retry in a loop, or look for a trick. Report that the page could not be read and why, and offer another source.
- **Credentials.** Never type a password or any secret unless the user supplied it for this task on this site. Never guess one.
- **Be a light visitor.** Few pages, no rapid repetition, respect what a site's terms or `robots.txt` ask. For bulk collection of a site's data, stop and ask the user.
- **Downloads are refused,** popups are closed and dialogs dismissed; the result tells you when that happened.
- **Your own site** (files in the workspace) is tested with `run_page`, not this tool.

## What to deliver

Write the findings to a workspace file when the task asks for one: the question, each finding with its source URL and retrieval time, what you could not read and why. Read the file back before finishing.
