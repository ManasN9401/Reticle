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
    real_stdout = sys.stdout
    sys.stdout = sys.stderr

    import litellm
    litellm.suppress_debug_info = True

    line = sys.stdin.readline()
    if not line: return
    
    req = json.loads(line)
    req_id = req.get("id")
    mem = req.get("memory", {})
    user_prompt = mem.get("user_prompt", "")
    available_agents = mem.get("available_agents", "None")
    
    try:
        print(f"[{req_id}] Architecting DAG...", file=sys.stderr)
        
        available_agents_prompt = available_agents if available_agents and available_agents.strip() != "None" else "None. You MUST create all new specialized agents (set is_new: true for ALL agents)."

        system_msg = f"""
You are the Chief Software Architect of HyperParallel.
The user will provide a software goal (e.g. 'Build a game', 'Analyze data').
Your job is to design a Directed Acyclic Graph (DAG) of agents to achieve this.

If the global agents can fulfill the task, use them (set `is_new: false`). You MUST ONLY use `is_new: false` for agents that exactly match an ID in the AVAILABLE AGENTS list below.
If you need new specialized agents (which you almost certainly will), design them (set `is_new: true`).
For ALL new agents, you MUST provide an `id`, `name`, `description`, and a highly detailed `system_prompt`.

AVAILABLE AGENTS:
{available_agents_prompt}

CRITICAL REQUIREMENT: You MUST categorize the complexity of the user's task. If the user's task is highly complex (e.g. building an app or a game), you MUST decompose it into wide, parallel pipelines (multiple agents running simultaneously), and one of your agents MUST explicitly be responsible for creating the main entrypoint (e.g., 'main.py' or 'index.js'). However, if the task is simple, a simple linear 1 or 2-node graph is perfectly acceptable.

CRITICAL INSTRUCTION: Your agents MUST be given extremely specific technical constraints. They are free to create non-code assets (e.g. markdown for lore, audio specs, pixel art grids), but for code, they MUST build everything from scratch using standard libraries (e.g. Python and 'pygame'). Do NOT let them hallucinate or import external imaginary engines (like 'import game_engine' or 'import engine'). Do NOT instruct them to "run" the game.

Return the DAG strictly as JSON with the following schema, and NOTHING else (no markdown blocks, just raw JSON):
{{
  "complexity_analysis": "complex", // MUST be exactly "simple" or "complex"
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
            {"model": "groq/llama-3.1-8b-instant", "api_key": os.environ.get("GROQ_API_KEY_2", "")},
            {"model": "openrouter/meta-llama/llama-3.1-8b-instruct", "api_key": os.environ.get("OPENROUTER_API_KEY", "")},
            {"model": "openrouter/meta-llama/llama-3.1-8b-instruct", "api_key": os.environ.get("OPENROUTER_API_KEY_2", "")}
        ]

        @retry(stop=stop_after_attempt(10), wait=wait_exponential(multiplier=2, min=4, max=60), before_sleep=before_sleep_log(logger, logging.WARNING))
        def get_architect_response():
            import random
            random.shuffle(endpoints)
            last_err = Exception("No endpoints available")
            
            resp = None
            for ep in endpoints:
                try:
                    resp = completion(
                        model=ep["model"],
                        api_key=ep["api_key"],
                        response_format={ "type": "json_object" },
                        messages=[
                            {"role": "system", "content": system_msg},
                            {"role": "user", "content": f"Design the agent graph for this goal: {user_prompt}"}
                        ],
                        max_tokens=8192
                    )
                    break
                except Exception as e:
                    last_err = e
                    logger.warning(f"Failed with {ep['model']}: {e}")
                    
            if resp is None:
                raise last_err
            
            # Programmatic Validation to enforce the strict constraints
            try:
                data = json.loads(resp.choices[0].message.content)
                
                # Gather valid agent IDs
                valid_agents = set()
                if available_agents and available_agents != "None":
                    import re
                    valid_agents.update(re.findall(r'^- ([^\s]+)', available_agents, re.MULTILINE))
                for a in data.get("agents", []):
                    if a.get("id"):
                        if a["id"] not in valid_agents:
                            a["is_new"] = True
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
                    
                # Cycle detection using Kahn's algorithm and Depth calculation
                depth = {n: 1 for n in valid_nodes if in_degree[n] == 0}
                queue = [n for n in valid_nodes if in_degree[n] == 0]
                visited_count = 0
                max_depth = 1
                while queue:
                    curr = queue.pop(0)
                    visited_count += 1
                    curr_d = depth[curr]
                    for neighbor in adj[curr]:
                        depth[neighbor] = max(depth.get(neighbor, 1), curr_d + 1)
                        max_depth = max(max_depth, depth[neighbor])
                        in_degree[neighbor] -= 1
                        if in_degree[neighbor] == 0:
                            queue.append(neighbor)
                            
                # Count nodes per depth layer to find maximum width
                width_counts = {}
                for d in depth.values():
                    width_counts[d] = width_counts.get(d, 0) + 1
                max_width = max(width_counts.values()) if width_counts else 0

                if visited_count != len(valid_nodes):
                    raise ValueError("Graph contains a cycle! Directed Acyclic Graph (DAG) requirement violated.")
                    
                if len(valid_nodes) == 0:
                    raise ValueError(f"Graph has 0 nodes. You MUST create at least 1 node.")
                    
                if data.get("complexity_analysis") == "complex":
                    if len(valid_nodes) < 4:
                        raise ValueError(f"Task is categorized as complex, but graph only has {len(valid_nodes)} nodes. You MUST create at least 4 nodes.")
                    if max_width < 2:
                        raise ValueError(f"Task is categorized as complex, but graph is completely linear. You MUST build WIDE parallel pipelines.")
                    
                if max_depth > 6:
                    raise ValueError(f"Graph is too deep (depth {max_depth}). Keep pipelines reasonably compressed. Max allowed depth is 6.")

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
        
        real_stdout.write(json.dumps({
            "id": req_id,
            "artifact": artifact
        }) + "\n")
        
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
