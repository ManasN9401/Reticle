# Studio workspace and activity views

Studio follows the run selected in the Runs sidebar. Its Explorer resolves compiler execution IDs such as `compile-<run>` to the same isolated session directory used by the resulting workflow, then shows that workspace's directory tree and files.

The Explorer header reports file and directory counts, total size and the most recently changed files. Selecting a recent file or a tree file opens the existing read-only editor. Selecting the folder button reveals the session in the operating system's file manager. While a run is active, Studio refreshes the summary and expanded directories every three seconds; the manual refresh button remains available after the run finishes.

Filesystem inspection stays in Electron's trusted main process. Paths must resolve inside the configured Reticle checkout, tree scans do not follow symlinks, sensitive environment/control-token files remain unreadable, large dependency/cache directories are skipped, and recursive summaries stop after 20,000 entries. The cap is displayed in the Explorer when reached.

The lower Activity panel is the default operational view and has three sections:

- **Progress** shows run totals, active agents, model/attempt details and recent lifecycle milestones.
- **Dependencies** shows the shared base environment and each agent package set. It distinguishes active installs, cache reuse, completion, duration, package manager and failures.
- **Tools** separates worker tool calls from ordinary output.

Raw output remains available under Logs, node-specific output and model text remain in the node inspector, and failures remain under Problems. Dependency provisioning emits `EnvironmentProvisioningStarted`, `EnvironmentProvisioningCompleted` and `EnvironmentProvisioningFailed` events over the normal telemetry stream. This allows Activity to work when Studio connects to an external Forge process instead of relying on terminal text from a process Studio launched.
