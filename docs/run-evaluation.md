# Run evaluation

`tools/eval_runs.py` submits a fixed set of requests to a running Forge, follows each run to the end, and scores what it delivered with objective checks. It answers "did this change make Reticle better or worse?" with numbers instead of an impression from one run. The cases are in [`evals/runs/cases.json`](../evals/runs/cases.json).

Unit tests check Reticle's contracts. This checks its output: a run the runtime records as completed can still deliver a page that throws on load and shows nothing, and only an external check like this one notices.

## Running it

Start Forge (and ComfyUI, if the image case should count), choose the models in Studio, then:

```powershell
python tools/eval_runs.py check                    # validate the cases; reports whether a browser and ComfyUI are available
python tools/eval_runs.py run                      # every case once
python tools/eval_runs.py run --cases landing-images,todo-app --repeat 3
python tools/eval_runs.py run --execute            # also run the generated Python tests on this machine
```

Cases run one after another, so they do not compete for a local GPU. Each case has its own timeout; a run that exceeds it is killed and recorded as `TIMEOUT`. Results go to `.reticle/evals/<time>-<commit>/`:

- `results.json`: every check and its details, run metrics and the environment;
- `summary.md`: one row per case;
- `<case>-<repeat>/workspace/`: a copy of the run's session, which the scorer works on so it never changes the original;
- `<case>-<repeat>/screenshot.png`: the page as the headless browser saw it;
- `run.log`: progress, including every checkpoint that was approved.

`score SESSION_DIR --case ID` scores an existing session directory (the one containing `src/`) without running anything, for example `.reticle/sessions/exec-…`.

### Approval checkpoints

An unattended run cannot wait for a person, so by default the tool approves the run's own approval checkpoints: requests in `.reticle/approvals/` whose task belongs to that execution, using the same hash-bound decision file Studio writes. Requests from other runs are never touched. Each approval is logged and listed in the result. Pass `--no-auto-approve` to leave them for a person; such a run then usually ends as `TIMEOUT`. The cases do not ask for deployments or other external effects.

### Generated tests

`--execute` runs each Python case's `test_command` with this interpreter, in the copied workspace, with a three-minute limit. That is generated code running on your machine, so it is off by default. A test run that fails only because a dependency the project declares in `requirements.txt` is not installed here is recorded as not applicable rather than as a failure. A missing module the project never declared still counts as a failure.

## Checks

A case's score is the fraction of its applicable checks that passed. A check that could not be assessed, such as browser checks without a browser, is left out of the score rather than counted as failed.

| Check | Applies to | Passes when |
|---|---|---|
| `finished` | all | the run ended `COMPLETED` |
| `files_present` | all | every `expect.files` glob matches a non-empty delivered file |
| `no_placeholders` | all | files were delivered, and none contains lorem ipsum, a `TODO`/`FIXME` comment, a "your code here" stub or a remote placeholder image service |
| `mentions` | when listed | every `expect.mentions` string appears in a delivered file or file name |
| `static_web` | web | `check_web_page` finds no blocking problem |
| `page_loads` | web | the first page loads in a headless browser |
| `no_page_errors` | web | no exceptions, console errors, failed requests or HTTP errors, and the page stays responsive (the problem count `run_page` reports) |
| `has_content` | web | under 95% of the screenshot is one colour |
| `animates` | `expect.canvas` | animation frames ran and a canvas has a drawing context |
| `images` | `expect.images` | at least that many real image files exist and a page, stylesheet or script references them. Not applicable when ComfyUI could not generate images as the run started |
| `python_compiles` | python | every `.py` file parses |
| `tests_pass` | `expect.test_command` | the generated tests pass (with `--execute`) |

Each result also records the run's node count, attempts, failed attempts with their reasons, and the models used, from `.reticle/executions/state.json`. These measure cost and reliability, not quality.

These checks are a floor, not a judgement of design. A page can pass all of them and still look plain. Look at the screenshots, and add a check here when a recurring problem can be stated objectively.

## Comparing versions

```powershell
python tools/eval_runs.py compare .reticle/evals/A/results.json .reticle/evals/B/results.json
```

This prints each case's mean score in A and B, the change, and which checks moved. It warns when the enabled models differ or a case was edited between the runs: either makes the comparison about something other than Reticle.

Model output varies from run to run. With one run per case, treat a difference smaller than one check as noise; use `--repeat 3` or more before drawing conclusions.

To evaluate an older version, check it out in its own worktree, build and start its Forge from there, and point the tool at it:

```powershell
git worktree add ..\reticle-old 954fcbc
# build and start that checkout's Forge on a free port, then:
python tools/eval_runs.py run --reticle-root ..\reticle-old --port 8090
```

The tool talks to Forge only through its websocket queue, `.reticle/sessions/`, `.reticle/approvals/` and `.reticle/executions/`, so it works with any version that has them. A version without a control token is reached without one.

## Adding a case

Add an entry to `evals/runs/cases.json`:

- `id`: lower-case letters, digits and hyphens;
- `kind`: `web` or `python`;
- `depth`: the Studio depth, `1` (single agent), `3` (balanced) or `5` (deep);
- `timeout_minutes`;
- `prompt`: the full request, as a user would type it;
- `expect`: `files` (required globs, where `**/` matches any depth including the top level), and optionally `mentions`, `images` and `canvas` (web cases) or `test_command` (Python cases; arguments after the interpreter).

Run `python tools/eval_runs.py check` to validate the file. Editing a case changes its digest, which `compare` reports, so results from before the edit are not silently compared with results after it.
