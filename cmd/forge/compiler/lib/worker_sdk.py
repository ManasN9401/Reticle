"""One worker protocol and tool loop for all generated/specialist workers."""
import json
import os
from pathlib import Path
import queue
import re
import shlex
import sys
import threading
import time
from contextlib import nullcontext
import forge_utils as toolset

def _llm_field(value, name, default=None):
    """Read a LiteLLM response field from either its object or dict form."""
    if isinstance(value, dict):
        return value.get(name, default)
    return getattr(value, name, default)

def _llm_reasoning(value):
    """Return reasoning text only when the provider included it in its response."""
    direct = (_llm_field(value, "reasoning_content") or _llm_field(value, "reasoning")
              or _llm_field(value, "thinking"))
    if direct:
        return direct
    provider_fields = _llm_field(value, "provider_specific_fields", {}) or {}
    if isinstance(provider_fields, dict) and (provider_fields.get("reasoning_content") or provider_fields.get("reasoning")):
        return provider_fields.get("reasoning_content") or provider_fields.get("reasoning")
    blocks = _llm_field(value, "thinking_blocks")
    if not blocks and isinstance(provider_fields, dict):
        blocks = provider_fields.get("thinking_blocks")
    if isinstance(blocks, list):
        return "".join(str(_llm_field(block, "thinking") or _llm_field(block, "text") or "") for block in blocks)
    return ""

def _emit_llm(kind, text, **metadata):
    """Emit a structured, single-line diagnostic without touching worker stdout."""
    if text is None or text == "":
        return
    event = {"kind": kind, "text": str(text)}
    event.update({key: value for key, value in metadata.items() if value})
    sys.stderr.write(f"\n[LLM_STREAM] {json.dumps(event, ensure_ascii=False)}\n")
    sys.stderr.flush()

def _stream_with_first_event_timeout(response, timeout_seconds):
    """Pump a provider stream and fail boundedly if it remains completely silent."""
    events = queue.Queue(maxsize=128)
    finished = object()

    def produce():
        try:
            for chunk in response:
                events.put(chunk)
        except BaseException as exc:
            events.put(exc)
        finally:
            events.put(finished)

    threading.Thread(target=produce, name="reticle-llm-stream", daemon=True).start()
    first = True
    while True:
        try:
            item = events.get(timeout=timeout_seconds if first else None)
        except queue.Empty as exc:
            raise TimeoutError(
                f"Model produced no stream event within {timeout_seconds} seconds"
            ) from exc
        first = False
        if item is finished:
            return
        if isinstance(item, BaseException):
            raise item
        yield item

def _tool_call_signature(tool_calls):
    """Canonicalize a tool round so repeated no-progress rounds are detectable."""
    signature = []
    for call in tool_calls:
        function = call.get("function", {})
        name = str(function.get("name", ""))
        raw_arguments = function.get("arguments", "") or ""
        try:
            arguments = json.dumps(json.loads(raw_arguments), sort_keys=True, separators=(",", ":"))
        except (TypeError, ValueError, json.JSONDecodeError):
            arguments = str(raw_arguments).strip()
        signature.append((name, arguments))
    return tuple(sorted(signature))

def _tool_repeat_state(previous, rounds, tool_calls):
    signature = _tool_call_signature(tool_calls)
    if not signature:
        return None, 0
    if signature == previous:
        return signature, rounds + 1
    return signature, 1

def _compact_local_instructions(text, limit=16000):
    """Bound verbose skill references for small local context windows.

    Keep the agent contract plus headings and directive/list lines from skill
    documents. Long fenced examples are useful references for cloud models but
    can consume most of an 8K local context before the task itself is seen.
    """
    if len(text) <= limit:
        return text
    lines = text.splitlines()
    result = []
    result_length = 0
    in_fence = False
    for index, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("```"):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        if index < 20 or stripped.startswith(("#", "-", "*", "name:", "description:")):
            result.append(line)
            result_length += len(line) + 1
        if result_length >= limit:
            break
    compacted = "\n".join(result).strip()
    return compacted[:limit] + "\n[Skill examples omitted to fit the local model context.]"

