# Creating Custom Agents in HyperParallel

HyperParallel allows you to define highly specialized autonomous agents by simply authoring a YAML file and a corresponding Python worker script.

## 1. Create the Agent Profile
Create a new file in `compiler/agents/` (e.g., `compiler/agents/security_auditor.yaml`):

```yaml
id: security-auditor
name: "Security Auditor"
description: "Scans code for OWASP vulnerabilities"
version: 1.0.0
runtime: python
entrypoint: workers/security_auditor.py
```

## 2. Write the Worker Logic
Create the Python worker script in `compiler/workers/security_auditor.py`. Your script MUST follow the HyperParallel `stdin/stdout` contract:

1. Read a single JSON line from `sys.stdin`.
2. Extract the `inputs` and `memory` payload.
3. Perform your logic (e.g., calling an LLM via `litellm`).
4. Print exactly one JSON object to `sys.stdout` containing your `artifact`.

```python
import sys
import json
import logging
from litellm import completion

logging.basicConfig(level=logging.ERROR)

def main():
    line = sys.stdin.readline()
    if not line: return
    req = json.loads(line)
    
    # Extract upstream context
    source_code = ""
    for inp in req.get("inputs", []):
        source_code += inp.get("data", "")
        
    # Run specialized logic
    resp = completion(
        model="gemini/gemini-3.5-flash",
        messages=[{"role": "user", "content": f"Find vulnerabilities in:\\n{source_code}"}]
    )
    
    # Broadcast result
    print(json.dumps({
        "id": req.get("id"),
        "artifact": {
            "id": f"{req.get('id')}_audit",
            "name": "Security Audit Report",
            "type": "text/plain",
            "data": resp.choices[0].message.content
        }
    }))

if __name__ == "__main__":
    main()
```

## 3. Registering the Agent
Simply drop the `.yaml` file into the `agents/` directory. `forge.exe` automatically hot-reloads agents on startup using the `registry.LoadAgents()` pipeline. You can now reference `security-auditor` in your DAG workflows!
