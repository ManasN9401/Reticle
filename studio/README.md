# Reticle Studio

Desktop client for the Reticle orchestrator. Watches and steers `forge.exe` runs:
live DAG node map, log stream, agent roster, artifacts, and human-in-the-loop
approvals.

```bash
npm install
npm run dev          # Vite + Electron, hot reload
npm run build        # typecheck + production bundle
npm run typecheck
npm run lint
npm run package      # electron-builder installer
```

## Connecting to forge

Studio can **attach** to a running orchestrator or **spawn** one itself.

```bash
cd cmd/forge && go build -o forge.exe
./forge.exe -native -port 8080
```

Then connect from the status bar, or press <kbd>F5</kbd> to let Studio launch
forge for you. Two constraints are enforced in `electron/forge/process.ts` and
are worth knowing, because getting either wrong fails silently:

- **cwd must be `<repo>/cmd/forge`.** `cmd/forge/main.go:166` resolves its root
  as `filepath.Abs("../../")`, and the logger and waitlist reach for
  `../../logs` and `../../.reticle`. Launched anywhere else, sessions, logs and
  artifacts resolve to the wrong place and the UI just looks empty.
- **`-native` is always passed.** Without it, forge probes Docker and, on
  failure, blocks on an interactive stdin prompt a spawned child can never
  answer — which presents as a hang, not an error.

## Architecture

```
electron/                  main process — owns everything privileged
  forge/client.ts          WebSocket to /ws, backoff reconnect
  forge/process.ts         spawn/kill forge.exe
  forge/store.ts           event ring buffer + projection + payload stripping
  forge/rest.ts            /api/models, /api/outputs, /api/upload
  hitl/checkpoints.ts      read/write approval markdown
  workspace/reader.ts      guarded filesystem reads
src/shared/                pure, imported by BOTH main and renderer
  events.ts                wire decoders (socket + runtime.log)
  ids.ts                   identity normalization
  projection.ts            events -> runs/nodes reducer
src/design/                tokens + primitives
src/app/                   shell: rail, sidebar, tabs, panel, status bar, palette
src/features/              graph · logs · runs · agents · artifacts · hitl · editor · settings
```

### The event store lives in the main process

The backend has no state-snapshot endpoint: reconnecting replays only
`WaitlistUpdated` and `WorkflowStarted`, so per-node status, timings and
artifacts exist *only* as a consequence of the event stream. Holding the
projection in the main process means run history survives reconnects, renderer
reloads and HMR without any change to the Go side.

It also absorbs two problems that would otherwise reach the UI:

- `WorkerLog` is ~77% of all event volume, so it is kept in per-node ring
  buffers and streamed to the renderer only for the node in view.
- `Artifact.Data` is broadcast unstripped and can be megabytes; it is reduced to
  a size and preview before crossing the IPC boundary.

`src/shared/projection.ts` is a pure fold over an ordered event log, which is
what makes the timeline scrubber possible — replaying to any `EventID` is just
running the same reducer over a prefix.

### Wire-format traps

The runtime is inconsistent about naming. Everything goes through
`src/shared/ids.ts` rather than reading payloads directly.

| Trap | Detail |
|---|---|
| Capitalized envelope | `RuntimeEvent` has no json tags: `ID`, `Type`, `Timestamp`, `Source`, `SessionID`, `Payload` |
| Two decoders | `runtime.log` uses lowercase `time`/`component`/`event`/`payload` |
| exec id | `exec_id` on graph_engine events, `execution` on worker events |
| Capitalized edges | `WorkflowStarted.payload.edges[].From` / `.To` |
| Join key | `task_id` is the composite `"{execId}\|{nodeId}"` |
| Display label | Show `agent_id`, never the generic `node-1` (RFC-022 law 5) |
| No durations | Derived from `WorkerCompleted − WorkerStarted` per task id |
| No tokens or cost | Not instrumented anywhere — do not build panels needing them |

Node status is `pending | running | done | failed`, plus a fifth Studio-only
`waiting`, synthesized from the `[UI_STATE: WAITING_HUMAN]` sentinel inside a
`WorkerLog` payload.

### Human-in-the-loop approvals

`hitl-agent` writes a markdown checkpoint, prints `[UI_STATE: WAITING_HUMAN]`,
then polls that file every 2s for a `STATUS:` change (RFC-038). Studio drives it
by writing the decision back, so approve/reject works with **no backend change**.

The checkpoint path is scraped from the agent's own
`Checkpoint file created at: …` log line rather than reconstructed, because
`hitl.py:24` hardcodes `d:\Reticle\runtime\checkpoints` — a directory that does
not exist in this repo and that `os.makedirs` therefore creates outside the tree.
Reading the path from the log keeps Studio correct either way.

## Settings and API keys

Settings is section-driven (`src/features/settings/`): Appearance, Connection,
Forge, **API Keys**, **Models**, Logs, About.

**API Keys** reads and writes the repo-root `.env`, which
`cmd/forge/main.go:23` parses itself — a plain `KEY=VALUE` scan with `#`
comments and no quote handling. Writes preserve every other line, comment and
ordering, and go through a temp file plus rename so a crash cannot truncate the
file. Two facts are surfaced in the panel rather than left to be discovered:

- the router only reads a **fixed set of variable names**
  (`routing/models.go:100`, `:194`, `:255`) — three OpenRouter slots, three Groq,
  one Gemini. Anything else is kept in the file but flagged as never read.
- `.env` is parsed once at boot, so edits need a **forge restart**.

Values are masked to the last four characters. Plaintext only leaves the main
process through an explicit reveal, which re-hides itself after 20 seconds.