def _meaningful_terminal_verification(kind, parts):
    """Recognize commands that inspect or validate the produced work."""
    if not parts:
        return False
    command = Path(parts[0]).stem.lower()
    subcommand = parts[1].lower() if len(parts) > 1 else ""
    if kind == "devops":
        return (command, subcommand) in {
            ("terraform", "fmt"), ("terraform", "validate"), ("terraform", "plan"),
            ("docker", "inspect"), ("kubectl", "get"), ("kubectl", "describe"),
        }
    if kind == "pentest":
        return command == "bandit" and subcommand == "-r"
    if command in {"pytest", "jest", "vitest", "tsc", "eslint", "oxlint", "ruff", "mypy", "pyright"}:
        return True
    if command in {"go", "cargo", "dotnet", "mvn", "gradle", "gradlew"}:
        return subcommand in {"test", "vet", "build", "check", "clippy", "verify"}
    if command in {"python", "python3", "py"} and subcommand == "-m" and len(parts) > 2:
        return parts[2].lower() in {"pytest", "unittest", "compileall"}
    if command in {"npm", "pnpm", "yarn", "bun"}:
        if subcommand == "test":
            return True
        return subcommand == "run" and len(parts) > 2 and parts[2].lower() in {
            "test", "check", "lint", "build", "typecheck",
        }
    return False

_SCRIPT_INTERPRETERS = {
    "python", "python3", "py", "node", "nodejs", "ruby", "php", "perl", "bash", "sh",
    "pwsh", "powershell",
}

def _direct_script_verification_path(parts):
    """Recognize `<interpreter> <script>` invocations, distinct from
    _meaningful_terminal_verification's test/build/lint allowlist. A standalone
    script with no test suite has no other way to prove it works than running
    it directly, so this returns the workspace-relative path of the script
    argument when the command is a real interpreter invoking a file — the
    caller still requires that path to match a just-modified file and the
    command to have exited 0 before treating it as verification.
    """
    if len(parts) < 2:
        return None
    command = Path(parts[0]).stem.lower()
    if command not in _SCRIPT_INTERPRETERS:
        return None
    for arg in parts[1:]:
        if arg.startswith("-"):
            continue
        return _normalized_workspace_path(arg)
    return None

def _normalized_workspace_path(path):
    return Path(os.path.normpath(path)).as_posix()

def _required_output_paths(instructions):
    """Extract a node's declared output files straight out of the fixed
    sentence architect.py's build_agent_prompt() emits ("Create or update
    exactly these workspace files: a, b."), so verification can require they
    actually get written. The Go runtime has no structured concept of
    per-node output_files at all — this is the only place that information
    still exists by the time a worker is running — so it's recovered from
    prose rather than plumbed through as a new field.
    """
    match = re.search(
        r"Create or update exactly these workspace files:\s*([^\n]*?)\.\s*\n",
        instructions,
    )
    if not match:
        return set()
    listed = match.group(1).strip()
    if not listed or listed.lower() == "none declared":
        return set()
    return {_normalized_workspace_path(p.strip()) for p in listed.split(",") if p.strip()}

def _output_satisfied(required, written_paths):
    return any(
        w == required or w.startswith(required + "/") or required.startswith(w + "/")
        for w in written_paths
    )

def shared_memory_context(memory):
    """Expose dispatched facts, excluding runtime controls, with an explicit size limit."""
    controls = {"user_prompt", "workspace_dir", "max_retries", "allow_native_execution",
                "ide_context", "prompt_attachments", "prompt_history", "global_effort",
                "agent_complexity", "task_timeout_seconds", "llm_num_ctx",
                "llm_max_tokens", "llm_temperature", "llm_first_token_timeout_seconds",
                "ollama_keep_alive"}
    facts = {key: value for key, value in memory.items() if key not in controls}
    if len(json.dumps(facts, ensure_ascii=False).encode("utf-8")) > 65536:
        raise ValueError("Shared memory exceeds 64 KiB: select fewer required_memory keys or use summaries/artifact references")
    return facts

def build_user_context(req, memory):
    return {"prompt":req.get("parameters",{}).get("user_prompt",memory.get("user_prompt")),
            "inputs":req.get("inputs",[]), "context":memory.get("ide_context"),
            "attachments":memory.get("prompt_attachments"), "history":memory.get("prompt_history"),
            "shared_memory":shared_memory_context(memory), "memory_metadata":req.get("memory_metadata",{})}

