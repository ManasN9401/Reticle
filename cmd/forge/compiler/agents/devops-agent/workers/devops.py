import sys
import json
import logging
import shutil
import subprocess

logging.basicConfig(level=logging.ERROR)

def is_low_quality_model(model_id):
    if not model_id:
        return True
    model_id = model_id.lower()
    # Check for free tier, mini, or known small models that shouldn't touch infrastructure
    if ":free" in model_id or "mini" in model_id or "flash-lite" in model_id or "8b" in model_id:
        return True
    return False

def check_binary(binary_name):
    return shutil.which(binary_name) is not None

def main():
    line = sys.stdin.readline()
    if not line:
        return
        
    try:
        req = json.loads(line)
    except Exception:
        return

    task_id = req.get("id", "unknown_task")
    params = req.get("parameters", {})
    llm_model = params.get("llm_model", "")
    
    # 1. Hallucination Guardrail Check
    if is_low_quality_model(llm_model):
        print(json.dumps({
            "id": task_id,
            "error": f"SECURITY_BLOCK: Model '{llm_model}' is flagged as low-quality or free-tier. It is strictly forbidden from executing DevOps tasks to prevent catastrophic infrastructure hallucinations."
        }))
        sys.exit(1)

    # 2. Binary Tooling Check
    missing_binaries = []
    for tool in ["terraform", "docker", "kubectl"]:
        if not check_binary(tool):
            missing_binaries.append(tool)
            
    warnings = ""
    if missing_binaries:
        warnings = f"WARNING: Missing CLI binaries for {', '.join(missing_binaries)}. The agent must fallback to Python SDKs (boto3, docker-py) and MUST request Human-in-the-Loop approval before proceeding.\n"

    # In a real scenario, this is where we would call litellm with tools for IaC generation
    # For now, we simulate the agent outputting a safety-checked response.
    
    output_message = f"{warnings}DevOps Agent initialized successfully on model {llm_model}. Ready to generate IaC templates using strict state declaration and dry-run validation."

    # Broadcast result
    print(json.dumps({
        "id": task_id,
        "artifact": {
            "id": f"{task_id}_iac_status",
            "name": "Infrastructure Status Report",
            "type": "text/plain",
            "data": output_message
        }
    }))

if __name__ == "__main__":
    main()
