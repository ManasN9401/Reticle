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
from tenacity import retry, stop_after_attempt, wait_exponential, before_sleep_log
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

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

If the global agents can fulfill the task, use them (set `is_new: false`). You MUST ONLY use `is_new: false` for agents that exactly match an ID in the AVAILABLE AGENTS list below.
If you need new specialized agents (which you almost certainly will), design them (set `is_new: true`).
For ALL new agents, you MUST provide an `id`, `name`, `description`, and a highly detailed `system_prompt`.

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
    }}
  ],
  "nodes": [
    {{ "id": "node-1", "agent_id": "agent-id" }}
  ],
  "edges": [
    {{ "from": "node-1", "to": "node-2" }}
  ]
}}

CRITICAL: Every node in the `nodes` array MUST have a valid `agent_id` that EXACTLY matches the `id` of an agent defined in the `agents` list or the AVAILABLE AGENTS list. NEVER leave `agent_id` blank or null.
CRITICAL: Every edge in the `edges` array MUST reference `from` and `to` nodes that EXACTLY match the `id` of a node defined in the `nodes` array. NEVER reference a node that does not exist.

Output ONLY the raw JSON. Do not output markdown code blocks.
"""

        endpoints = [
            {"model": "groq/llama-3.1-8b-instant", "api_key": os.environ.get("GROQ_API_KEY", "")},
            {"model": "groq/llama-3.1-8b-instant", "api_key": "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"},
            {"model": "openrouter/meta-llama/llama-3.1-8b-instruct:free", "api_key": os.environ.get("OPENROUTER_API_KEY", "")},
            {"model": "openrouter/meta-llama/llama-3.1-8b-instruct:free", "api_key": "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"}
        ]

        @retry(stop=stop_after_attempt(10), wait=wait_exponential(multiplier=2, min=4, max=60), before_sleep=before_sleep_log(logger, logging.WARNING))
        def get_architect_response():
            import random
            random.shuffle(endpoints)
            last_err = None
            
            for ep in endpoints:
                try:
                    resp = completion(
                        model=ep["model"],
                        api_key=ep["api_key"],
                        response_format={ "type": "json_object" },
                        messages=[
                            {"role": "system", "content": system_msg},
                            {"role": "user", "content": f"Design the agent graph for this goal: {user_prompt}"}
                        ]
                    )
                    return resp
                except Exception as e:
                    last_err = e
                    logger.warning(f"Failed with {ep['model']}: {e}")
                    
            raise last_err
            
            # Programmatic Validation to enforce the strict constraints
            try:
                data = json.loads(resp.choices[0].message.content)
                
                # Gather valid agent IDs
                valid_agents = set()
                if available_agents and available_agents != "None":
                    valid_agents.update([a.strip() for a in available_agents.split(',')])
                for a in data.get("agents", []):
                    if a.get("id"):
                        valid_agents.add(a["id"])
                
                # Gather valid node IDs and check agent assignments
                valid_nodes = set()
                for n in data.get("nodes", []):
                    if not n.get("id"):
                        raise ValueError("Node is missing 'id'")
                    valid_nodes.add(n["id"])
                    
                    if not n.get("agent_id") or n["agent_id"] == "None":
                        raise ValueError(f"Node {n['id']} is missing 'agent_id'")
                    if n["agent_id"] not in valid_agents:
                        raise ValueError(f"Node {n['id']} references unknown agent: {n['agent_id']}")
                
                # Check edges and build adjacency list
                adj = {n: [] for n in valid_nodes}
                in_degree = {n: 0 for n in valid_nodes}
                
                for e in data.get("edges", []):
                    from_node = e.get("from")
                    to_node = e.get("to")
                    if from_node not in valid_nodes:
                        raise ValueError(f"Edge references unknown from node: {from_node}")
                    if to_node not in valid_nodes:
                        raise ValueError(f"Edge references unknown to node: {to_node}")
                    adj[from_node].append(to_node)
                    in_degree[to_node] += 1
                    
                # Cycle detection using Kahn's algorithm
                queue = [n for n in valid_nodes if in_degree[n] == 0]
                visited_count = 0
                while queue:
                    curr = queue.pop(0)
                    visited_count += 1
                    for neighbor in adj[curr]:
                        in_degree[neighbor] -= 1
                        if in_degree[neighbor] == 0:
                            queue.append(neighbor)
                            
                if visited_count != len(valid_nodes):
                    raise ValueError("Graph contains a cycle! Directed Acyclic Graph (DAG) requirement violated.")
                        
            except Exception as e:
                raise Exception(f"Validation failed: {str(e)}") # This triggers the @retry
                
            return resp
            
        response = get_architect_response()
        
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
