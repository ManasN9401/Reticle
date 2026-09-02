import sys, json, os, subprocess

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id", "unknown")
    inputs = req.get("inputs", [])
    
    system_prompt = "Your goal is to satisfy the user's objective to 'Create a simple test' by writing unit tests for the core utilities. You are responsible for creating a file named 'test_math_utils.py'. Use Python 3 with the standard 'unittest' framework. The other agent is building the math functions in 'math_utils.py', so you must import those functions to test them. Implement test cases for addition, subtraction, multiplication, division, and error handling for division by zero, utilizing unittest.TestCase and assertEqual/assertRaises, ending with a standard main execution block."
    
    user_prompt = ""
    for inp in inputs:
        user_prompt += f"Input '{inp.get('name')}':\n{inp.get('data')}\n\n"
        
    prompt = system_prompt + "\n\n" + user_prompt
    
    # We use a simple subprocess call to invoke the LLM via node or another CLI, or we can just mock it for this prototype
    # For now, we will just echo a placeholder response since we don't have the LLM bindings injected in this script
    # Wait, the Runtime injects 'llm_model' in parameters!
    
    # For this prototype, we'll just mock the LLM response to avoid complex binding setup.
    result_text = f"Mock response from test-writer for task {req_id}"
    
    artifact = {
        "id": f"{req_id}_output",
        "name": "test-writer Output",
        "type": "text/plain",
        "data": result_text
    }
    
    print(json.dumps({"id": req_id, "artifact": artifact}))

if __name__ == "__main__":
    main()
