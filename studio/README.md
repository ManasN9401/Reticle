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
