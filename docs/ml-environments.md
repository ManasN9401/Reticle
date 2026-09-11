# ML environment selection

Assessment date: 2026-09-08. This document distinguishes the existing implementation from recommended changes; a hardware profile picker has not been implemented.

Users should select the execution environment and workload budget. A fixed NVIDIA requirement or universal minimum VRAM is inappropriate. A Ryzen 9 9900X and Radeon RX 7800 XT are reasonable candidates for classical ML, small neural-network experiments and some inference workloads; suitability for a particular model depends on data, precision, batch size, available memory and software support. This is an engineering assessment, not a measured training result on that GPU.

AMD's [current PyTorch installer](https://rocm.docs.amd.com/projects/ai-ecosystem/en/latest/frameworks/pytorch/install.html) separates GPU, operating system, ROCm, Python and PyTorch versions. The RX 7800 XT appears in AMD's device selection. Earlier Radeon documentation explicitly covers releases only through 7.2.1; its native-Windows list must not be used to declare the GPU unsupported by every newer release. Choose an exact supported combination and run a small GPU computation before advertising acceleration. Native Windows, WSL2 and Linux are distinct targets. No target was selected or GPU experiment run in this review.

## Current limitations

The ML skill manifest automatically installs unpinned `torch`, Hugging Face and other experiment packages into a worker environment. The skill text prefers PyTorch. This is a project default, not user-selectable framework provisioning. Installing generic `torch` does not establish an AMD-compatible GPU stack. Container jobs use a separate image, so host-installed libraries do not prepare that image.

`RETICLE_WORKER_IMAGE`, CPU/memory limits and command deadlines are configurable. `RETICLE_GPU_DEVICES` currently generates Docker `--gpus device=...`; it is not a complete ROCm adapter. AMD's Linux container examples use device mappings including `/dev/kfd` and `/dev/dri`. Do not advertise AMD support based solely on accepting a numeric GPU ID.

## Recommended implementation

Keep the orchestration SDK dependencies pinned independently of the experiment. Replace unconditional heavy skill dependencies with explicit, validated environment profiles: CPU, AMD ROCm, NVIDIA CUDA, and a user-supplied environment. Allow framework selection within profiles that actually support it. Each selected profile should resolve to a reproducible dependency lock or image digest, with recorded Python/framework/driver compatibility.

Expose device selection, RAM/VRAM budget, maximum duration and whether CPU fallback is allowed. Auto-detection should suggest a profile and report evidence; it must not silently change backend or download a large stack. Retain a small CPU option. Before admitting a job, verify device visibility, a tensor operation, allocation within budget and required model operations. Record dataset/model revisions, metrics and checkpoint/resume behavior with the result. Add separate device reservation and release at task completion/cancellation to avoid concurrent jobs exhausting one GPU.

These changes remain engineering work. Changing documentation or `.env` alone does not implement environment selection, ROCm device forwarding or resource scheduling.
