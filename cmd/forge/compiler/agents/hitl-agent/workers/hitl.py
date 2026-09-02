import os
import sys
import json
import time
import uuid

def emit_log(msg):
    print(f"[TOOL] {msg}", flush=True)

def ui_state(state):
    print(f"[UI_STATE: {state}]", flush=True)

def run_agent(payload):
    # Check for auto-approve bypass
    if payload.get("auto-approve", False) or payload.get("auto_approve", False):
        emit_log("Auto-approved via flag. Bypassing human checkpoint.")
        sys.exit(0)

    task_prompt = payload.get("prompt", "Human authorization required.")
    context_files = payload.get("context", [])

    task_id = str(uuid.uuid4())[:8]
    checkpoint_dir = r"d:\HyperParallel\runtime\checkpoints"
    os.makedirs(checkpoint_dir, exist_ok=True)
    
    checkpoint_path = os.path.join(checkpoint_dir, f"approval_req_{task_id}.md")
    
    content = f"""# 🛑 HUMAN APPROVAL REQUIRED

## Task Context
{task_prompt}

## Proposed Plan
"""
    if isinstance(context_files, list):
        for c in context_files:
            content += f"- {c}\n"
    elif isinstance(context_files, str):
        content += f"{context_files}\n"
    else:
        content += f"{json.dumps(context_files, indent=2)}\n"
        
    content += """

---
## Authorization
Change PENDING to APPROVED to authorize execution. Change to REJECTED to halt execution and return feedback to the Architect.

STATUS: PENDING
FEEDBACK: 
"""
    
    with open(checkpoint_path, "w", encoding="utf-8") as f:
        f.write(content)
        
    emit_log(f"Checkpoint file created at: {checkpoint_path}")
    ui_state("WAITING_HUMAN")
    
    # Polling Loop
    while True:
        time.sleep(2)
        if not os.path.exists(checkpoint_path):
            emit_log("Checkpoint file deleted. Assuming REJECTED.")
            sys.exit(1)
            
        with open(checkpoint_path, "r", encoding="utf-8") as f:
            current_content = f.read()
            
        status = "PENDING"
        feedback = ""
        
        for line in current_content.split("\n"):
            if line.startswith("STATUS:"):
                status = line.split("STATUS:")[1].strip().upper()
            if line.startswith("FEEDBACK:"):
                feedback = line.split("FEEDBACK:")[1].strip()
                
        if status == "APPROVED":
            emit_log("Human Authorized: APPROVED. Continuing DAG execution.")
            ui_state("RESUMED")
            try:
                os.remove(checkpoint_path)
            except Exception:
                pass
            sys.exit(0)
            
        elif status == "REJECTED":
            emit_log(f"Human Authorized: REJECTED. Feedback: {feedback}")
            ui_state("RESUMED")
            print(f"Feedback from User: {feedback}")
            try:
                os.remove(checkpoint_path)
            except Exception:
                pass
            sys.exit(1)

if __name__ == "__main__":
    if len(sys.argv) > 1:
        with open(sys.argv[1], "r", encoding="utf-8") as f:
            payload = json.load(f)
        run_agent(payload)
    else:
        print("Error: No payload provided.")
        sys.exit(1)
