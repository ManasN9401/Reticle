# RFC-002: System Overview

**Status:** Accepted
**Date:** 2026-09-02

## 1. Introduction
This RFC provides a high-level architectural "map" of the Reticle Orchestrator Framework. It outlines how the foundational systems interact to execute complex, multi-agent LLM workflows securely and concurrently.

## 2. Runtime Architecture
The framework is built as a hybrid architecture:
- **Go Orchestrator (`forge.exe`):** A high-performance, concurrent backend responsible for state management, API rate limiting (AIMD), graph traversal, and telemetry.
- **Python Workers:** Lightweight, ephemeral sandboxes where the LLM agents actually execute. The Go Orchestrator spawns Python processes to interact with the LLM API and execute tool calls (e.g. filesystem mutations).

## 3. Core Components

### 3.1. Event-Driven Design
The system operates entirely on an asynchronous Event Bus. There are no blocking functional calls between core subsystems. When an agent finishes a prompt, a `WorkerCompleted` event fires, which the `GraphEngine` listens for to unlock the next node in the DAG.

### 3.2. Supervisor Graph & High-Level Execution Flow
1. **Compilation:** A user prompt is sent to the `forge` compiler. An `architect-agent` dynamically generates a JSON-based Directed Acyclic Graph (DAG) detailing the necessary steps and agents required.
2. **Scaffolding:** The framework creates an isolated `Session` (a sandboxed workspace folder) to prevent file collisions.
3. **Execution:** The `GraphEngine` traverses the DAG. When a node's dependencies are met, it fires a `TaskCreated` event.
4. **Dispatch:** The `Dispatcher` catches the task, assigns it to a Python Worker, uses Bayesian routing to assign an LLM, and launches the sub-process.
5. **Assembly:** As workers finish, they produce `Artifacts`, which are stored in the Memory bus and passed as input context to downstream nodes.

### 3.3. Memory Architecture
Reticle avoids monolithic memory stores. Instead, memory is tightly scoped:
- **Global Memory:** Configuration, API routing utility matrices, and telemetry.
- **Execution Memory:** State restricted entirely to the bounds of a specific DAG run.
- **Artifact Store:** Immutable blobs of data produced by Workers, passed downstream.

### 3.4. Agent Ecosystem
Agents are defined by `.yml` files in the `agents/` directory and backed by `worker.py` scripts. They are strictly single-purpose (e.g., `ml-agent`, `coder-agent`, `hitl-agent`) and heavily restricted in scope to prevent catastrophic hallucination cascades.

### 3.5. Plugin & Skill Ecosystem
To inject intelligence without hardcoding logic into the Go binary, the framework heavily utilizes `Skills` (`SKILL.md` markdown files dictating methodologies) and `Plugins` (bundles of tools and skills). The Meta-Scaffolder intercepts these skills and injects them directly into the Python Worker's system prompt before compilation.

### 3.6. User Interaction Model
The framework aims for "Zero-Boilerplate". Users do not write code to define workflows. They simply interface with the Orchestrator via natural language prompts, or intervene during explicit `Human-in-the-Loop` (HitL) approval checkpoints, allowing the framework to act as an autonomous parallel compiler.
