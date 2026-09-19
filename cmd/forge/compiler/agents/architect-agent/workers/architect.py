"""
Reticle Worker Script
MANDATORY READING:
- RFC-008: Agent Architecture
- RFC-027: Worker Runtime Contract
"""
import sys
import json
import os
from litellm import completion
import logging

logging.basicConfig(level=logging.CRITICAL)
logger = logging.getLogger(__name__)

def _list_text(values):
    return ", ".join(values) if values else "none declared"

def build_agent_prompt(agent, data, user_prompt):
    """Build a bounded worker prompt without another fallible model request."""
    agent_id = agent["id"]
    owned_nodes = [n for n in data.get("nodes", []) if n.get("agent_id") == agent_id]
    node_ids = [n["id"] for n in owned_nodes]
    inputs = sorted({p for n in owned_nodes for p in n.get("input_files", [])})
    outputs = sorted({p for n in owned_nodes for p in n.get("output_files", [])})
    predecessors = sorted({
        e["from"] for e in data.get("edges", []) if e.get("to") in node_ids
    })
    successors = sorted({
        e["to"] for e in data.get("edges", []) if e.get("from") in node_ids
    })
    description = str(agent.get("description", "Complete the assigned work")).strip()
    return (
        f"You are {agent_id}. Your responsibility is: {description}.\n"
        f"The user's goal is: {user_prompt}\n"
        f"Your workflow nodes are: {_list_text(node_ids)}. Read these workspace files: "
        f"{_list_text(inputs)}. Create or update exactly these workspace files: "
        f"{_list_text(outputs)}.\n"
        f"Upstream nodes are: {_list_text(predecessors)}. Downstream nodes are: "
        f"{_list_text(successors)}. Preserve upstream work and make every declared output usable "
        "by downstream nodes. Use the language and framework requested by the user or established "
        "by upstream artifacts. Verify every changed file with the available read or validation "
        "tools, then call mark_task_complete with a concise summary."
    )

def validate_dag(data, available_agent_ids, available_skill_ids, agent_complexity=5):
    if not isinstance(data, dict):
        raise ValueError("DAG must be a JSON object")
    agents = data.get("agents")
    nodes = data.get("nodes")
    edges = data.get("edges")
    if not isinstance(agents, list) or not isinstance(nodes, list) or not isinstance(edges, list):
        raise ValueError("agents, nodes and edges must be arrays")
    if not nodes or len(nodes) > 16:
        raise ValueError("Graph must contain between 1 and 16 nodes")
    if agent_complexity == 1 and len(nodes) != 1:
        raise ValueError("Single-agent workflow depth requires exactly one node")
    if agent_complexity <= 3 and len(nodes) > 5:
        raise ValueError("Balanced workflow depth permits at most five nodes")

    agent_ids = set(available_agent_ids)
    declared_agent_ids = set()
    for agent in agents:
        if not isinstance(agent, dict) or not agent.get("id"):
            raise ValueError("Every agent requires an id")
        agent_id = agent["id"]
        if agent_id in declared_agent_ids:
            raise ValueError(f"Duplicate agent id: {agent_id}")
        declared_agent_ids.add(agent_id)
        if not isinstance(agent.get("is_new"), bool):
            raise ValueError(f"Agent {agent_id} requires boolean is_new")
        if not agent["is_new"] and agent_id not in available_agent_ids:
            raise ValueError(f"Agent {agent_id} is not registered and must be marked is_new")
        for skill in agent.get("skills", []):
            if skill not in available_skill_ids:
                raise ValueError(f"Agent {agent_id} references unavailable skill: {skill}")
        agent_ids.add(agent_id)

    node_by_id = {}
    producers = {}
    for node in nodes:
        if not isinstance(node, dict) or not node.get("id"):
            raise ValueError("Every node requires an id")
        node_id = node["id"]
        if node_id in node_by_id:
            raise ValueError(f"Duplicate node id: {node_id}")
        if node.get("agent_id") not in agent_ids:
            raise ValueError(f"Node {node_id} references unknown agent: {node.get('agent_id')}")
        for field in ("input_files", "output_files"):
            values = node.get(field, [])
            if not isinstance(values, list) or not all(isinstance(v, str) and v.strip() for v in values):
                raise ValueError(f"Node {node_id} {field} must be an array of non-empty paths")
        for path in node.get("output_files", []):
            if path in producers:
                raise ValueError(f"Workspace output {path} has multiple producers")
            producers[path] = node_id
        node_by_id[node_id] = node

    predecessors = {node_id: set() for node_id in node_by_id}
    import graphlib
    sorter = graphlib.TopologicalSorter()
    for node_id in node_by_id:
        sorter.add(node_id)
    for edge in edges:
        if not isinstance(edge, dict) or edge.get("from") not in node_by_id or edge.get("to") not in node_by_id:
            raise ValueError(f"Edge references an unknown node: {edge}")
        sorter.add(edge["to"], edge["from"])
        predecessors[edge["to"]].add(edge["from"])
    try:
        order = list(sorter.static_order())
    except graphlib.CycleError as exc:
        raise ValueError(f"Graph contains a cycle involving: {exc.args[1]}") from exc

    ancestors = {node_id: set() for node_id in node_by_id}
    for node_id in order:
        for parent in predecessors[node_id]:
            ancestors[node_id].add(parent)
            ancestors[node_id].update(ancestors[parent])
        for path in node_by_id[node_id].get("input_files", []):
            producer = producers.get(path)
            # A path without a producer can be an existing workspace input.
            # When this workflow does produce it, the producer must be ordered
            # before the consumer rather than merely existing in another branch.
            if producer is not None and producer not in ancestors[node_id]:
                raise ValueError(
                    f"Node {node_id} requires {path} from {producer}, but {producer} is not an upstream dependency"
                )
    return data

