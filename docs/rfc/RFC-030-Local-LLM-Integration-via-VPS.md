# RFC-030: Local LLM Integration via VPS (Shelved)

## Status
Shelved (Temporarily)

## Context
API Rate limits on free-tier providers severely bottleneck `forge.exe` when orchestrating parallel agent graphs. To bypass cloud rate limits, a strategy was proposed to run a massive open-source MoE (Mixture of Experts) model locally. Specifically, the C99 implementation of the "Kimi-K3" 2.78 Trillion parameter model. 

## Proposal
- Provision an Ubuntu Storage-Optimized VPS (e.g., AWS `i4i.2xlarge`) with 64GB+ RAM and 1.9TB Local NVMe SSD.
- Execute `setup_vps.sh` to clone the C99 engine, natively compile it for Linux x86-64, and download the 1.56 TB model shards via HuggingFace.
- Run `vps_server.py`, a lightweight Flask wrapper around the `k3` binary that exposes an OpenAI-compatible `/v1/chat/completions` endpoint.
- Configure `forge.exe` Python workers to intercept requests and route them to `KIMI_VPS_URL` using `litellm`.

## Reasons for Shelving
While architecturally sound, the C99 CPU-based MoE model operates at ~25 seconds per token. For an autonomous multi-agent coding framework that relies on generating thousands of tokens across wide parallel graphs, this throughput is unacceptably slow. The setup scripts and Python wrappers were reverted from `main`.

## Future Work
Revisit this integration strategy using **Option 2**: A GPU-based VPS (e.g., Paperspace via DigitalOcean) running `vLLM` or `Ollama` with smaller, highly optimized coding models (like `Llama-3.3-70B` or `Qwen-2.5-Coder`), which provide instant token generation.
