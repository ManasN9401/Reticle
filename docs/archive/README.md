# Historical and unsupported material

Maintained runtime entrypoints are `cmd/forge`, `runtime`, and `studio`. The offline checks are `.github/workflows/verify.yml`, Go package tests, Python unittest discovery under `tests`, and `studio/tests`.

Older embedded UI copies, scratch generators and one-off repair/probe scripts elsewhere in this repository are historical material. They are not alternate supported launchers or a test suite. Run only the maintained commands listed in `CONTRIBUTING.md`; importing arbitrary historical scripts may execute top-level operations.

Root `src/features` files duplicate old Studio components and are outside the Studio build. They are retained as historical source until references are retired; edit the corresponding `studio/src/features` files for the application.