def main():
    real_stdout = sys.stdout
    import litellm
    litellm.suppress_debug_info = True

    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    sys.stderr.reconfigure(encoding='utf-8')

    # Keep diagnostics on stderr so the dispatcher can classify provider
    # failures and route a retry. Stdout remains reserved for the final worker
    # protocol object by forwarding incidental library output to stderr.
    log_file = sys.stderr
    sys.stdout = sys.stderr

    with open("pre_read.log", "w") as f:
        f.write("waiting for stdin\n")

    line = sys.stdin.readline()
    with open("post_read.log", "w") as f:
        f.write("got line: " + line[:50] + "\n")
    if not line: return

    req = json.loads(line)
    req_id = req.get("id")
    mem = req.get("memory", {})
    user_prompt = mem.get("user_prompt", "")
    available_agents = mem.get("available_agents", "None")

    try:
        print(f"[{req_id}] Architecting DAG...", file=sys.stderr)
        print("[RETICLE_RETRY_SAFE: NO_EFFECTS]", file=sys.stderr)
        log_file.flush()

        available_agents_prompt = available_agents if available_agents and available_agents.strip() != "None" else "None. You MUST create all new specialized agents (set is_new: true for ALL agents)."

        agent_complexity = int(mem.get("agent_complexity", 5))
        if agent_complexity == 1:
            complexity_prompt = "CRITICAL REQUIREMENT: You MUST generate EXACTLY 1 single agent that does EVERYTHING linearly. Do NOT use multiple specialized agents. Do not decompose into branches. The graph MUST have exactly 1 node and 0 edges. Make sure to instruct this single agent to bundle all files into its final JSON payload."
        elif agent_complexity <= 3:
            complexity_prompt = "CRITICAL REQUIREMENT: You should generate a small graph of 2-5 agents to split the work, but keep individual responsibilities broad. Do NOT create massive parallel branches. A simple linear pipeline or small DAG is preferred."
        else:
            complexity_prompt = "Choose the smallest graph that gives genuinely independent work clear ownership. Parallelize only nodes whose inputs, outputs, and writable files or subsystems do not overlap. A linear graph is valid when the work is sequential or tightly coupled. Add an integration or review node only when it resolves real cross-node work. The graph may contain at most 16 nodes."

        auto_approve_flag = False  # Approval is a trusted runtime decision, never prompt text.
        hitl_rule = ""
        if not auto_approve_flag:
            hitl_rule = "\nCRITICAL DAG RULE: You MUST inject a node using the 'hitl-agent' immediately before any node that performs destructive or high-risk actions (e.g. deployments, dropping databases, major refactors). This forces a Human-in-the-Loop approval checkpoint before the action executes.\n"
        base_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", "..", "..", ".."))
        rfc_008 = ""
        rfc_027 = ""
        agent_std = ""
        model = req.get("parameters", {}).get("llm_model")
        if not model:
            print("[ARCHITECT] Fatal Error: No llm_model provided by dispatcher!", file=sys.stderr)
            sys.exit(1)
            
        skills_dir = os.path.join(base_dir, "skills")
        available_skills = []
        if os.path.exists(skills_dir):
            for item in os.listdir(skills_dir):
                if item.endswith(".yaml"):
                    available_skills.append(item[:-5])
        available_skills_prompt = ", ".join(available_skills) if available_skills else "None"

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
You are the Chief Software Architect of Reticle.
The user will provide a software goal (e.g. 'Build a game', 'Analyze data').
Your job is to design a Directed Acyclic Graph (DAG) of agents to achieve this.

