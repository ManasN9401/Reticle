---
name: run-and-observe
description: How to verify a web page by running it in a headless browser and acting on what the browser reports.
---

# Run and observe

A page that passes the static check can still be dead on arrival. If you have `run_page`, run it.

1. **Static first.** Call `check_web_page` until it reports no blocking problems. It is cheap and it names the file.
2. **Then `run_page`.** Read its report in this order:
   - `UNCAUGHT EXCEPTIONS` and `FAILED REQUESTS`: while a script fails to load or throws, nothing below matters.
   - `HTTP errors`: a 404 for a texture, model or script is a wrong path or a file nobody wrote.
   - `Canvases`, `Animation` and `Screenshot content`: what actually reached the screen.
3. **Common causes.**
   - `THREE is not defined`: the library script did not load (look at FAILED REQUESTS) or loads after the script that uses it. `ERR_BLOCKED_BY_ORB` on a CDN script means the URL answered with a 404 page: use a URL that exists, for example `https://cdnjs.cloudflare.com/ajax/libs/three.js/r128/three.min.js`.
   - `no context was ever requested`: the code that creates the renderer never ran, usually because of the error above.
   - `NO requestAnimationFrame callbacks`: the render loop never started, or threw before its first frame.
   - `BLANK` with no errors: the camera is inside geometry or looking away, there are no lights, the canvas has no size, or a texture did not load.
4. **Reach the states that need input** with `actions`, for example `{"click": "#enter"}`, `{"press": "ArrowRight", "hold_ms": 400}`, `{"eval": "document.title"}`.
5. **Fix, then run again.** Stop when the report ends `Result: NO PROBLEMS FOUND.`

Software rendering is slower than a GPU: a low frame rate alone is not a bug. Report honestly what `run_page` said; never describe a page as working if it flagged a problem you did not fix.
