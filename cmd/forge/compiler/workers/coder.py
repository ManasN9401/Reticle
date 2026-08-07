"""
HyperParallel Worker Script
"""
import sys
import json

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    inputs = req.get("inputs", [])
    
    dag_json_str = ""
    for i in inputs:
        if i.get("name") == "DAG JSON":
            dag_json_str = i.get("data", "{}")
            break
            
    dag = json.loads(dag_json_str)
    
    generated_files = {}
    
    for agent in dag.get("agents", []):
        if not agent.get("is_new") and not agent.get("system_prompt"):
            continue
            
        agent_id = agent.get("id")
        sys_prompt = agent.get("system_prompt", "You are a helpful assistant.")
        sys_prompt += """\n\nTo write new files or edit existing files, you MUST include a JSON block in your response wrapped in ```json ... ``` with this exact structure:
{
  "file_operations": [
    {
      "action": "create",
      "path": "main.py",
      "content": "print('hello')"
    },
    {
      "action": "edit",
      "path": "main.py",
      "search": "old string to replace",
      "replace": "new string"
    }
  ]
}
All paths must be relative to the src/ directory."""
        
        code = f"""import sys, json, time, logging, os
from litellm import completion
from tenacity import retry, stop_after_attempt, wait_exponential, before_sleep_log

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

def main():
    line = sys.stdin.readline()
    if not line: return
    try:
        req = json.loads(line)
        req_id = req.get("id", "unknown")
        
        endpoints = [
            {{"model": "groq/llama-3.1-8b-instant", "api_key": os.environ.get("GROQ_API_KEY", "")}},
            {{"model": "groq/llama-3.1-8b-instant", "api_key": "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"}},
            {{"model": "openrouter/meta-llama/llama-3.1-8b-instruct:free", "api_key": os.environ.get("OPENROUTER_API_KEY", "")}},
            {{"model": "openrouter/meta-llama/llama-3.1-8b-instruct:free", "api_key": "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"}}
        ]
        
        @retry(stop=stop_after_attempt(10), wait=wait_exponential(multiplier=2, min=4, max=60), before_sleep=before_sleep_log(logger, logging.WARNING))
        def do_completion():
            import random
            random.shuffle(endpoints)
            last_err = None
            for ep in endpoints:
                try:
                    resp = completion(
                        model=ep["model"],
                        api_key=ep["api_key"],
                        messages=[
                            {{"role": "system", "content": {json.dumps(sys_prompt)}}},
                            {{"role": "user", "content": f"Process this request: {{json.dumps(req)}}"}}
                        ]
                    )
                    return resp
                except Exception as e:
                    last_err = e
                    logger.warning(f"Failed with {{ep['model']}}: {{e}}")
            raise last_err
            
        response = do_completion()
        result = response.choices[0].message.content
        
        artifact = {{
            "id": f"{{req_id}}_output",
            "name": "{agent_id} Output",
            "type": "document/markdown",
            "data": result
        }}
        print(json.dumps({{"id": req_id, "artifact": artifact}}))
    except Exception as e:
        print(f"ERROR: {{e}}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
"""
        generated_files[f"workers/{agent_id}.py"] = code.strip()
        
    artifact = {
        "id": f"{req_id}_output",
        "name": "Python Files",
        "type": "application/json",
        "data": json.dumps(generated_files)
    }
    
    print(json.dumps({
        "id": req_id,
        "artifact": artifact
    }))

if __name__ == "__main__":
    main()