If the global agents can fulfill the task, use them (set `is_new: false`). You MUST ONLY use `is_new: false` for agents that exactly match an ID in the AVAILABLE AGENTS list below.
If you need new specialized agents (which you almost certainly will), design them (set `is_new: true`).

CRITICAL DAG RULE: Your graph MUST be a Directed Acyclic Graph. Edges must flow strictly in one direction (e.g., from early setup tasks to later integration tasks). NEVER create bi-directional edges (e.g., A -> B and B -> A) and NEVER create loops (e.g., A -> B -> C -> A).

AVAILABLE AGENTS:
{available_agents_prompt}

AVAILABLE SKILLS:
{available_skills_prompt}
CRITICAL INSTRUCTION: When assigning `skills` to an agent, you MUST ONLY use skill IDs from the AVAILABLE SKILLS list above. NEVER invent or hallucinate new skills.
CRITICAL INSTRUCTION: ONLY assign skills that are ABSOLUTELY ESSENTIAL for the specific agent's exact task! Do NOT assign massive ML or DevOps skills (like 'ml-engineering' or 'devops-infrastructure') to simple frontend or backend agents. If no skill perfectly fits, assign an empty list: [].

{complexity_prompt}
{hitl_rule}
CRITICAL INSTRUCTION: For code, agents MUST build everything from scratch using ONLY standard libraries (e.g. Python with 'pygame'). Do NOT let them hallucinate or import external imaginary engines. Instruct them to write fully robust code with NO PLACEHOLDERS (no 'pass', 'TODO', or '...').

SYSTEM PROMPT QUALITY REQUIREMENT: Each agent's `system_prompt` MUST be at least 3-5 sentences and MUST include ALL of the following:
1. The user's EXACT goal (repeat it verbatim so the agent knows what is being built)
2. The EXACT file(s) this agent is responsible for creating (e.g. "Create main.py and player.py")
3. The language and framework to use (e.g. "Use Python 3 with pygame")
4. What the OTHER agents are building (e.g. "Another agent is building the level loader in levels.py, so import from there")
5. Specific technical details about what to implement (e.g. "Implement a game loop with 60fps tick rate, keyboard input handling for WASD movement, and sprite rendering using pygame.sprite.Group")

Vague prompts like "Create the main entrypoint" are FORBIDDEN. Every prompt must be specific enough that the agent can write complete, functional code without guessing.

