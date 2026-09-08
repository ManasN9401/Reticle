# Environment configuration

Copy `.env.example` to `.env` only when no configuration exists. Preserve populated keys. The model catalog uses OpenRouter keys 1–3, Groq keys 1–3 and one Gemini key; none of these providers is mandatory when a supported local provider is configured.

`OLLAMA_HOST` and `COMFYUI_HOST` are endpoint settings, not secret values. `HF_TOKEN` and `WANDB_API_KEY` are optional for experiments that actually need those services. A blank optional key does not mean the core application is broken.

The audit repair inspected variable names and whether values were populated, without printing values. All currently catalogued hosted-provider variables were populated. Additional missing secret values require the user's integration/key names and an identified credential source; they cannot be reconstructed or invented.

The local control service creates `.reticle/control-token`. Studio reads this in its trusted main process. The browser interface asks for that access code. Never commit this file, forward it to an agent, or paste it into task prompts.

Worker dependency provisioning and job environments are separate from credentials. Cloud deployments must use a scoped, explicitly configured execution identity; installing a Python SDK does not establish authorization or a working cloud CLI.

Workers provision dependencies lazily, with a cancellation-aware deadline. The shared Python base has pinned direct dependencies; specialist transitive dependencies still require a reviewed lockfile for reproducible experiments. Native commands use the worker interpreter's directory on `PATH`. Container commands use the configured image instead of host-installed packages.

For a prepared job image, set `RETICLE_WORKER_IMAGE` (prefer a digest), `RETICLE_MEMORY_LIMIT`, `RETICLE_CPU_LIMIT`, and `RETICLE_COMMAND_TIMEOUT` (1–3600 seconds; default 120). ML workers can receive `RETICLE_GPU_DEVICES` as explicit numeric devices. This exposes devices to that container; it does not implement cross-job GPU scheduling. Validate the selected image's libraries, drivers, CLI versions and a smoke run before a full experiment. The default image is a small Python environment, not a ready-made CUDA or cloud deployment environment.

`COMFYUI_CHECKPOINT` must match a checkpoint installed on the configured local ComfyUI server. Image requests have a deadline; an already-running server job may outlive the requesting worker. Inspect the server queue after cancellation.

`HF_TOKEN` and `WANDB_API_KEY` are passed only to the builtin ML worker. They are not automatically injected into arbitrary generated workers or container commands. Configure any such transfer deliberately in a scoped adapter.
