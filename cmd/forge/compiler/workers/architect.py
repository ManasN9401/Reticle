"""
HyperParallel Worker Script
MANDATORY READING:
- RFC-008: Agent Architecture
- RFC-027: Worker Runtime Contract
"""
import sys
import json
import os
from litellm import completion

def main():
    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    mem = req.get("memory", {})
    user_prompt = mem.get("user_prompt", "")
    available_agents = mem.get("available_agents", "None")
    
    try:
        print(f"[{req_id}] Architecting DAG...", file=sys.stderr)
        
        system_msg = f"""
You are the Chief Software Architect of HyperParallel.
The user will provide a software goal (e.g. 'Build a game', 'Analyze data').
Your job is to design a Directed Acyclic Graph (DAG) of agents to achieve this.

If the global agents can fulfill the task, use them (set `is_new: false`).
If you need new specialized agents, design them (set `is_new: true`).
For new agents, provide an `id`, `name`, `description`, and a highly detailed `system_prompt`.

AVAILABLE AGENTS:
{available_agents}

CRITICAL REQUIREMENT: You MUST build highly interconnected multi-agent pipelines with PARALLEL branches. 
For example, instead of a linear pipeline, have [Researcher 1, Researcher 2] run in PARALLEL and both feed their outputs simultaneously into an [Analyst], which then feeds into a [Writer] and [Auditor].
If your graph is just a linear chain (A -> B -> C -> D), or only has 1 or 2 isolated nodes, you have FAILED. 
Leverage the parallel nature of the system!

Return the DAG strictly as JSON with the following schema, and NOTHING else (no markdown blocks, just raw JSON):
{{
  "workflow_name": "Name of workflow",
  "agents": [
    {{
      "id": "agent-id",
      "name": "Human Readable Name",
      "description": "Short description",
      "is_new": true, 
      "system_prompt": "Highly detailed prompt explaining their job..."
    }},
    {{
      "id": "auditor-agent",
      "is_new": false
    }}
  ],
  "nodes": [
    {{ "id": "node-1", "agent_id": "agent-id" }},
    {{ "id": "node-2", "agent_id": "auditor-agent" }}
  ],
  "edges": [
    {{ "from": "node-1", "to": "node-2" }}
  ]
}}
"""

        response = completion(
            model="groq/llama-3.3-70b-versatile",
            messages=[
                {"role": "system", "content": system_msg},
                {"role": "user", "content": user_prompt}
            ],
            response_format={ "type": "json_object" }
        )
        
        result = response.choices[0].message.content
        
        artifact = {
            "id": f"{req_id}_output",
            "name": "DAG JSON",
            "type": "application/json",
            "data": result
        }
        
        print(json.dumps({
            "id": req_id,
            "artifact": artifact
        }))
        
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