CRITICAL INSTRUCTION: You MUST follow the Reticle Architecture and Standards strictly when designing agents. Read them below:

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
      "system_prompt": "LEAVE THIS BLANK. Output exactly 'TBD' for now to save tokens.",
      "inputs": ["expected_artifact_id"], // List of artifact IDs this agent depends on
      "memory": ["expected_memory_key"], // List of memory keys this agent needs
      "skills": ["required_skill_id"] // List of global skills this agent needs
    }}
  ],
  "nodes": [
    {{ "id": "planning-step", "agent_id": "planner-agent", "input_files": [], "output_files": ["plan.md"] }},
    {{ "id": "frontend-dev", "agent_id": "worker-a-agent", "input_files": ["plan.md"], "output_files": ["src/index.html"] }},
    {{ "id": "backend-dev", "agent_id": "worker-b-agent", "input_files": ["plan.md"], "output_files": ["src/server.py"] }},
    {{ "id": "final-assembly", "agent_id": "assembler-agent", "input_files": ["src/index.html", "src/server.py"], "output_files": ["README.md"] }}
  ],
  "edges": [
    {{ "from": "planning-step", "to": "frontend-dev" }},
    {{ "from": "planning-step", "to": "backend-dev" }},
    {{ "from": "frontend-dev", "to": "final-assembly" }},
    {{ "from": "backend-dev", "to": "final-assembly" }}
  ]
}}

CRITICAL: Keep agent descriptions EXTREMELY CONCISE. Do not write paragraphs of text. Use bullet points or short sentences. Your output MUST fit within strict token limits.
CRITICAL: Do NOT write the `system_prompt` yet. The system prompts will be generated in parallel after you design the architecture. For `system_prompt`, you MUST output exactly "TBD" to save tokens!
CRITICAL: Every node in the `nodes` array MUST have a valid `agent_id` that EXACTLY matches the `id` of an agent defined in the `agents` list or the AVAILABLE AGENTS list. NEVER leave `agent_id` blank or null.
CRITICAL: Node IDs MUST be highly descriptive, semantic, and human-readable (e.g. 'compile-frontend', 'research-sources', 'draft-outline'). DO NOT use generic IDs like 'node-1' or 'node-2'.
CRITICAL: Every edge in the `edges` array MUST reference `from` and `to` nodes that EXACTLY match the `id` of a node defined in the `nodes` array. NEVER reference a node that does not exist.
CRITICAL: Every node MUST declare `input_files` and `output_files`. If an input file is created by this workflow, its producer MUST be an upstream node connected through the edge graph; inputs already present in the workspace are allowed. Each output file may have only one producer. Use workspace-relative paths.
CRITICAL: Keep your reasoning brief. Do NOT repeat instructions or rules. Output the JSON as soon as possible without getting stuck in a loop.