def generation_options(model, memory):
    """Return only the request options supported across the selected provider."""
    options = {}
    # num_ctx is an Ollama request option. OpenAI-compatible cloud APIs such
    # as Groq reject this field, while llama.cpp configures context capacity
    # on the server rather than per completion request.
    if model.startswith(("ollama/", "ollama_chat/")) and "llm_num_ctx" in memory:
        options["num_ctx"] = int(memory["llm_num_ctx"])
        options["keep_alive"] = str(memory.get("ollama_keep_alive", "5m"))
    if "llm_max_tokens" in memory:
        options["max_tokens"] = int(memory["llm_max_tokens"])
    if "llm_temperature" in memory:
        options["temperature"] = float(memory["llm_temperature"])
    return options

def run(instructions, kind="coding"):
    req = json.load(sys.stdin)
    model = req.get("parameters", {}).get("llm_model")
    if not model:
        raise ValueError("No model was routed for this task")
    from litellm import completion
    mem = req.get("memory", {})
    workspace = mem["workspace_dir"]
    os.environ["RETICLE_EXECUTION_ID"] = req.get("execution", "")
    compatibility_capabilities = {
        "workspace.read", "workspace.write", "network.public", "process.container",
        "process.native", "memory.execution", "graph.delegate", "image.local", "rag.local",
    }
    capabilities = set(req["capabilities"]) if "capabilities" in req else compatibility_capabilities
    native = (str(mem.get("allow_native_execution", False)).lower() == "true"
              and "process.native" in capabilities)
    files = {}
    memory_updates = []
    graph_mutation = None
    tool_capabilities = {
        "read_file": "workspace.read", "list_dir": "workspace.read", "search_codebase": "workspace.read",
        "write_file": "workspace.write", "replace_file_content": "workspace.write",
        "read_url": "network.public", "execute_terminal_command": "process.native" if native else "process.container",
        "mark_task_complete": None,
    }
    definitions = {name: spec for name, spec in toolset._definitions.items()
                   if tool_capabilities.get(name) is None or tool_capabilities[name] in capabilities}
    if "memory.execution" in capabilities:
        definitions["remember"] = ("Save a JSON value in this execution's memory", {"key":"string", "value_json":"string"})
        definitions["remember_if_version"] = ("Update an execution-memory key only if its execution-scoped revision in memory_metadata still matches; use 0 to create an absent execution key", {"key":"string", "value_json":"string", "expected_version":"string"})
    if "graph.delegate" in capabilities:
        definitions["delegate"] = ("Request a registered agent and then return to this supervisor", {"target_agent":"string"})
    implementations = {}
    if kind == "rag" and "rag.local" in capabilities:
        import rag_tools
        for name, key in (("index_directory","path"),("query_knowledge","query"),("remove_path_from_index","path")):
            definitions[name] = (name.replace("_"," "), {key:"string"})
            implementations[name] = getattr(rag_tools,name)
    import comfy_tools
    import urllib.request, urllib.parse
    comfy_checkpoints = ""
    if "image.local" in capabilities:
        try:
            host = os.getenv("COMFYUI_HOST", "http://127.0.0.1:8188").rstrip("/")
            req_chk = urllib.request.Request(host+"/object_info/CheckpointLoaderSimple")
            with urllib.request.urlopen(req_chk, timeout=2) as res:
                chk_data = json.loads(res.read(1024*1024))
                ckpt_list = chk_data.get("CheckpointLoaderSimple", {}).get("input", {}).get("required", {}).get("ckpt_name", [[]])[0]
                if ckpt_list:
                    comfy_checkpoints = " Available checkpoints: " + ", ".join(ckpt_list)
        except Exception:
            pass

    if "image.local" in capabilities:
        definitions["generate_local_asset"]=(
            f"Generate an image using configured local ComfyUI.{comfy_checkpoints}",
            {
                "prompt":"string",
                "output_path":"string",
                "checkpoint":"string"
            }
        )
        implementations["generate_local_asset"] = comfy_tools.generate_local_asset
    tools=[{"type":"function","function":{"name":name,"description":desc,"parameters":{"type":"object","properties":{k:{"type":v} for k,v in props.items()},"required":list(props),"additionalProperties":False}}} for name,(desc,props) in definitions.items()]
    verified = False
    verification = []
    pending_modified_paths = set()
    written_paths = set()
    required_outputs = _required_output_paths(instructions)
    effects_started = False
    def missing_outputs():
        return sorted(r for r in required_outputs if not _output_satisfied(r, written_paths))
    def record_verification(tool, target, checked_path=None, covers_changes=False):
        nonlocal verified
        verification.append({"tool": tool, "target": target, "outcome": "succeeded"})
        if covers_changes:
            pending_modified_paths.clear()
        elif checked_path is not None:
            pending_modified_paths.discard(checked_path)
        verified = bool(verification) and not pending_modified_paths and not missing_outputs()
    prompt_instructions = instructions
    if model.startswith(("ollama/", "ollama_chat/")):
        prompt_instructions = _compact_local_instructions(instructions)
        if prompt_instructions != instructions:
            _emit_llm("status", f"Compacted skill references from {len(instructions)} to {len(prompt_instructions)} characters for the local context window")
    messages = [{"role":"system", "content": prompt_instructions + "\n" + req.get("parameters",{}).get("system_prompt","") + "\nUse workspace-relative paths. Terminal cwd is src. Finish only after checking your work. An exhausted loop is a failure."},
                {"role":"user", "content":json.dumps(build_user_context(req, mem))}]
    kwargs = generation_options(model, mem)
    key_name = req.get("parameters", {}).get("api_key")
    if model.startswith(("ollama/", "ollama_chat/")):
        kwargs["api_base"] = os.getenv("OLLAMA_HOST", "http://localhost:11434")
        if "api_key" not in kwargs and not os.environ.get(key_name or ""):
            kwargs["api_key"] = "dummy"
    elif model.startswith("llama/"):
        model = "openai/" + model[6:]
        llama_host = os.getenv("LLAMA_HOST", "http://localhost:8080").rstrip("/")
        kwargs["api_base"] = llama_host + "/v1"
        if key_name and key_name in os.environ:
            kwargs["api_key"] = os.environ[key_name]
        else:
            kwargs["api_key"] = "dummy"
    elif key_name:
        kwargs["api_key"] = os.environ[key_name]
    
    started = time.monotonic()
    last_tool_signature = None
    repeated_tool_rounds = 0
    for iteration in range(30):
        if time.monotonic() - started > 3600:
            _emit_llm("status", "Stopped: agent time budget exhausted")
            if not effects_started:
                print("[RETICLE_RETRY_SAFE: NO_EFFECTS]", file=sys.stderr, flush=True)
            raise TimeoutError("Agent time budget exhausted")
        try:
            extra_headers = {
                "HTTP-Referer": "https://github.com/ManasN9401/Reticle",
                "X-Title": "Reticle Agentic Harness",
            }
            first_event_timeout = max(5, int(mem.get("llm_first_token_timeout_seconds", 180)))
            local_model = model.startswith(("ollama/", "ollama_chat/"))
            if local_model and not comfy_tools.ollama_model_loaded(model):
                status = f"Loading local model {model}; waiting for first output"
            else:
                status = f"Request sent to {model}; waiting for first output"
            _emit_llm("status", f"{status} (timeout {first_event_timeout}s)")
            session = (
                comfy_tools.local_gpu_session("ollama", lambda message: _emit_llm("status", message))
                if local_model else nullcontext()
            )
            with session:
                response = completion(model=model, messages=messages, tools=tools, timeout=1800, num_retries=0, extra_headers=extra_headers, stream=True, **kwargs)
                content_buffer = []
                tool_calls_buffer = {}
                if hasattr(response, "choices") and hasattr(response.choices[0], "message"):
                    complete_message = response.choices[0].message
                    complete_reasoning = _llm_reasoning(complete_message)
                    if complete_reasoning:
                        _emit_llm("reasoning", complete_reasoning)
                    if getattr(complete_message, "content", None):
                        content_buffer.append(complete_message.content)
                        _emit_llm("content", complete_message.content)
                    for idx, tc in enumerate(getattr(complete_message, "tool_calls", None) or []):
                        tool_calls_buffer[idx] = {
                            "id": getattr(tc, "id", "") or "",
                            "function": {
                                "name": getattr(tc.function, "name", "") or "",
                                "arguments": getattr(tc.function, "arguments", "") or "",
                            },
                        }
                        _emit_llm("tool", f"Requested {getattr(tc.function, 'name', '') or 'tool'}", name=getattr(tc.function, "name", "") or "")
                    response_chunks = []
                else:
                    response_chunks = _stream_with_first_event_timeout(response, first_event_timeout)
                for chunk in response_chunks:
                    delta = chunk.choices[0].delta
                    reasoning_delta = _llm_reasoning(delta)
                    if reasoning_delta:
                        _emit_llm("reasoning", reasoning_delta)
                    if hasattr(delta, "content") and delta.content:
                        _emit_llm("content", delta.content)
                        content_buffer.append(delta.content)
                    if hasattr(delta, "tool_calls") and delta.tool_calls:
                        for tc in delta.tool_calls:
                            idx = tc.index
                            if idx not in tool_calls_buffer:
                                tc_id = getattr(tc, "id", None) or ""
                                tc_name = ""
                                if hasattr(tc, "function") and hasattr(tc.function, "name") and tc.function.name:
                                    tc_name = tc.function.name
                                tool_calls_buffer[idx] = {"id": tc_id, "function": {"name": tc_name, "arguments": ""}}
                                if tc_name:
                                    _emit_llm("tool", f"Requested {tc_name}", name=tc_name)
                            else:
                                if hasattr(tc, "id") and tc.id:
                                    tool_calls_buffer[idx]["id"] = tc.id
                                if hasattr(tc, "function") and hasattr(tc.function, "name") and tc.function.name:
                                    had_name = bool(tool_calls_buffer[idx]["function"]["name"])
                                    tool_calls_buffer[idx]["function"]["name"] = tc.function.name
                                    if not had_name:
                                        _emit_llm("tool", f"Requested {tc.function.name}", name=tc.function.name)
                            if hasattr(tc, "function") and hasattr(tc.function, "arguments") and tc.function.arguments:
                                tool_calls_buffer[idx]["function"]["arguments"] += tc.function.arguments

            # Reconstruct the message
            message_dict = {"role": "assistant"}
            if content_buffer:
                message_dict["content"] = "".join(content_buffer)
                
                # FALLBACK: Try to parse raw JSON into a tool call if native tool_calls are missing
                if not tool_calls_buffer:
                    content_str = message_dict["content"].strip()
                    try:
                        import re
                        parsed = None
                        json_match = re.search(r'```(?:json)?\s*(\{.*\}|\[.*\])\s*```', content_str, re.DOTALL)
                        if json_match:
                            parsed = json.loads(json_match.group(1))
                        else:
                            start_idx = content_str.find('{')
                            end_idx = content_str.rfind('}')
                            if start_idx != -1 and end_idx != -1 and end_idx > start_idx:
                                parsed = json.loads(content_str[start_idx:end_idx+1])
                        
                        if parsed:
                            tcs = []
                            if isinstance(parsed, dict):
                                if "tool_calls" in parsed and isinstance(parsed["tool_calls"], list):
                                    for idx, tc in enumerate(parsed["tool_calls"]):
                                        tcs.append({"id": f"call_man_{idx}", "type": "function", "function": {"name": tc.get("name", tc.get("function", {}).get("name", "")), "arguments": json.dumps(tc.get("arguments", tc.get("function", {}).get("arguments", {}))) if isinstance(tc.get("arguments", tc.get("function", {}).get("arguments", {})), dict) else tc.get("arguments", tc.get("function", {}).get("arguments", ""))}})
                                elif "name" in parsed and "arguments" in parsed:
                                    tcs.append({"id": "call_man_0", "type": "function", "function": {"name": parsed["name"], "arguments": json.dumps(parsed["arguments"]) if isinstance(parsed["arguments"], dict) else parsed["arguments"]}})
                                elif len(parsed) == 1:
                                    key = list(parsed.keys())[0]
                                    if isinstance(parsed[key], dict):
                                        tcs.append({"id": "call_man_0", "type": "function", "function": {"name": key, "arguments": json.dumps(parsed[key])}})
                            
                            if tcs:
                                message_dict["tool_calls"] = tcs
                    except Exception:
                        pass

            if tool_calls_buffer:
                message_dict["tool_calls"] = []
                for idx in sorted(tool_calls_buffer.keys()):
                    message_dict["tool_calls"].append({
                        "id": tool_calls_buffer[idx]["id"],
                        "type": "function",
                        "function": tool_calls_buffer[idx]["function"]
                    })

            if "tool_calls" in message_dict and not effects_started:
                last_tool_signature, repeated_tool_rounds = _tool_repeat_state(
                    last_tool_signature, repeated_tool_rounds, message_dict["tool_calls"]
                )
                if repeated_tool_rounds >= 3:
                    _emit_llm(
                        "status",
                        "Stopped: repeated identical tool requests for 3 consecutive iterations",
                    )
                    raise RuntimeError(
                        "Agent stalled: repeated identical tool requests for 3 consecutive iterations"
                    )
            elif "tool_calls" not in message_dict and not effects_started:
                last_tool_signature = None
                repeated_tool_rounds = 0
            
            messages.append(message_dict)
            if "tool_calls" not in message_dict:
                messages.append({"role":"user","content":"Use tools to verify and finish with mark_task_complete."})
                continue
            
            # Create a mock message object with tool_calls for the remainder of the loop
            class MockMessage:
                pass
            message = MockMessage()
            message.tool_calls = []
            class MockFunction:
                pass
            class MockToolCall:
                pass
            for tc in message_dict["tool_calls"]:
                mtc = MockToolCall()
                mtc.id = tc["id"]
                mtc.function = MockFunction()
                mtc.function.name = tc["function"]["name"]
                mtc.function.arguments = tc["function"]["arguments"]
                message.tool_calls.append(mtc)

        except Exception:
            if not effects_started:
                print("[RETICLE_RETRY_SAFE: NO_EFFECTS]", file=sys.stderr, flush=True)
            raise
        for call in message.tool_calls:
            name = call.function.name
            try:
                args = json.loads(call.function.arguments)
                expected = definitions.get(name)
                if expected is None:
                    # A weak model can hallucinate a plausible-looking tool name
                    # (e.g. echoing a node/agent id from its own prompt) instead of
                    # picking from its actual tool schema. A bare "unsupported"
                    # error gives it nothing to correct against, so list the real
                    # options — the same intent as the write_file/mark_task_complete
                    # error messages already do for their own failure modes.
                    raise ValueError(
                        f"'{name}' is not a real tool. Choose one of the tools actually "
                        f"available to you: {', '.join(sorted(definitions))}."
                    )
                if set(args) != set(expected[1]) or not all(isinstance(v,str) for v in args.values()):
                    raise ValueError(
                        f"Invalid arguments for '{name}'. It requires exactly these string "
                        f"arguments: {', '.join(sorted(expected[1])) or '(none)'}."
                    )
                if name in ("write_file", "replace_file_content", "execute_terminal_command", "generate_local_asset", "index_directory", "remove_path_from_index"):
                    effects_started = True
                if name == "mark_task_complete":
                    if not verified:
                        still_missing = missing_outputs()
                        if still_missing:
                            raise ValueError(
                                "You have not created your required output files yet: "
                                f"{', '.join(still_missing)}. Reading or verifying an unrelated file "
                                "does not satisfy this — use write_file to create each of these, then "
                                "verify them, before calling mark_task_complete again."
                            )
                        raise ValueError("No successful verification has been recorded. Re-read every changed file with read_file, or run it/test it successfully with execute_terminal_command, before calling mark_task_complete again.")
                    sys.stdout.write(json.dumps({"id":req["id"],"memory":memory_updates,"graph_mutation":graph_mutation,"verification":verification,"artifact":{"id":req["id"]+"_output","name":kind+" output","type":"document/markdown","data":args["summary"]}})+"\n")
                    return
                if name in ("remember", "remember_if_version"):
                    if not args["key"] or len(args["key"]) > 120 or len(args["value_json"]) > 65536:
                        raise ValueError("Memory key/value exceeds the task limit")
                    update={"key":args["key"],"value":json.loads(args["value_json"]),"scope":"execution"}
                    if name == "remember_if_version":
                        expected=int(args["expected_version"])
                        if expected < 0: raise ValueError("Expected version must be non-negative")
                        update["expected_version"]=expected
                    memory_updates.append(update)
                    result = "Memory will be committed with the final response"
                elif name == "delegate":
                    import re
                    if not re.fullmatch(r"[A-Za-z0-9_-]{1,120}", args["target_agent"]):
                        raise ValueError("Invalid agent identifier")
                    graph_mutation = {"action":"delegate","target_agent":args["target_agent"],"return_to_supervisor":True}
                    result = "Delegation will be committed with the final response"
                elif name in implementations:
                    result=implementations[name](workspace_dir=workspace,**args)
                    if not str(result).startswith("Error"):
                        if kind == "rag" and name == "query_knowledge":
                            record_verification(name, args["query"])
                        elif name == "generate_local_asset":
                            record_verification(name, args["output_path"])
                elif name == "execute_terminal_command":
                    parts = shlex.split(args["command"])
                    if kind in ("devops", "pentest"):
                        allowed = {"terraform":{"version","fmt","validate","plan"},"docker":{"version","images","inspect"},"kubectl":{"version","get","describe"},"bandit":{"--version","-r"}}
                        if len(parts)<2 or parts[0] not in allowed or parts[1] not in allowed[parts[0]] or any(c in args["command"] for c in ";&|`$<>\n"):
                            raise PermissionError("This specialist supports validation/read-only operations. Protected deployment or active scanning requires a separately authorized execution adapter.")
                    result = toolset.execute_terminal_command(args["command"],workspace,native)
                    if result.startswith("Exit code: 0\n"):
                        if _meaningful_terminal_verification(kind, parts):
                            record_verification(name, args["command"], covers_changes=True)
                        else:
                            script_path = _direct_script_verification_path(parts)
                            if script_path and script_path in pending_modified_paths:
                                record_verification(name, args["command"], checked_path=script_path)
                elif name == "write_file":
                    result = toolset.write_file(workspace_dir=workspace, files_modified=files, **args)
                    if result.startswith("Successfully"):
                        verified = False
                        written_path = _normalized_workspace_path(args["path"])
                        pending_modified_paths.add(written_path)
                        written_paths.add(written_path)
                elif name == "replace_file_content":
                    result = toolset.replace_file_content(workspace_dir=workspace, **args)
                    if result.startswith("Successfully"):
                        verified = False
                        written_path = _normalized_workspace_path(args["path"])
                        pending_modified_paths.add(written_path)
                        written_paths.add(written_path)
                else:
                    result = getattr(toolset,name)(workspace_dir=workspace, **args)
                    if not result.startswith("Error"):
                        if name == "read_file":
                            path = _normalized_workspace_path(args["path"])
                            if not pending_modified_paths or path in pending_modified_paths:
                                record_verification(name, path, checked_path=path)
            except Exception as exc:
                result = f"Error: {exc}"
            result_text = str(result)
            if result_text.startswith("Error"):
                detail = result_text.splitlines()[0][:300]
                _emit_llm("tool", f"\nFailed {name}: {detail}", name=name)
            else:
                _emit_llm("tool", f"\nCompleted {name}", name=name)
            messages.append({"role":"tool","tool_call_id":call.id,"content":result_text[:20000]})
        # A weak local model can keep exploring with other tools long after
        # verification is already satisfied, never returning to
        # mark_task_complete on its own. Surface that state explicitly rather
        # than relying on it to remember — mark_task_complete itself already
        # returns before reaching here on success, so getting this far with
        # `verified` true means it wasn't called (or wasn't the last call).
        if verified:
            messages.append({"role":"user","content":"Verification is already satisfied. Call mark_task_complete now instead of taking further actions."})
    _emit_llm("status", "Stopped: agent iteration budget exhausted without verified completion")
    if not effects_started:
        print("[RETICLE_RETRY_SAFE: NO_EFFECTS]", file=sys.stderr, flush=True)
    raise RuntimeError("Agent iteration budget exhausted without verified completion")

if __name__ == "__main__":
    raise SystemExit("Import run() from an agent entrypoint")
