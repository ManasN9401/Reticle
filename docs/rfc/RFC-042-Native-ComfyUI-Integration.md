---
status: historical
owner: Reticle Project
updated: 2026-09-19
---

> Historical design record. Current versioned specifications and schemas take precedence.

> Implementation update (2026-09-08): this historical proposal is superseded where it conflicts with RFC-043 and the current specifications under docs/specifications/. See the audit repair ledger for remaining capability limits.

# RFC-042: Native ComfyUI Integration (Local Asset Generation)

## 1. Objective
To allow agents (specifically the Frontend Agent) to natively generate high-quality image assets using local hardware accelerators (GPU) via a direct integration with ComfyUI, avoiding external API costs for image generation.

## 2. Implementation

### 2.1 Virtual Image Modality
The routing matrix automatically populates a virtual local model: `{ID: "comfyui/default", Modality: "image", APIKeyEnv: "COMFYUI_HOST"}`. This ensures the orchestrator recognizes local image generation capabilities.

### 2.2 The `generate_local_asset` Python Tool
Inside `comfy_tools.py`, the `frontend-agent` exposes a native `generate_local_asset` Python function.
When the LLM intends to generate an image:
1. It calls the tool with a text prompt and an absolute destination path.
2. The function constructs an SDXL-compatible JSON workflow payload targeting `http://127.0.0.1:8188`.
3. The function uses the `CheckpointLoaderSimple` node set to a standard SDXL checkpoint (e.g. `DreamShaperXL_Turbo_v2_1.safetensors`).
4. It polls the ComfyUI history endpoint until the generation is complete.
5. It retrieves the raw image bytes and saves them directly to the `output_path` inside the workspace.

### 2.3 Batch generation and image editing
`generate_local_asset` accepts only `prompt` and `output_path` as required arguments; `checkpoint`, `width` and `height` are optional. Because each generated image would otherwise cost a model turn, `generate_local_assets` takes up to 12 `{prompt, output_path}` items, runs them serially in one GPU session and reports `{"generated": [...], "failed": [...]}`; items fail independently. `image_tools.py` adds `resize_image`, `make_thumbnails`, `convert_image` and `crop_image`, which use Pillow (declared by the `frontend-uiux` skill), resolve every path with `safe_path`, accept only PNG, JPEG and WebP, and refuse sources above 64 megapixels or 50 MiB. All of these sit behind the `image.local` capability, and every file they write counts toward the node's declared outputs. The per-task turn budget defaults to 60 for nodes with `image.local` and can be set with `llm_max_iterations`.

## 3. Requirements
- A local instance of ComfyUI running on `127.0.0.1:8188`.
- The corresponding SDXL `.safetensors` checkpoint file located in the `models/checkpoints/` directory.
