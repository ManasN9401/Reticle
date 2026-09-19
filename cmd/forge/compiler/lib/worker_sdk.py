"""One worker protocol and tool loop for all generated/specialist workers."""
import json
import os
from pathlib import Path
import shlex
import sys
import time
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

def _normalized_workspace_path(path):
    return Path(os.path.normpath(path)).as_posix()

def shared_memory_context(memory):
    """Expose dispatched facts, excluding runtime controls, with an explicit size limit."""
    controls = {"user_prompt", "workspace_dir", "max_retries", "allow_native_execution",
                "ide_context", "prompt_attachments", "prompt_history", "global_effort",
                "agent_complexity", "task_timeout_seconds", "llm_num_ctx",
                "llm_max_tokens", "llm_temperature"}
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
    effects_started = False
    def record_verification(tool, target, checked_path=None, covers_changes=False):
        nonlocal verified
        verification.append({"tool": tool, "target": target, "outcome": "succeeded"})
        if covers_changes:
            pending_modified_paths.clear()
        elif checked_path is not None:
            pending_modified_paths.discard(checked_path)
        verified = bool(verification) and not pending_modified_paths
    messages = [{"role":"system", "content": instructions + "\n" + req.get("parameters",{}).get("system_prompt","") + "\nUse workspace-relative paths. Terminal cwd is src. Finish only after checking your work. An exhausted loop is a failure."},
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
                response_chunks = response
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
                if expected is None or set(args) != set(expected[1]) or not all(isinstance(v,str) for v in args.values()):
                    raise ValueError("Unsupported tool or invalid arguments")
                if name in ("write_file", "replace_file_content", "execute_terminal_command", "generate_local_asset", "index_directory", "remove_path_from_index"):
                    effects_started = True
                if name == "mark_task_complete":
                    if not verified:
                        raise ValueError("No successful verification has been recorded")
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
                    if result.startswith("Exit code: 0\n") and _meaningful_terminal_verification(kind, parts):
                        record_verification(name, args["command"], covers_changes=True)
                elif name == "write_file":
                    result = toolset.write_file(workspace_dir=workspace, files_modified=files, **args)
                    verified = False
                    if result.startswith("Successfully"):
                        pending_modified_paths.add(_normalized_workspace_path(args["path"]))
                elif name == "replace_file_content":
                    result = toolset.replace_file_content(workspace_dir=workspace, **args)
                    verified = False
                    if result.startswith("Successfully"):
                        pending_modified_paths.add(_normalized_workspace_path(args["path"]))
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
    _emit_llm("status", "Stopped: agent iteration budget exhausted without verified completion")
    if not effects_started:
        print("[RETICLE_RETRY_SAFE: NO_EFFECTS]", file=sys.stderr, flush=True)
    raise RuntimeError("Agent iteration budget exhausted without verified completion")

if __name__ == "__main__":
    raise SystemExit("Import run() from an agent entrypoint")
