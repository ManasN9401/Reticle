---
name: RAG Context Summarization
description: Methodology for local vector indexing and context reduction.
---

# RAG & Context Compression Methodology

This skill equips the agent to act as a **Knowledge Retrieval Agent**, navigating vast codebases and documentation and compressing them into digestible summaries for downstream agents that have strict 8k context limits.

## 1. Indexing Strategy
When instructed to index a directory:
- **Chunk Size:** Documents and code must be chunked into blocks of ~1000 characters.
- **Overlap:** Ensure a 200-character overlap between chunks so that functions or sentences are not cleanly severed in the middle of important logic.
- **Persistence:** All embeddings must be stored in the designated ChromaDB directory: `d:\Reticle\runtime\vector_db`.

## 2. Retrieval Strategy
When querying for information:
- Use `sentence-transformers/all-MiniLM-L6-v2` as the embedding function natively in Python to ensure maximum performance on local hardware without depending on external API servers.
- Retrieve the top 5-10 most relevant chunks (`top_k=10`).

## 3. Summarization (Compression)
Do not just blindly return raw chunks. 
- You MUST synthesize the retrieved chunks into a cohesive, highly dense summary that answers the user's specific query.
- Use your internal LLM to condense the technical details, throwing away irrelevant boilerplate found in the chunks.
- Your final output will often be injected directly into the prompt of another agent (like the Coder), so keep the signal-to-noise ratio extremely high.
