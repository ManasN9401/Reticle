---
status: historical
owner: Reticle Project
updated: 2026-09-19
---

> Historical design record. Current versioned specifications and schemas take precedence.

# RFC 037: Dynamic Context Compression & Local RAG

## 1. Overview
As Reticle scales to handle massive codebases, agents frequently hit strict token context limits (often 8k or 32k for fast local models). Feeding an entire repository into a prompt to answer a simple question results in catastrophic token overflow, high latency, and severe hallucination rates.

This RFC defines the architecture for the **Knowledge Retrieval Agent (`rag-agent`)**, which solves this problem by indexing workspaces into a persistent vector database and performing "Dynamic Context Compression"—synthesizing thousands of lines of code into highly dense summaries.

## 2. Local Vector Engine Architecture
To align with Reticle's philosophy of hardware acceleration and data privacy, the RAG engine must operate entirely locally without relying on external API endpoints (like OpenAI or Pinecone).

### Components:
- **Persistence Layer:** We utilize `ChromaDB`, configured as a `PersistentClient` targeting `d:\Reticle\runtime\vector_db`. This ensures the index survives orchestrator restarts.
- **Embedding Engine:** We mandate the use of native Python embeddings via `sentence-transformers/all-MiniLM-L6-v2`. This ensures sub-second embedding speeds using the local CPU/GPU, completely avoiding the network overhead and potential bottlenecks of passing text to an external Ollama or vLLM server that may be occupied with heavier generative tasks.

## 3. Data Lifecycle & Robust Memory Management
A naïve RAG implementation appends data indefinitely, leading to database bloat, stale context, and fatal ID collision errors upon re-indexing. The `rag-agent` must adhere to strict lifecycle primitives to ensure the vector space remains pristine.

### 3.1 Idempotent Indexing
When traversing a directory:
- Documents are split into 1000-character chunks with a 200-character overlap (to preserve function signatures and context).
- Each chunk is assigned a deterministic ID using an MD5 hash of its `source_path` and `start_index`.
- The agent **MUST** use `collection.upsert()` rather than `collection.add()`. If a user modifies a file and requests a re-index, the stable hash ensures the stale chunks are overwritten seamlessly rather than triggering a primary key crash.

### 3.2 Autonomous Purging Primitives
The agent is equipped with native tools to manage database hygiene:
- `reset_knowledge_base`: Drops the entire `workspace_index` collection. Useful when pivoting entirely between isolated, unrelated projects.
- `remove_path_from_index(path)`: Uses ChromaDB's `$contains` metadata operator to purge specific files or directories from the index. If a directory is deleted from disk, the agent can manually purge it from memory to prevent ghost-retrievals.

## 4. Synthesis vs. Raw Retrieval
A fatal flaw in standard orchestration RAG is "chunk dumping"—where the orchestrator simply pastes the top 5 raw code chunks directly into the prompt of the downstream worker (e.g., the `coder-agent`). 

This violates Context Density principles. Raw chunks often contain boilerplate imports, licensing comments, or half-cut logic.

The `rag-agent` acts as a middleman. It performs the vector query (`top_k=5`), reads the raw chunks, and then uses a rapid `litellm` ReAct loop to **synthesize** the data. The final output passed to the orchestrator is a compressed, human-readable summary of the exact logic requested, effectively compressing 10,000 tokens of raw retrieval down to 500 tokens of pure signal.

## 5. Pre-Flight Predictive Compression (The 3-Stage Heuristic)
While the `rag-agent` handles explicit codebase queries, downstream execution agents can still suffer catastrophic `400 Token Limit Exceeded` failures if the orchestrator passes them too much upstream history or a massive IDE context.

To prevent this, the `hermes.py` meta-scaffolder dynamically injects a **Pre-Flight Predictive Compression** interceptor into every Python worker. Right before a prompt is sent to `litellm`, the worker mathematically counts the exact token size. If the payload consumes >85% of the model's safe capacity, it triggers a 3-Stage heuristic fallback:

- **Stage 1 (Light Compression):** The agent strips the raw workspace directory tree mapping, saving thousands of tokens on large repositories. The agent can still use `list_dir` if it needs to navigate.
- **Stage 2 (Medium Compression):** The agent aggressively truncates the historical prompt iterations down to the last 1500 characters, maintaining only immediate short-term memory.
- **Stage 3 (Heavy Compression):** The agent engages a **Middle-Out** truncation algorithm on the upstream context payload. It mathematically reserves 20% of the remaining token budget for the beginning of the text, and 80% for the end, completely gutting the middle of the payload. (LLMs natively suffer from "Lost in the Middle" syndrome, so preserving the edges retains the highest density of useful signal).

This algorithmic guarantee ensures that the Reticle DAG *never* crashes from an accidental token overflow, even when dynamically shifting between an OpenAI 128k context model and a local Ollama 8k context model on the fly.
