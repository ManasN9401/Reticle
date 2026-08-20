import sys
import json
import time
import logging

logging.basicConfig(level=logging.ERROR)
logger = logging.getLogger(__name__)

def main():
    line = sys.stdin.readline()
    if not line:
        return
        
    req = json.loads(line)
    req_id = req.get("id")
    # req_id is passed as the execution ID in SubmitWorkflow. Wait, WaitlistManager submits with sessionID as execution ID.
    session_id = req.get("execution", req_id)
    
    # 1. Request Lock
    print(json.dumps({
        "action": "FileLockRequested",
        "path": "shared.txt",
        "session_id": session_id
    }), flush=True)
    
    # 2. Wait for IPC response from Orchestrator (Blocking)
    resp_line = sys.stdin.readline()
    if not resp_line:
        # Orchestrator closed pipe unexpectedly
        sys.exit(1)
        
    resp = json.loads(resp_line)
    if resp.get("status") != "FileLockGranted":
        logger.error(f"Failed to acquire lock: {resp}")
        sys.exit(1)
        
    # 3. We hold the lock! 
    # Simulate some artificial latency to force the other agent to hit the block
    time.sleep(1)
    
    # In a real agent, we would read `shared.txt` from disk here.
    # We will simulate reading and writing by just returning an artifact 
    # that the orchestrator or a downstream file_writer would write.
    
    artifact = {
        "id": f"{req_id}_stress_output",
        "name": "Stress Test Output",
        "type": "text/plain",
        "data": f"Session {session_id} successfully locked and modified the file!"
    }
    
    # 4. Release Lock
    print(json.dumps({
        "action": "FileLockReleased",
        "path": "shared.txt",
        "session_id": session_id
    }), flush=True)
    
    # 5. Final Output
    print(json.dumps({
        "id": req_id,
        "artifact": artifact
    }), flush=True)

if __name__ == "__main__":
    main()
