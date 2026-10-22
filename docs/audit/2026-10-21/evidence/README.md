# Audit evidence and reproduction

Baseline: `ca04c9e`. Date: 21 October 2026.

- `verification.json` records checks and execution-environment limitations.
- `python-probes.json` contains the offline Python observations.
- `go-probes.txt` contains the Go protocol/registry/schema observations.
- `probe_python.py` uses disposable directories and mocked model, network-policy and container-command boundaries. It invokes the actual SDK completion path, RAG loop and compiler helper without contacting providers. A child process deliberately exits while holding an edit lock; its entire fixture stays under this evidence directory and is removed by the temporary-directory context.
- `probe_agent_test.go` and `probe_broker_test.go` are audit-only characterization tests loaded using Go's overlay support. They were not inserted into production packages or committed as acceptance tests. A passing probe means the recorded behavior was observed, not that the behavior is safe.

From the repository root, run the Python probes with:

```powershell
python -B docs/audit/2026-10-21/evidence/probe_python.py
```

To run the Go probes on Windows without editing production sources:

```powershell
$repoRoot = (Get-Location).Path
$evidenceRoot = Join-Path $repoRoot 'docs/audit/2026-10-21/evidence'
$scratchRoot = Join-Path $repoRoot '.reticle/audit-2026-10-21'
New-Item -ItemType Directory -Path $scratchRoot -Force | Out-Null
$env:TEMP = $scratchRoot
$env:TMP = $scratchRoot
$overlay = @{ Replace = @{} }
$overlay.Replace[(Join-Path $repoRoot 'runtime/agent/audit_probes_test.go')] = Join-Path $evidenceRoot 'probe_agent_test.go'
$overlay.Replace[(Join-Path $repoRoot 'runtime/toolbroker/audit_probes_test.go')] = Join-Path $evidenceRoot 'probe_broker_test.go'
$overlayPath = Join-Path $scratchRoot 'go-overlay.json'
$overlay | ConvertTo-Json -Depth 4 | Set-Content $overlayPath
Push-Location (Join-Path $repoRoot 'runtime')
try {
    go test -overlay $overlayPath -count=1 -run '^TestAudit' -v ./agent ./toolbroker
} finally {
    Pop-Location
}
```

Ordinary verification commands used:

```text
runtime: go test -timeout 120s ./...
runtime: go vet ./...
cmd/forge: go test ./...
cmd/forge: go vet ./...
repository: python -B -m unittest discover -s tests
repository: python -B -m unittest discover -s cmd/forge/compiler/tests
studio: npm test
studio: npm run typecheck
studio: npm run lint
studio: npm run build
```

The audit environment denied loopback connections and browser startup. Full-suite errors caused by those restrictions were kept distinct from confirmed code defects. Optional dependency skips, the Docker-test mock interaction and disabled CGO remain limitations. No live provider, actual container, signed installer, dependency scanner or external side-effect test was run.
