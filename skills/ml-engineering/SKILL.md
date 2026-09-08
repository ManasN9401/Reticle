---
name: ml-engineering
description: Build and verify PyTorch experiments with explicit data, hardware, environment and recovery requirements.
---
# Machine learning engineering

Use PyTorch and the selected Hugging Face libraries for this project's deep-learning work. This is a project consistency choice, not a claim that mixing frameworks necessarily crashes.

Distinguish generated code, a small smoke run, and a full experiment. Discover the actual device and available memory before choosing batch size, precision or accumulation. Do not assume 16 GB or promise that a small batch cannot run out of memory. Accumulation is optional and must preserve the intended effective batch/optimizer behavior.

Record dataset revision, preprocessing, split construction, baseline, evaluation metrics, model revision, dependency versions and hardware. Avoid train/evaluation leakage. Synthetic data is suitable for smoke checks, not benchmark accuracy claims.

Initialize Python, NumPy and Torch RNGs by calling the seed initializer. Configure deterministic algorithms, backend flags and data-loader seeding where appropriate; report unsupported operations and performance tradeoffs. Verify repeatability within stated tolerances in the recorded environment. Cross-platform and cross-version bitwise reproducibility is not guaranteed.

Keep visible local metrics/logs. External experiment tracking is optional and requires its own configured credentials. Save checkpoints containing the state needed for the advertised resume behavior, including optimizer/scheduler/RNG state when resuming training. Test interruption/resumption on a small fixture before running a long job.

Honor the execution environment's resource/time limits. A full training run requires a configured training environment and explicit budget. Never turn iteration exhaustion, missing CUDA, missing dependencies or a failed evaluation into success.

Reference: https://docs.pytorch.org/docs/2.9/notes/randomness.html
