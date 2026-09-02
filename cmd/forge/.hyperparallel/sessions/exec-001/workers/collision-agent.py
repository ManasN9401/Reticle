import sys, json, os, subprocess

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id", "unknown")
    inputs = req.get("inputs", [])
    
    system_prompt = 'Your task is to build a 2D Space Shooter Game in Python using Pygame. You are responsible for creating the manager.py file. Use Python 3 with pygame. Other agents are building player.py, enemy.py, and main.py, so import from those files. Implement collision detection using pygame.sprite.groupcollide to check player lasers hitting enemies, and enemy lasers hitting the player. Maintain a score counter, health pool, and trigger game-over states when player lives reach zero. Write fully implemented logic without placeholders.'
    
    user_prompt = ""
    for inp in inputs:
        user_prompt += f"Input '{inp.get('name')}':\n{inp.get('data')}\n\n"
        
    prompt = system_prompt + "\n\n" + user_prompt
    
    # We use a simple subprocess call to invoke the LLM via node or another CLI, or we can just mock it for this prototype
    # For now, we will just echo a placeholder response since we don't have the LLM bindings injected in this script
    # Wait, the Runtime injects 'llm_model' in parameters!
    
    # For this prototype, we'll just mock the LLM response to avoid complex binding setup.
    result_text = f"Mock response from collision-agent for task {req_id}"
    
    artifact = {
        "id": f"{req_id}_output",
        "name": "collision-agent Output",
        "type": "text/plain",
        "data": result_text
    }
    
    print(json.dumps({"id": req_id, "artifact": artifact}))

if __name__ == "__main__":
    main()
