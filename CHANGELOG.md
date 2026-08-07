# Changelog

All notable changes to the HyperParallel Orchestrator Framework will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] - 2026-08-07

### Added
- **Forge Autonomous Compiler**: Built a zero-boilerplate, LLM-driven compiler pipeline (`forge.exe`). Users simply pass a prompt (e.g. "Create a 2d platformer game") and the runtime automatically provisions an Architect to design a DAG, a Scaffolder to build the structure, and a Coder to synthesize bespoke Python agents on the fly.
- **Dynamic File Writer Pipeline**: The `scaffolder` now automatically injects a `file-writer-node` into every generated workflow. This node securely intercepts code blocks produced by upstream agents and physically writes them to an isolated `src/` directory inside the workspace, effectively building software end-to-end.
- **Massive Load Balancing Swarm**: Implemented a randomized failover and load-balancing strategy into both the compiler and all dynamically generated agents. By shuffling a pool of 4 API keys (Groq + OpenRouter) on every LLM call, the massive concurrent DAG execution seamlessly evades API rate limits.
- **Artifact GUI Sync**: Upgraded the telemetry UI to parse, render, and display the new file-writing outputs. Successful file operations (creations/edits) are now visible as clickable Artifacts inside the node details panel.

### Changed
- **Compiler State Isolation**: Moving away from static workflows, `forge` now generates sandboxed `forge_workspace_YYYYMMDD_HHMMSS` directories for each execution, keeping generated agents, DAGs, and source code isolated.
- **Robust Stdout Parsing**: The Go orchestrator's `worker.go` now uses `bufio.Scanner` to isolate the final valid JSON payload line-by-line, discarding noisy logs and warnings emitted by 3rd-party libraries (e.g. `litellm`).
- **Telemetry Integration**: The Telemetry UI is now natively embedded into the compiled Go binary using `//go:embed`, requiring no external frontend hosting.

## [0.1.0] - 2026-08-04

### Added
- **Telemetry UI**: Fully interactive Canvas-based Star Map for visualizing execution DAGs in real-time (`hyperparallel.exe -gui=true`).
- **Telemetry UI Controls**: Added drag-to-resize panel dividers, canvas panning, and scroll-wheel zooming for massive graph navigability.
- **GraphEngine**: Replaced hardcoded orchestration with dynamic YAML-driven Directed Acyclic Graph (DAG) execution.
- **Worker Fallbacks**: Implemented mock data fallback mechanisms inside python workers (`worker_llm.py`) to prevent massive DAGs from collapsing during LLM rate limits/API key failures.
- **Example Workflows**: Created `02_book_creator` and `03_startup_launch` (22-node stress test) to prove extreme concurrency scaling and cross-layer edge routing.
- **Architectural Documentation**: Formalized new architecture specifications:
  - `RFC-022`: Dashboard & Graph UI
  - `RFC-025`: Dynamic Workflow Compilation (Zero-Boilerplate orchestrator goals)
  - `RFC-026`: Worker Fault Tolerance & Stdout Protocol
- **Dependency Sandboxing**: Added `.hyperparallel/envs/` directory for environment dependency isolation.
- **Worker Communication**: Established the JSON-RPC STDIO IPC communication contract between Go runtime and Polyglot Workers.
