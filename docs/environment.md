# Environment configuration

Copy `.env.example` to `.env` only when no configuration exists. Preserve populated keys. The model catalog uses OpenRouter keys 1–3, Groq keys 1–3 and one Gemini key; none of these providers is mandatory when a supported local provider is configured.

`OLLAMA_HOST` and `COMFYUI_HOST` are endpoint settings, not secret values. `HF_TOKEN` and `WANDB_API_KEY` are optional for experiments that actually need those services. A blank optional key does not mean the core application is broken.

The audit repair inspected variable names and whether values were populated, without printing values. All currently catalogued hosted-provider variables were populated. Additional missing secret values require the user's integration/key names and an identified credential source; they cannot be reconstructed or invented.

The local control service creates `.reticle/control-token`. Studio reads this in its trusted main process. The browser interface asks for that access code. Never commit this file, forward it to an agent, or paste it into task prompts.

Worker dependency provisioning and job environments are separate from credentials. Cloud deployments must use a scoped, explicitly configured execution identity; installing a Python SDK does not establish authorization or a working cloud CLI.

## AWS authentication

EC2 provisioning needs an authenticated AWS identity with the required permissions. It does not necessarily need long-lived secrets in `.env`:

- For a local operator, configure an AWS CLI IAM Identity Center (SSO) profile, authenticate with `aws sso login --profile <profile>`, and select it using `AWS_PROFILE`. Set `AWS_REGION` for SDKs and `AWS_DEFAULT_REGION` for the CLI as needed. Keep profile/cache files outside the repository.
- When supplying temporary environment credentials, use `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` together. A session token is required for temporary credentials, not ordinary long-lived access keys. Environment access keys can override profile credentials; choose one credential path deliberately.
- For software already running on EC2, attach an appropriate IAM instance profile and let the AWS credential provider obtain temporary role credentials. The operator creating that instance still needs their own identity.

An EC2 SSH private key is a separate connection credential, not an AWS API credential. SSM access has its own IAM and instance configuration requirements.

The commented AWS examples describe operator/adapter configuration only. Reticle currently does **not** forward these credentials to generic workers, mount profile files into job containers, provide an AWS command adapter, or perform protected EC2 deployment. Filling `.env` cannot supply those missing capabilities. The deployment adapter must bind an approved action to its account, region and resource scope before credentials are made available to that action. Actual secret values must come from the configured credential source; none were invented or added during this repair.

Sources: [AWS static/temporary credential settings](https://docs.aws.amazon.com/sdkref/latest/guide/feature-static-credentials.html), [CLI credential precedence](https://docs.aws.amazon.com/cli/latest/topic/config-vars.html), [EC2 role credentials](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp_use-resources.html).

Workers provision dependencies lazily, with a cancellation-aware deadline. The shared Python base has pinned direct dependencies; specialist transitive dependencies still require a reviewed lockfile for reproducible experiments. Native commands use the worker interpreter's directory on `PATH`. Container commands use the configured image instead of host-installed packages.

For a prepared job image, set `RETICLE_WORKER_IMAGE` (prefer a digest), `RETICLE_MEMORY_LIMIT`, `RETICLE_CPU_LIMIT`, and `RETICLE_COMMAND_TIMEOUT` (1–3600 seconds; default 120). ML workers can receive `RETICLE_GPU_DEVICES` as explicit numeric devices. This exposes devices to that container; it does not implement cross-job GPU scheduling. Validate the selected image's libraries, drivers, CLI versions and a smoke run before a full experiment. The default image is a small Python environment, not a ready-made CUDA or cloud deployment environment.

`COMFYUI_CHECKPOINT` must match a checkpoint installed on the configured local ComfyUI server. Image requests have a deadline; an already-running server job may outlive the requesting worker. Inspect the server queue after cancellation.

`HF_TOKEN` and `WANDB_API_KEY` are passed only to the builtin ML worker. They are not automatically injected into arbitrary generated workers or container commands. Configure any such transfer deliberately in a scoped adapter.

## Memory lifecycle and recovery

With `RETICLE_ROOT` configured, retained shared memory and artifacts are recovered from `.reticle/memory/state.json`. The `.reticle` directory is ignored by Git and the snapshot is written with owner-only permissions where the operating system supports them. Do not store credentials in shared memory: the snapshot contains values in readable JSON.

Defaults limit runtime state to 10,000 entries and 16 MiB, expire orphaned execution memory after 24 hours, release execution scratch memory at terminal workflow events, and retain the latest 100 versions of each artifact. Override these using `RETICLE_MEMORY_MAX_ENTRIES`, `RETICLE_MEMORY_MAX_MIB`, `RETICLE_EXECUTION_MEMORY_TTL_HOURS`, and `RETICLE_ARTIFACT_MAX_VERSIONS`. Set `RETICLE_MEMORY_PERSISTENCE=false` to run without restart recovery.

Conditional memory updates carry `expected_version`. Use zero for create-if-absent or the revision returned by a read/update for compare-and-set. A mismatch rejects the mutation; it does not overwrite the newer value. Unconditional updates remain available for facts whose last writer is intentionally authoritative.

Run `tools/eval_memory_quality.py` without a model for an offline contract check. Supplying `--model <litellm-model-id>` runs model calls in three context modes and can consume provider quota. Store reviewed result JSON outside committed source unless it is an intentional evaluation baseline.
