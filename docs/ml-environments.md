# ML environment selection

Assessment date: 2026-09-08. Implementation update: 2026-09-17. The runtime now accepts an explicit environment profile; host compatibility and experiment quality still require measured validation.

Users should select the execution environment and workload budget. A fixed NVIDIA requirement or universal minimum VRAM is inappropriate. A Ryzen 9 9900X and Radeon RX 7800 XT are reasonable candidates for classical ML, small neural-network experiments and some inference workloads; suitability for a particular model depends on data, precision, batch size, available memory and software support. This is an engineering assessment, not a measured training result on that GPU.

The current project target is native Windows. Reticle auto-detects native Windows, WSL2 or Linux and warns when the chosen profile needs a different execution path. It does not silently switch the OS target or accelerator family.

AMD's [current PyTorch installer](https://rocm.docs.amd.com/projects/ai-ecosystem/en/latest/frameworks/pytorch/install.html) separates GPU, operating system, ROCm, Python and PyTorch versions. At this update, AMD's [native Windows matrix](https://rocm.docs.amd.com/projects/radeon-ryzen/en/latest/docs/compatibility/compatibilityrad/windows/windows_compatibility.html) does not list the RX 7800 XT, and the [Windows limitations](https://rocm.docs.amd.com/projects/radeon-ryzen/en/latest/docs/limitations/limitationsrad.html) say ML training is unsupported. Older WSL documentation has listed the RX 7800 XT, but that is a different target. Reticle therefore warns on the native Windows AMD profile and does not claim supported acceleration. Recheck the current matrix before installing because AMD's support changes over time.

## Current limitations

The orchestration SDK uses exact direct pins. Registry skills support `locked`, `floating` and `profile` dependency policies; the ML skill uses `profile`, so experiment frameworks belong in the selected prepared image or user-supplied environment. Reticle does not silently install a generic GPU stack. The ML skill text prefers PyTorch, but profile selection does not establish that an image contains an AMD-compatible build.

`RETICLE_ML_PROFILE` selects `cpu`, `amd-rocm`, `nvidia-cuda` or `user`. `RETICLE_WORKER_IMAGE`, CPU/memory limits and command deadlines remain configurable. Accelerator profiles require numeric `RETICLE_GPU_DEVICES`. NVIDIA uses Docker GPU selection; Linux/WSL AMD maps `/dev/kfd` and `/dev/dri` and adds the video group. Native Windows AMD commands require explicitly enabled native execution and produce a compatibility warning. The runtime leases selected devices between its own attempts. This wiring is not proof that the OS, driver, ROCm/CUDA release, image and framework are compatible.

## Remaining implementation and acceptance work

Resolve every production profile to a reviewed transitive dependency lock or image digest, with recorded Python/framework/driver compatibility. A future UI may present these choices, but the environment variables are already the authoritative runtime input.

Add explicit RAM/VRAM budget and CPU-fallback policy. Auto-detection may suggest a profile and report evidence; it must not silently change backend or download a large stack. Before admitting a real experiment, verify device visibility, a tensor operation, allocation within budget and required model operations. Record dataset/model revisions, metrics and checkpoint/resume behavior with the result. The current device lease is process-local; a host-wide or remote resource manager remains necessary for multiple runtimes.

No live GPU smoke test was run in this review. The selected native Windows/RX 7800 XT path remains an experimental, warning-gated inference path unless AMD adds the card to the current matrix. CPU jobs remain supported, and WSL2/Linux can be selected later without changing Reticle's profile contract.
