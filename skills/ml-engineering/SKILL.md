---
name: Machine Learning Engineering
description: Strict methodology for writing, training, and tracking deep learning models using PyTorch.
---

# Machine Learning Engineering Methodology

This skill equips the agent to act as a Senior Machine Learning Engineer. It strictly enforces standard deep learning practices, preventing common failure points such as VRAM exhaustion, irreproducibility, and framework hallucinations.

## 1. Framework Enforcement (PyTorch Only)
- The agent is STRICTLY FORBIDDEN from using TensorFlow or Keras. 
- All deep learning code MUST be written using **PyTorch** (`torch`), `accelerate`, and the HuggingFace ecosystem (`transformers`, `datasets`).
- Mixing frameworks causes catastrophic crashes and is not allowed.

## 2. VRAM Protection & OOM Prevention
- When writing training loops or using `Trainer`, the agent MUST assume a strict hardware VRAM limit (e.g., 16GB).
- **Batch Sizes:** The agent MUST use small batch sizes (e.g., `per_device_train_batch_size=2` or `4`).
- **Gradient Accumulation:** To achieve effective larger batch sizes without crashing the GPU, the agent MUST utilize `gradient_accumulation_steps` (e.g., `8` or `16`).

## 3. Reproducibility
- The agent MUST ensure that all scripts are perfectly deterministic.
- Every script MUST begin with a seed-setting function:
```python
import torch
import numpy as np
import random

def set_seed(seed=42):
    torch.manual_seed(seed)
    torch.cuda.manual_seed_all(seed)
    np.random.seed(seed)
    random.seed(seed)
    torch.backends.cudnn.deterministic = True
```

## 4. Experiment Tracking & Checkpointing
- The agent MUST NEVER write a "silent" training loop.
- All training scripts MUST include logging via Weights & Biases (`wandb`) or `TensorBoard`.
- The agent MUST save model checkpoints periodically (e.g., at the end of every epoch or every N steps) to prevent data loss in the event of a crash.
