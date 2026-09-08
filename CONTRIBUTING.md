# Contributing

Make focused changes with regression tests for observable behavior. Run Go tests and vet in runtime, cmd/forge and each example module. Run Studio test, typecheck, build and lint, and run python -B tests/test_workers.py from the repository root. Never run provider probe scripts as ordinary offline tests. CI configuration is in .github/workflows/verify.yml.

Keep secrets out of source, fixtures, logs and screenshots. Use .env.example for variable names. Do not replace populated local credentials.

Frozen contract changes follow docs/governance/RFC_PROCESS.md. Use the schemas and current specifications; historical audit reports retain their baseline findings. Record fixes in the audit repair ledger.

The repository owner still needs to choose and provide the project's license text; the empty historical LICENSE file did not specify one. Do not infer a license grant from it.
