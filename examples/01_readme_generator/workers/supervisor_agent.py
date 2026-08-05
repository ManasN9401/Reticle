"""
HyperParallel Worker Script
MANDATORY READING:
- RFC-008: Agent Architecture
- RFC-027: Worker Runtime Contract
"""
import sys
import json
import uuid

def main():
    lines = sys.stdin.readlines()
    if not lines:
        return

    task = json.loads(lines[0])
    task_id = task.get("id")
    inputs = task.get("inputs", [])
    
    # We simulate a "State Machine" with iterations.
    # If the supervisor has NO inputs, it means it's the first time running.
    # It delegates to "outline-gen" and waits for a response.
    
    if not inputs:
        # Iteration 1: Delegate
        output = {
            "id": task_id,
            "artifact": {
                "id": str(uuid.uuid4()),
                "name": "supervisor-delegation",
                "type": "state/internal",
                "data": {"status": "delegating_to_worker"}
            },
            "graph_mutation": {
                "action": "delegate",
                "target_agent": "outline-gen",
                "return_to_supervisor": True
            }
        }
        print(json.dumps(output))
        return

    # If it HAS inputs, the worker returned its artifact back to the supervisor!
    # Iteration 2: Verification and Completion
    returned_artifact = inputs[0]
    
    output = {
        "id": task_id,
        "artifact": {
            "id": str(uuid.uuid4()),
            "name": "supervisor-final",
            "type": "document/verified",
            "data": {
                "status": "approved",
                "verified_content": returned_artifact.get("data")
            }
        }
    }
    print(json.dumps(output))

if __name__ == "__main__":
    main()
