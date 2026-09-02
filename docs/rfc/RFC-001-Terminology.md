# RFC-001: Terminology

**Status:** Accepted
**Date:** 2026-09-02

## 1. Introduction
This RFC establishes a shared, canonical vocabulary for the Reticle Orchestrator Framework. Every future RFC and architectural decision MUST reference these exact definitions to ensure consistency across the ecosystem.

## 2. Core Entities

### 2.1. Agent
An autonomous, LLM-backed entity capable of taking actions, calling tools, and reasoning about a problem. Agents are stateless across runs unless explicitly provided with Context by the Orchestrator.

### 2.2. Skill
A reusable, domain-specific instruction set (usually defined in a `SKILL.md` file) that injects strict behavioral constraints, formatting rules, or methodologies (e.g., `ml-engineering`, `frontend-uiux`) into an Agent.

### 2.3. Plugin
A bundle of capabilities that extends the core Framework. Plugins group together custom Skills, Subagents, and Tools into a distributable package.

### 2.4. Tool
An executable function exposed to an Agent. Tools bridge the LLM reasoning layer with the host operating system or external services (e.g., `execute_terminal_command`, `write_file`).

### 2.5. Supervisor
A special class of Agent responsible for planning, task delegation, and graph construction. Supervisors do not perform work; they instruct Workers. (In Reticle, `architect-agent` and `planner-agent` act as temporary supervisors).

### 2.6. Worker
An Agent assigned to execute a specific, bounded Task. Workers return Artifacts upon completion.

## 3. Work & Execution

### 3.1. Task
An atomic unit of work assigned to a single Worker. A Task includes a prompt, required inputs (Artifacts), and expected outputs.

### 3.2. Job
A collection of Tasks that together achieve a user's overarching goal.

### 3.3. Graph (Supervisor Graph)
A Directed Acyclic Graph (DAG) representing the dependencies and parallel execution paths of Tasks within a Job. 

### 3.4. Session
An isolated workspace environment created for a specific Job. Sessions prevent state clashing by sandboxing file mutations (e.g., `.reticle/sessions/exec-001`).

## 4. State & Data

### 4.1. Event
An asynchronous message broadcast over the Go Event Bus. Everything in the framework (e.g., `TaskCreated`, `WorkerStarted`, `ArtifactStored`) is driven by Events.

### 4.2. Memory
The global key-value store utilized by the Go Orchestrator to persist state across asynchronous Event firings.

### 4.3. Knowledge (Knowledge Items)
Curated, persistent summaries of established patterns within a codebase. KIs serve as ground-truth retrieval points for Agents to understand local repo conventions.

### 4.4. Context
The dynamically assembled payload (prompt history, file contents, upstream artifacts) injected into an Agent's prompt. 

### 4.5. Artifact
A structured output produced by a Worker at the conclusion of a Task. Artifacts are passed downstream to dependent nodes in the Graph.

## 5. System

### 5.1. Runtime (Orchestrator)
The core Go binary (`forge.exe`) responsible for bootstrapping the Event Bus, managing the Session isolations, routing API calls, and orchestrating the DAG.

### 5.2. Capability
A quantitative or qualitative metric used by the Model Router to determine if an LLM is sophisticated enough to handle a specific Task (e.g., assigning a high-capability model to architecture, and a low-capability model to simple formatting).
