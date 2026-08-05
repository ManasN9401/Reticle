# Changelog

All notable changes to the HyperParallel Orchestrator Framework will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] - 2026-08-04

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

### Changed
- **Robust Stdout Parsing**: The Go orchestrator's `worker.go` now uses `bufio.Scanner` to isolate the final valid JSON payload line-by-line, discarding noisy logs and warnings emitted by 3rd-party libraries (e.g. `litellm`).
- **Telemetry Integration**: The Telemetry UI is now natively embedded into the compiled Go binary using `//go:embed`, requiring no external frontend hosting.
