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
from tenacity import retry, stop_after_attempt, wait_exponential, before_sleep_log
import logging

logging.basicConfig(level=logging.CRITICAL)
logger = logging.getLogger(__name__)

def main():
    real_stdout = sys.stdout
    import litellm
    litellm.suppress_debug_info = True

    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    sys.stderr.reconfigure(encoding='utf-8')

    log_file = open("architect_debug.log", "w", encoding='utf-8')
    sys.stderr = log_file
    sys.stdout = log_file

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
            complexity_prompt = "CRITICAL REQUIREMENT: You MUST categorize the complexity of the user's task. Reticle is designed for massive parallelism. You MUST decompose EVERY task into a WIDE, MULTI-BRANCH DAG. Do NOT create purely linear pipelines (e.g. A -> B -> C). Even simple tasks must be broken down into at least 3-4 specialized agents. \nFor complex applications, you MUST generate a massively parallel graph with 10, 20, or even 50+ specialized nodes (e.g., one agent per file, one agent per class, one agent per API endpoint). DO NOT anchor to the small 4-node example below; that is just a schema demonstration. Scale the number of agents and nodes to be as large as necessary to achieve extreme modularity. Single-node or purely linear workflows are STRICTLY FORBIDDEN. One of your agents MUST explicitly be responsible for creating the main entrypoint or final assembly."

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

        try:
            with open(os.path.join(base_dir, "docs", "rfc", "RFC-008-Agent-Architecture.md"), "r", encoding="utf-8") as f:
                rfc_008 = f.read()
            with open(os.path.join(base_dir, "docs", "rfc", "RFC-027-Worker-Runtime-Contract.md"), "r", encoding="utf-8") as f:
                rfc_027 = f.read()
            with open(os.path.join(base_dir, "docs", "standards", "007 AGENT_STANDARD.md"), "r", encoding="utf-8") as f:
                agent_std = f.read()

            # Truncate for strict context limits on Groq and local models
            if "groq" in model.lower() or "llama" in model.lower() or "ollama" in model.lower() or "qwen" in model.lower():
                rfc_008 = rfc_008[:1000] + "\n...(TRUNCATED)"
                rfc_027 = rfc_027[:1000] + "\n...(TRUNCATED)"
                agent_std = agent_std[:1000] + "\n...(TRUNCATED)"
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
    {{ "id": "planning-step", "agent_id": "planner-agent" }},
    {{ "id": "frontend-dev", "agent_id": "worker-a-agent" }},
    {{ "id": "backend-dev", "agent_id": "worker-b-agent" }},
    {{ "id": "final-assembly", "agent_id": "assembler-agent" }}
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

        @retry(stop=stop_after_attempt(7), wait=wait_exponential(multiplier=2, min=5, max=120))
        def get_architect_response():
            try:
                # Architect output is just a schema with 'TBD' system prompts, so it's very small
                target_max_tokens = 8192
                
                extra_headers = {
                    "HTTP-Referer": "https://github.com/ManasN9401/Reticle",
                    "X-Title": "Reticle Agentic Harness",
                }
                print(f"Calling litellm.completion... with kwargs: {kwargs}", file=sys.stderr)
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
                import graphlib
                ts = graphlib.TopologicalSorter()
                for n in valid_nodes:
                    ts.add(n)
                for e in data.get("edges", []):
                    # ts.add(node, *predecessors)
                    ts.add(e.get("to"), e.get("from"))

                try:
                    ts.prepare()
                except graphlib.CycleError as ce:
                    cyclic_nodes = ce.args[1]
                    raise ValueError(f"Graph contains a cycle! Directed Acyclic Graph (DAG) requirement violated. The cycle involves these nodes: {cyclic_nodes}. You MUST remove the bi-directional edges or circular dependencies between them.")

                if len(valid_nodes) == 0:
                    raise ValueError(f"Graph has 0 nodes. You MUST create at least 1 node.")

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
                raise Exception(err_msg) # This triggers the @retry

            return data

        data = get_architect_response()

        # Spawn parallel prompt engineers for all new agents
        new_agents = [a for a in data.get("agents", []) if a.get("is_new")]
        if new_agents:
            print(f"[{req_id}] Parallelizing prompt engineering for {len(new_agents)} new agents...", file=sys.stderr)
            import concurrent.futures

            def generate_prompt(agent):
                sys_msg = f"You are an expert Prompt Engineer for Reticle. The Architect designed this graph:\\n{json.dumps(data.get('nodes', []))}\\n{json.dumps(data.get('edges', []))}\\nYour task is to write the system prompt for the agent '{agent['id']}'. It must be highly detailed and include all 5 requirements: 1. Exact goal 2. Exact files 3. Language/Framework 4. Integration with other agents 5. Technical specs."
                user_msg = f"Agent Name: {agent.get('name')}\\nAgent Description: {agent.get('description', '')}\\nUser Goal: {user_prompt}\\nWrite the 'system_prompt' for this agent. Output ONLY the prompt text, no markdown blocks."
                try:
                    resp = completion(
                        model=model,
                        max_tokens=8192,
                        temperature=0.2,
                        messages=[{"role": "system", "content": sys_msg}, {"role": "user", "content": user_msg}],
                        timeout=7200,
                        **kwargs
                    )
                    return resp.choices[0].message.content.strip()
                except Exception as e:
                    print(f"[{agent['id']}] Error generating prompt: {e}", file=sys.stderr)
                    return "Error generating prompt. You must figure out what to do based on your description: " + agent.get("description", "")

            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
                future_to_agent = {executor.submit(generate_prompt, a): a for a in new_agents}
                for future in concurrent.futures.as_completed(future_to_agent):
                    a = future_to_agent[future]
                    try:
                        a["system_prompt"] = future.result()
                    except Exception as e:
                        a["system_prompt"] = "Error"

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