Output ONLY the raw JSON. Do not output markdown code blocks.
"""



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

        api_key_env = req.get("parameters", {}).get("api_key")
        api_key = os.environ.get(api_key_env) if api_key_env else None

        kwargs = {}
        if model.startswith(("ollama/", "ollama_chat/")):
            kwargs["api_base"] = os.getenv("OLLAMA_HOST", "http://127.0.0.1:11434")
            if not api_key: api_key = "dummy"
        elif model.startswith("llama/"):
            model = "openai/" + model[6:]
            llama_host = os.getenv("LLAMA_HOST", "http://127.0.0.1:8080").rstrip("/")
            kwargs["api_base"] = llama_host + "/v1"
            if not api_key: api_key = "dummy"

        if api_key:
            kwargs["api_key"] = api_key

        def get_architect_response():
            try:
                # Architect output is just a schema with 'TBD' system prompts, so it's very small
                target_max_tokens = 8192
                
                extra_headers = {
                    "HTTP-Referer": "https://github.com/ManasN9401/Reticle",
                    "X-Title": "Reticle Agentic Harness",
                }
                print(f"Calling litellm.completion for model {model}", file=sys.stderr)
                log_file.flush()
                resp = completion(
                    model=model,
                    max_tokens=target_max_tokens,
                    messages=conversation,
                    temperature=0.2,
                    timeout=7200,
                    extra_headers=extra_headers,
                    stop=["```\n", "``` "],
                    **kwargs
                )
                print("litellm.completion returned!", file=sys.stderr)
                raw_text = getattr(resp.choices[0].message, "content", "") or ""
                reasoning = getattr(resp.choices[0].message, "reasoning_content", "") or ""
                print(f"RAW LLM REASONING:\n{reasoning}\n", file=sys.stderr)
                print(f"RAW LLM OUTPUT:\n{raw_text}\n", file=sys.stderr)
                log_file.flush()
            except Exception as e:
                err_str = str(e)
                if "RateLimit" in err_str or "429" in err_str or "quota" in err_str.lower() or "overloaded" in err_str.lower() or "NotFoundError" in err_str or "404" in err_str or "APIError" in err_str or "APIConnectionError" in err_str or "502" in err_str or "503" in err_str or "too large" in err_str.lower() or "context_window" in err_str.lower() or "max_tokens" in err_str.lower() or "BadRequest" in err_str or "InvalidRequest" in err_str or "model_ter" in err_str.lower() or "invalid_request_error" in err_str.lower() or "402" in err_str or "payment" in err_str.lower() or "credits" in err_str.lower() or "purchased" in err_str.lower() or "authenticationerror" in err_str.lower() or "timeout" in err_str.lower():
                    # We fail FAST on hard limits so the Go orchestrator can catch it and route to a new model
                    print("[RETICLE_RETRY_SAFE: NO_EFFECTS]", file=sys.stderr)
                    print(f"[LLM] Hard limit reached on {model}: {err_str[:150]}", file=sys.stderr)
                    sys.exit(1)
                else:
                    print(f"[LLM] Error: {err_str[:300]}{'...' if len(err_str) > 300 else ''}", file=sys.stderr)
                raise e

            try:
                content = resp.choices[0].message.content
                if content:
                    for line in content.split("\n"):
                        print(f"[LLM] {line}", file=sys.stderr, flush=True)
                raw_content = content.strip() if content else ""

                # Robust JSON extraction
                import re
                json_match = re.search(r'```(?:json)?\s*(\{.*\}|\[.*\])\s*```', raw_content, re.DOTALL)
                if json_match:
                    raw_content = json_match.group(1)
                else:
                    # Fallback to finding the first { and last } if no markdown blocks
                    start_idx = raw_content.find('{')
                    end_idx = raw_content.rfind('}')
                    if start_idx != -1 and end_idx != -1 and end_idx > start_idx:
                        raw_content = raw_content[start_idx:end_idx+1]

                raw_content = raw_content.strip()
                data = json.loads(raw_content)

                # Gather the registry IDs separately so an invented agent cannot
                # claim to be an existing specialist.
                available_agent_ids = set()
                if available_agents and available_agents != "None":
                    import re
                    available_agent_ids.update(re.findall(r'^- ([^\s]+)', available_agents, re.MULTILINE))
                validate_dag(data, available_agent_ids, set(available_skills), agent_complexity)

            except Exception as e:
                err_msg = f"Validation failed: {str(e)}"
                if resp and resp.choices and len(resp.choices) > 0:
                    assistant_content = resp.choices[0].message.content or ""
                    if len(assistant_content) > 1500:
                        assistant_content = assistant_content[:1500] + "\n...[TRUNCATED]"
                    conversation.append({"role": "assistant", "content": assistant_content})
                    conversation.append({"role": "user", "content": f"{err_msg}\nFix this and output the raw JSON again."})
                print(f"[ARCHITECT] {err_msg}", file=sys.stderr)
                log_file.flush()
                raise ValueError(err_msg)

            return data

        last_validation_error = None
        for validation_attempt in range(3):
            try:
                data = get_architect_response()
                break
            except (ValueError, json.JSONDecodeError) as exc:
                last_validation_error = exc
                if validation_attempt == 2:
                    raise
                print(f"[ARCHITECT] Requesting corrected graph ({validation_attempt + 2}/3)", file=sys.stderr)
                log_file.flush()
        else:
            raise last_validation_error

        # Build prompts from the validated graph. Calling the provider once per
        # agent made compilation slow and allowed partial, placeholder workers.
        new_agents = [a for a in data.get("agents", []) if a.get("is_new")]
        for agent in new_agents:
            agent["system_prompt"] = build_agent_prompt(agent, data, user_prompt)

        result = json.dumps(data, indent=2)

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
