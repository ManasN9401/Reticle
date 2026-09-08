# Audit evidence

These fixtures document observed behavior in checkout `9a5fd070fef92da7826698e58ac37ae245a9c252`. They are diagnostic probes, not a full regression suite or a claim of live cloud/ML verification.

- `tracked-files.csv`: census of all 384 tracked files before this audit added artifacts.
- `check_python.py` / `python-results.json`: syntax of all tracked Python; temporary generator fixtures; synthetic file containment/content checks; recorded native/Docker command construction; HitL stdin check. Uses an existing Python interpreter and the standard library. Does not execute generated workers or start Docker.
- `check_runtime.go` / `runtime-results.json`: real Go registry, workflow and worker interfaces with synthetic subprocesses and a 500 ms deadline. Run from the `runtime` module with `go run ../docs/audit/2026-09-08/evidence/check_runtime.go`. Existing module dependencies are required.
- `check_studio.cjs` / `studio-results.json`: transpiles the existing pure projection with Studio's already-installed TypeScript and exercises synthetic lifecycle/session/replay events. Run from the repository root with `node docs/audit/2026-09-08/evidence/check_studio.cjs`. No Electron or network launch.
- `race-results.txt`: records the environment limitation preventing `go run -race`. It is not a successful race-test result. The optional runtime probe `race` argument is intended for a suitably configured diagnostic environment and may intentionally expose the concurrent-map failure.

The probes save results alongside themselves. Temporary filesystem fixtures are scoped to their own temporary directory. Output paths in the saved results identify disposable fixtures, not user files. Full build/vet/lint/audit command outcomes are recorded in the findings report; not every console transcript is retained.

No actual `.env` contents, provider credentials, private session contents, live cloud resources or model outputs were needed for these probes. No production source changes were made.