**Models** lists the live catalog from `GET /api/models`, filterable by search,
provider, **API key slot**, enabled state, and health — where health folds in
whether that key is rate-limited (from `WaitlistUpdated.lockedKeys`) or missing
from `.env` entirely. A model bound to a dead key cannot run, and that is
invisible from the model id alone. Bulk enable/disable applies to the current
filter.

## Node map presentations

The graph toolbar switches between three, and the choice is persisted:

| Style | Node | Use |
|---|---|---|
| **Detailed** | 224x68 card — agent, node id, model, artifacts, retries, duration | default; full triage detail |
| **Compact** | 168x30 one-liner — status rail, agent, duration | graphs of dozens |
| **Hexagonal** | 72x58 hexagon — status stroke + core, label beneath | densest; echoes the embedded star map |

All three encode status as saturated colour on an otherwise achromatic shape, so
run state is readable at zoom levels where no text is. Geometry *and* dagre
separation are per-style (`NODE_GEOMETRY` in `features/graph/layout.ts`) — the
spacing that keeps 224px cards legible leaves hexagons swimming.

The hexagon is a single inline `<svg>` `<polygon>` — `clip-path` discards
borders, so faking an outline with a second clipped layer behind reads as a
thick soft ring at this size and cannot antialias. Its interior stays empty, as
in the star map (`runtime/telemetry/ui/index.html:2717-2745`): a dark well
filled with `--color-inset`, a crisp status stroke, and a small status core —
solid when done, pulsing when running, hollow when pending or blocked, an `x`
when failed. Identity lives in the label beneath; state lives in the stroke.

Two details are load-bearing rather than decorative. A mocked node keeps a small
amber dot on its upper-right vertex, because RFC-026 §8 requires mock output to
be obviously distinguishable. And a node blocked on a human turns its *label*
into the Review button rather than floating a pill over it — same footprint, no
overlap, still one click.

Edges are orthogonal elbows with rounded corners, in every style. That routing
needs roughly `2 x borderRadius` of vertical clearance, which is why
`NODE_GEOMETRY` reserves 50px of rank separation for hexagons — starved of room
it flattens into right-angle staples. Both handles are pinned to the hexagon's
own vertices rather than the node box: the box is taller than the shape because
it carries the label, so a default bottom handle launches every outgoing edge
from under the text. The label then carries the canvas colour behind it, so a
line dropping from the vertex is interrupted at the glyphs instead of striking
through them.

### Keeping the map responsive under load

`layoutPositions` (dagre) is deliberately separate from `buildGraph`. Positions
depend only on topology — ids, edges, direction, style, manual overrides — never
on status or duration, so the layout memo in `GraphCanvas` is keyed on
`run.edges` identity and the node count rather than on `run` itself. The reducer
preserves that array reference across copy-on-writes, making it an O(1) proxy
for "the shape changed".

This matters more than it sounds. Measured at 60 nodes, one dagre pass costs
~10.8ms; keyed on `run` it ran on every event batch, which at the old 16ms flush
was **~650ms of layout work per second** — most of a core, and the reason zoom
and pan stuttered mid-run. `buildGraph` costs 0.03ms by comparison. Two
supporting changes: `AgentNode` carries an explicit `memo` comparator (fresh
`data` objects otherwise defeat it, repainting every node on every push), and
`STATE_FLUSH_MS` is 100ms rather than 16ms.

## Design

Direction is "Instrument": graphite neutrals, hairline borders, no glow. The
organizing rule is that **chrome is achromatic and saturated colour means run
status and nothing else** — which is what makes one failing node findable in a
graph of sixty. Status hues are inherited from the embedded telemetry star map
so the two consoles agree.

Tokens live in `src/design/tokens.css` (declared inside Tailwind's `@theme`, so
one definition produces both utilities and raw custom properties). Fonts are
bundled via `@fontsource-variable`, never fetched — an Electron app must render
correctly with no network.

### Theming

Dark, Light, or Match System — from Settings → Appearance, the status-bar
toggle, <kbd>Ctrl+Shift+L</kbd>, or the View menu.

"System" is resolved to a concrete `dark`/`light` in `state/theme.ts` **before**
it reaches the DOM, so the stylesheet only expresses two palettes and anything
needing the actual theme (Monaco, the native window background) reads one value.
`data-theme` is always written explicitly; there is no media query in the CSS.

The light palette is not an inversion. The dark status hues are tuned for a
near-black ground and several — the cyan especially — all but vanish on white,
so every saturated value is re-picked for contrast. Shadows are re-picked too,
shallower and tighter, since dark shadow recipes go muddy on light surfaces.

Two things follow the theme that are easy to miss: the **Electron window
background** (set at creation from the persisted preference and updated on
change, so neither a relaunch nor a reload flashes the wrong colour), and
**Monaco**, which needs literal hex rather than custom properties and so carries
a hand-maintained mirror of the surface tokens in `features/editor/monacoSetup.ts`.

## Security

The renderer runs with `nodeIntegration: false`, `contextIsolation: true` and
`sandbox: true`. Its only route to the OS, the filesystem or the orchestrator is
the preload bridge in `src/shared/ipc.ts`. Filesystem reads are confined to the
repository root, and approval writes are restricted to `approval_req_*.md`.

Both main and preload are bundled as **CommonJS**. This is deliberate:
`electron` is a CJS module and Node cannot statically resolve its named exports
through an ESM entry, and an ESM preload would have to be named `.mjs` and would
force `sandbox: false`. `vite-plugin-electron` derives the format from
`package.json` `"type"`, which is why this package intentionally does not set it.

## Not yet wired

The Terminal and Preview dock tabs render labelled empty states describing what
will fill them. They are not disabled mystery buttons — every other visible
control is backed by a real capability or is visibly disabled with a reason.
