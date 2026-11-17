# Project environments, managed servers and vision

These additive SDK tools implement the application-loop portions of the [production plan](audit/2026-10-21/02-implementation-plan.md) and [RFC-054 proposal](rfc/RFC-054-Agent-Tool-Surface-Roadmap.md). Existing worker framing and capability names are unchanged. They do not implement durable conversations, multi-file transactions or Git rewind.

## Explicit environment preparation

`prepare_environment(path=".", profile="auto")` is available in container mode with `workspace.read`, `workspace.write`, `process.container` and `network.public`. It is unavailable to DevOps and pentest workers. Installation can execute project scripts and is an unsafe effect for retry purposes; it is never triggered by reading files or running diagnostics.

Configure reviewed toolchain images in the environment that starts Forge:

| Setting | Image must contain | Supported project lock | Preparation |
| --- | --- | --- | --- |
| `RETICLE_NODE_IMAGE` | Node, npm, POSIX shell | `package.json` and `package-lock.json` | `npm ci --no-audit --no-fund` |
| `RETICLE_PYTHON_IMAGE` | Python, uv, POSIX shell | `pyproject.toml` and `uv.lock` | `uv sync --frozen --no-managed-python` |
| `RETICLE_GO_IMAGE` | Go, POSIX shell | `go.mod` and `go.sum` | `go mod download` |

Prefer reviewed digest-pinned images. Reticle resolves any configured image reference to its immutable local Docker image ID before running installation. It checks toolchain versions, reports their output, and hashes the manifest/lock inputs. It does not guess toolchain images, silently install a host toolchain, or claim that every package script is safe. Node preparation supports npm only; the diagnostic adapters still support pnpm, Yarn and Bun when already available in the chosen environment. A project with multiple language manifests requires an explicit `profile`.

Dependencies and caches persist in the session's source project: `node_modules`, Python `.venv`, and `.reticle-npm-cache`, `.reticle-uv-cache` or `.reticle-go-cache`. These directories are not application deliverables. A prepared Python command uses that project's `.venv/bin` on PATH; Go module/build caches and npm/uv caches use project-local paths instead of disappearing with a command container. Project environment state is local to this worker attempt, not a global cache or durable environment lease. An OS-owned advisory lock excludes other preparation attempts for the same project until cleanup or process death; it does not prevent arbitrary project shell commands from changing dependencies. Use separate project directories/worktrees for parallel work.

After successful preparation, `execute_terminal_command(command, path?)`, `run_diagnostics(kind?, path?)` and `start_server(..., path?)` use the same resolved image for the same project path. Set `path` explicitly for a subproject; shell `cd` text is not interpreted for environment selection. Changed dependency manifests invalidate the prepared state. A failed or partial reinstallation cannot retain an old prepared lease. The diagnostic receipt records the selected image and environment overrides alongside artifact hashes. Source files and caches can still be changed by project commands; the receipts do not attest to the integrity of every installed dependency.

Native execution keeps its existing trusted-host meaning and uses the operator's environment. Managed native preparation and managed native servers are not implemented. Existing direct commands remain available under the native grant and setting.

## Managed development servers

`start_server(command, port, path=".", ready_path="/", timeout_seconds=30)` takes an argv array, never adds a shell wrapper, and returns `{id, url, ready_status, ...}` only after an HTTP 2xx response at the requested local path. Example after preparing a Vite project:

```json
{"command":["npm","run","dev","--","--host","0.0.0.0","--port","3000","--strictPort"],"port":3000,"path":"."}
```

The application must bind `0.0.0.0` **inside its container**. Docker publishes only `127.0.0.1:<port>` on the host. An already occupied port is refused. Readiness does not follow redirects or prove that a user journey works. Ports are 1024-65535; the readiness deadline is 1-60 seconds. One active server and at most 16 starts are allowed per attempt.

- `server_logs(server_id, tail=100)` returns at most 200 lines / 20,000 characters from a 64 KiB in-memory log tail.
- `stop_server(server_id)` stops only that attempt's known handle. Unknown or foreign handles fail.
- `run_page(server_id, path?, ...)` observes a running managed server with the existing browser checks. `path` is a route such as `/dashboard`, not an arbitrary URL. Browser grants and other-local-service filtering remain in force.

Servers are unsafe effects. SDK cleanup runs on success and exceptions. Forge additionally assigns a random resource owner to every worker invocation; after worker exit or cancellation it removes only Docker containers carrying that invocation's label, using a separate bounded cleanup deadline. Cleanup failures are reported as worker failures rather than successful completion. This covers worker death while Forge remains alive; recovery from an abrupt Forge/controller crash and startup resource sweeping remain open. The Docker daemon is outside the worker process tree.

Starting a container server currently also requires `network.public`: Docker's `--network none` cannot publish an accessible server port. Network-enabled containers still have unrestricted egress; this feature does not introduce an OS-enforced public-only network boundary. Filesystem mounts and command resource limits retain the existing capability policy. A readiness check can generate application logs or execute application handlers and is part of the unsafe server action.

## Seeing screenshots and local images

`view_image(path, max_side=1280)` is offered only with `workspace.read` and explicit image-input support on the selected model. PNG, JPEG and WebP are supported; URLs and other formats are refused. The worker needs Pillow through a registered skill, such as `frontend-uiux`; an unavailable dependency produces an explicit error.

Inputs are bounded to 16 MiB and 32 million pixels. Output is downscaled to at most the requested side (256-2048) and encoded below 1 MiB per image. Transparency is rendered on white for model observation; source artifacts are unchanged. The model receives source-path/digest metadata plus actual image content, not a text description substituted for an image. At most two images attach per turn; a new image observation replaces prior image payloads, and request compaction removes images before truncating text.

Vision routes also support `run_page(..., view=true)` to attach the generated screenshot. Screenshots remain task data, never higher-priority instructions. Image observation and server readiness do not independently certify application correctness; run diagnostics and relevant browser actions after the final edit.

Image support defaults to false, without guessing from model names. Current sources are OpenRouter `architecture.input_modalities` containing `image`, compatible configured-provider discovery `supports_vision: true`, and operator-supplied static model `vision: true`. Other discovery paths remain unknown until explicitly supported. Studio's provider editor allows marking static models with image input and preserves their other metadata when edited.

When visual observation is an acceptance requirement, set `parameters.requires_vision: true` on the task/node. Dispatcher routing, confidence/modality fallbacks and forced-model checks retain this requirement. A route without explicit support cannot satisfy it. `llm_supports_vision` is selected-route metadata; it is not a grant that a skill or task prompt can use to add execution authority.
