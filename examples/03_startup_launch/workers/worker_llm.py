import sys
import json
import os
from litellm import completion

def main():
    line = sys.stdin.readline()
    if not line:
        return
        
    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        parameters = req.get("parameters", {})
        inputs = req.get("inputs", [])
        
        system_prompt = parameters.get("system_prompt", "You are a helpful assistant.")
        user_prompt = parameters.get("user_prompt", "Complete the task.")
        llm_model = parameters.get("llm_model", "groq/llama-3.1-8b-instant")
        
        # Check ECC
        ecc_agent_file = parameters.get("ecc_agent", None)
        if ecc_agent_file:
            ecc_path = os.path.expanduser(f"~/.gemini/config/plugins/everything-claude-code/agents/{ecc_agent_file}")
            if os.path.exists(ecc_path):
                with open(ecc_path, "r", encoding="utf-8") as f:
                    system_prompt += "\n\n" + f.read()
                    
        # Append input artifacts
        if inputs:
            user_prompt += "\n\nINPUT DATA:\n"
            for inp in inputs:
                user_prompt += f"--- {inp.get('name', inp.get('artifact_id', 'Input'))} ---\n"
                user_prompt += str(inp.get("data", "")) + "\n"
        
        messages = [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt}
        ]
        
        try:
            print(f"[{req_id}] Calling LLM: {llm_model}...", file=sys.stderr)
            response = completion(model=llm_model, messages=messages)
            result_text = response.choices[0].message.content
        except Exception as e:
            print(f"[{req_id}] LLM call failed (missing API key?). Falling back to mock data. Error: {e}", file=sys.stderr)
            result_text = f"**Mocked Content for {req_id}**\n\nThe brave hero ventured into the dark forest, seeking the legendary artifact. Along the way, they encountered a mysterious stranger who offered guidance. The journey was long and perilous, but in the end, they found what they were looking for."
        
        artifact = {
            "id": parameters.get("output_id", f"{req_id}_output"),
            "name": parameters.get("output_name", "LLM Output"),
            "type": "document/markdown",
            "data": result_text
        }
        
        resp = {
            "id": req_id,
            "artifact": artifact
        }
        print(json.dumps(resp))
        
    except Exception as e:
        print(f"ERROR: {str(e)}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
