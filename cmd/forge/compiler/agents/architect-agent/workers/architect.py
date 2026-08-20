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

logging.basicConfig(level=logging.CRITICAL)
logger = logging.getLogger(__name__)

def main():
    real_stdout = sys.stdout
    sys.stdout = sys.stderr

    import litellm
    litellm.suppress_debug_info = True

    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    sys.stderr.reconfigure(encoding='utf-8')

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

        base_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", "..", "..", ".."))
        rfc_008 = ""
        rfc_027 = ""
        agent_std = ""
        try:
            with open(os.path.join(base_dir, "docs", "rfc", "RFC-008-Agent-Architecture.md"), "r", encoding="utf-8") as f:
                rfc_008 = f.read()
            with open(os.path.join(base_dir, "docs", "rfc", "RFC-027-Worker-Runtime-Contract.md"), "r", encoding="utf-8") as f:
                rfc_027 = f.read()
            with open(os.path.join(base_dir, "docs", "standards", "007 AGENT_STANDARD.md"), "r", encoding="utf-8") as f:
                agent_std = f.read()
        except Exception as e:
            print(f"[LLM] Error reading standards: {e}", file=sys.stderr)

        system_msg = f"""
You are the Chief Software Architect of HyperParallel.
The user will provide a software goal (e.g. 'Build a game', 'Analyze data').
Your job is to design a Directed Acyclic Graph (DAG) of agents to achieve this.

If the global agents can fulfill the task, use them (set `is_new: false`). You MUST ONLY use `is_new: false` for agents that exactly match an ID in the AVAILABLE AGENTS list below.
If you need new specialized agents (which you almost certainly will), design them (set `is_new: true`).

CRITICAL DAG RULE: Your graph MUST be a Directed Acyclic Graph. Edges must flow strictly in one direction (e.g., from early setup tasks to later integration tasks). NEVER create bi-directional edges (e.g., A -> B and B -> A) and NEVER create loops (e.g., A -> B -> C -> A).

AVAILABLE AGENTS:
{available_agents_prompt}

CRITICAL REQUIREMENT: You MUST categorize the complexity of the user's task. HyperParallel is designed for massive parallelism. You MUST decompose EVERY task into a WIDE, MULTI-BRANCH DAG. Do NOT create purely linear pipelines (e.g. A -> B -> C). Even simple tasks must be broken down into at least 3-4 specialized agents. 
For complex applications, you MUST generate a massively parallel graph with 10, 20, or even 50+ specialized nodes (e.g., one agent per file, one agent per class, one agent per API endpoint). DO NOT anchor to the small 4-node example below; that is just a schema demonstration. Scale the number of agents and nodes to be as large as necessary to achieve extreme modularity. Single-node or purely linear workflows are STRICTLY FORBIDDEN. One of your agents MUST explicitly be responsible for creating the main entrypoint or final assembly.

CRITICAL INSTRUCTION: For code, agents MUST build everything from scratch using ONLY standard libraries (e.g. Python with 'pygame'). Do NOT let them hallucinate or import external imaginary engines. Instruct them to write fully robust code with NO PLACEHOLDERS (no 'pass', 'TODO', or '...').

SYSTEM PROMPT QUALITY REQUIREMENT: Each agent's `system_prompt` MUST be at least 3-5 sentences and MUST include ALL of the following:
1. The user's EXACT goal (repeat it verbatim so the agent knows what is being built)
2. The EXACT file(s) this agent is responsible for creating (e.g. "Create main.py and player.py")
3. The language and framework to use (e.g. "Use Python 3 with pygame")
4. What the OTHER agents are building (e.g. "Another agent is building the level loader in levels.py, so import from there")
5. Specific technical details about what to implement (e.g. "Implement a game loop with 60fps tick rate, keyboard input handling for WASD movement, and sprite rendering using pygame.sprite.Group")

Vague prompts like "Create the main entrypoint" are FORBIDDEN. Every prompt must be specific enough that the agent can write complete, functional code without guessing.

CRITICAL INSTRUCTION: You MUST follow the HyperParallel Architecture and Standards strictly when designing agents. Read them below:

--- RFC-008: Agent Architecture ---
{rfc_008}

--- RFC-027: Worker Runtime Contract ---
{rfc_027}

--- 007 AGENT_STANDARD.md ---
{agent_std}
--- END OF STANDARDS ---

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
      "system_prompt": "Highly detailed prompt with all 5 requirements above...",
      "inputs": ["expected_artifact_id"], // List of artifact IDs this agent depends on
      "memory": ["expected_memory_key"], // List of memory keys this agent needs
      "skills": ["required_skill_id"] // List of global skills this agent needs
    }}
  ],
  "nodes": [
    {{ "id": "node-1", "agent_id": "planner-agent" }},
    {{ "id": "node-2", "agent_id": "worker-a-agent" }},
    {{ "id": "node-3", "agent_id": "worker-b-agent" }},
    {{ "id": "node-4", "agent_id": "assembler-agent" }}
  ],
  "edges": [
    {{ "from": "node-1", "to": "node-2" }},
    {{ "from": "node-1", "to": "node-3" }},
    {{ "from": "node-2", "to": "node-4" }},
    {{ "from": "node-3", "to": "node-4" }}
  ]
}}

CRITICAL: Every node in the `nodes` array MUST have a valid `agent_id` that EXACTLY matches the `id` of an agent defined in the `agents` list or the AVAILABLE AGENTS list. NEVER leave `agent_id` blank or null.
CRITICAL: Every edge in the `edges` array MUST reference `from` and `to` nodes that EXACTLY match the `id` of a node defined in the `nodes` array. NEVER reference a node that does not exist.

Output ONLY the raw JSON. Do not output markdown code blocks.
"""

        model = req.get("parameters", {}).get("llm_model", "groq/qwen/qwen3.6-27b")
        
        ide_context = mem.get("ide_context", "")
        ide_prefix = ""
        if ide_context:
            ide_prefix = f"## IDE Context\nThe user currently has the following workspace context. Use this to infer what they are referring to (e.g., if they say 'this file' or 'this function'):\n{ide_context}\n\n"

        prompt_history = mem.get("prompt_history", "")
        hist_prefix = ""
        if prompt_history:
            hist_prefix = f"## Previous Iterations History\nThis is a continuation of previous work. Here is the history of previous prompts in this group:\n{prompt_history}\n\n"

        conversation = [
            {"role": "system", "content": system_msg},
            {"role": "user", "content": f"{ide_prefix}{hist_prefix}Design the agent graph for this goal: {user_prompt}"}
        ]
        
        # We can configure fallbacks natively in litellm
        fallbacks = ["groq/groq/compound-mini", "gemini/gemini-3.5-flash-lite"]
        # Ensure we don't put the primary model in the fallbacks list
        if model in fallbacks:
            fallbacks.remove(model)

        @retry(stop=stop_after_attempt(7), wait=wait_exponential(multiplier=2, min=5, max=120))
        def get_architect_response():
            try:
                resp = completion(
                    model=model,
                    response_format={ "type": "json_object" },
                    messages=conversation,
                    max_tokens=8192,
                    fallbacks=fallbacks
                )
            except Exception as e:
                err_str = str(e)
                if "RateLimitError" in err_str:
                    print(f"[LLM] Error: RateLimitError: API rate limit exceeded. Retrying...", file=sys.stderr)
                else:
                    print(f"[LLM] Error: {err_str[:300]}{'...' if len(err_str) > 300 else ''}", file=sys.stderr)
                raise e
            
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
                    cyclic_nodes = [n for n in valid_nodes if in_degree[n] > 0]
                    raise ValueError(f"Graph contains a cycle! Directed Acyclic Graph (DAG) requirement violated. The cycle involves these nodes: {cyclic_nodes}. You MUST remove the bi-directional edges or circular dependencies between them.")
                    
                if len(valid_nodes) == 0:
                    raise ValueError(f"Graph has 0 nodes. You MUST create at least 1 node.")

            except Exception as e:
                err_msg = f"Validation failed: {str(e)}"
                if resp and resp.choices and len(resp.choices) > 0:
                    conversation.append({"role": "assistant", "content": resp.choices[0].message.content})
                    conversation.append({"role": "user", "content": f"{err_msg}\nFix this and output the raw JSON again."})
                print(f"[ARCHITECT] {err_msg}", file=sys.stderr)
                raise Exception(err_msg) # This triggers the @retry
                
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
