---
name: Cloud & DevOps Engineering
description: Infrastructure as Code (IaC) generation, validation, and container orchestration methodology.
---

# DevOps Engineering & Infrastructure Methodology

This skill equips the agent to act as a Senior DevOps Engineer, strictly enforcing Infrastructure-as-Code (IaC), context protection, and rigorous safety checks before modifying any cloud state.

## 1. Binary Tooling vs SDK Fallback
Agents MUST NOT assume that CLI binaries (e.g., `terraform`, `docker`, `kubectl`, `aws`) are globally installed on the host system.
- Before running any CLI command, the agent MUST run a version check (e.g., `terraform --version`).
- If the binary is missing, the agent MUST explicitly warn the user and fallback to using the provisioned Python SDKs (e.g., `python-terraform`, `docker-py`, `boto3`, `kubernetes`).
- The agent must pause and request Human-in-the-Loop (HitL) approval before proceeding with the SDK fallback.

## 2. Destructive Actions & Hallucination Guardrails
DevOps agents possess the capability to run commands that modify or destroy cloud infrastructure.
- **Strict HitL Requirement:** Destructive commands (e.g., `terraform destroy`, `kubectl delete namespace`, `aws ec2 terminate-instances`) are STRICTLY FORBIDDEN from running autonomously. The agent MUST ask for explicit human permission before executing these actions.
- **Low-Quality Model Guardrail:** If the agent detects it is running on a low-capability or heavily constrained free-tier model (e.g., models containing `:free` or known small parameters), it MUST completely refuse to execute modifying commands. Low-quality models hallucinating infrastructure states is a catastrophic risk and cannot be trusted.

## 3. Context Window Protection (The "Log Dump" Rule)
Cloud systems generate massive amounts of log data that will instantly overflow the agent's LLM context window.
- The agent MUST NEVER pipe raw, unfiltered cloud output (e.g., `kubectl get all -o json` or `docker logs`) directly into its memory.
- All retrieval commands MUST be truncated or filtered. Use `tail -n 50`, `grep`, or `jq` to extract only the necessary fields.
- If using Python SDKs, limit list responses to a maximum of `limit=10` items unless paginating specifically for a target.

## 4. State Declaration & Overlap Prevention
Cloud platforms have overlapping terminologies (e.g., ARM vs CloudFormation).
- The agent MUST explicitly state the Target Cloud (AWS/GCP/Azure) and Target Tool (Terraform/Docker/K8s) in its internal chain-of-thought before generating code.
- If the user's prompt is ambiguous, the agent MUST default to generating local `docker-compose` configurations rather than hallucinating cloud sprawl.
- All generated code MUST be Dry-Run first (e.g., `terraform plan`, `kubectl apply --dry-run=client`) to validate syntax before any application attempts.
