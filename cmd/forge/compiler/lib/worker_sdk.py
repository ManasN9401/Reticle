"""One worker protocol and tool loop for all generated/specialist workers."""
import json
import os
from pathlib import Path
import shlex
import sys
import time
import forge_utils as toolset

def shared_memory_context(memory):
    """Expose dispatched facts, excluding runtime controls, with an explicit size limit."""
    controls = {"user_prompt", "workspace_dir", "max_retries", "allow_native_execution",
                "ide_context", "prompt_attachments", "prompt_history", "global_effort",
                "agent_complexity", "task_timeout_seconds"}
    facts = {key: value for key, value in memory.items() if key not in controls}
    if len(json.dumps(facts, ensure_ascii=False).encode("utf-8")) > 65536:
        raise ValueError("Shared memory exceeds 64 KiB: select fewer required_memory keys or use summaries/artifact references")
    return facts

def build_user_context(req, memory):
    return {"prompt":req.get("parameters",{}).get("user_prompt",memory.get("user_prompt")),
            "inputs":req.get("inputs",[]), "context":memory.get("ide_context"),
            "attachments":memory.get("prompt_attachments"), "history":memory.get("prompt_history"),
            "shared_memory":shared_memory_context(memory), "memory_metadata":req.get("memory_metadata",{})}

def run(instructions, kind="coding"):
    req = json.load(sys.stdin)
    model = req.get("parameters", {}).get("llm_model")
    if not model:
        raise ValueError("No model was routed for this task")
    from litellm import completion
    mem = req.get("memory", {})
    workspace = mem["workspace_dir"]
    os.environ["RETICLE_EXECUTION_ID"] = req.get("execution", "")
    native = str(mem.get("allow_native_execution", False)).lower() == "true"
    files = {}
    memory_updates = []
    graph_mutation = None
    definitions = dict(toolset._definitions)
    definitions["remember"] = ("Save a JSON value in this execution's memory", {"key":"string", "value_json":"string"})
    definitions["remember_if_version"] = ("Update an execution-memory key only if its execution-scoped revision in memory_metadata still matches; use 0 to create an absent execution key", {"key":"string", "value_json":"string", "expected_version":"string"})
    definitions["delegate"] = ("Request a registered agent and then return to this supervisor", {"target_agent":"string"})
    implementations = {}
    if kind == "rag":
        import rag_tools
        for name, key in (("index_directory","path"),("query_knowledge","query"),("remove_path_from_index","path")):
            definitions[name] = (name.replace("_"," "), {key:"string"})
            implementations[name] = getattr(rag_tools,name)
    import comfy_tools
    import urllib.request, urllib.parse
    comfy_checkpoints = ""
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
    effects_started = False
    messages = [{"role":"system", "content": instructions + "\n" + req.get("parameters",{}).get("system_prompt","") + "\nUse workspace-relative paths. Terminal cwd is src. Finish only after checking your work. An exhausted loop is a failure."},
                {"role":"user", "content":json.dumps(build_user_context(req, mem))}]
    kwargs = {}
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
    
    if "llm_num_ctx" in mem:
        kwargs["num_ctx"] = int(mem["llm_num_ctx"])
    if "llm_max_tokens" in mem:
        kwargs["max_tokens"] = int(mem["llm_max_tokens"])
    if "llm_temperature" in mem:
        kwargs["temperature"] = float(mem["llm_temperature"])

    started = time.monotonic()
    for iteration in range(30):
        if time.monotonic() - started > 3600:
            raise TimeoutError("Agent time budget exhausted")
        try:
            extra_headers = {
                "HTTP-Referer": "https://github.com/ManasN9401/Reticle",
                "X-Title": "Reticle Agentic Harness",
            }
            response = completion(model=model, messages=messages, tools=tools, timeout=1800, num_retries=0, extra_headers=extra_headers, stream=True, **kwargs)
            content_buffer = []
            tool_calls_buffer = {}
            for chunk in response:
                delta = chunk.choices[0].delta
                if hasattr(delta, "content") and delta.content:
                    # Stream JSON chunk to stderr
                    sys.stderr.write(f"\n[LLM_STREAM] {json.dumps(delta.content)}\n")
                    sys.stderr.flush()
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
                        else:
                            if hasattr(tc, "id") and tc.id:
                                tool_calls_buffer[idx]["id"] = tc.id
                            if hasattr(tc, "function") and hasattr(tc.function, "name") and tc.function.name:
                                tool_calls_buffer[idx]["function"]["name"] = tc.function.name
                        if hasattr(tc, "function") and hasattr(tc.function, "arguments") and tc.function.arguments:
                            tool_calls_buffer[idx]["function"]["arguments"] += tc.function.arguments
                            sys.stderr.write(f"\n[LLM_STREAM] {json.dumps(tc.function.arguments)}\n")
                            sys.stderr.flush()

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
                    sys.stdout.write(json.dumps({"id":req["id"],"memory":memory_updates,"graph_mutation":graph_mutation,"artifact":{"id":req["id"]+"_output","name":kind+" output","type":"document/markdown","data":args["summary"]}})+"\n")
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
                elif name == "execute_terminal_command":
                    if kind in ("devops", "pentest"):
                        parts = shlex.split(args["command"])
                        allowed = {"terraform":{"version","fmt","validate","plan"},"docker":{"version","images","inspect"},"kubectl":{"version","get","describe"},"bandit":{"--version","-r"}}
                        if len(parts)<2 or parts[0] not in allowed or parts[1] not in allowed[parts[0]] or any(c in args["command"] for c in ";&|`$<>\n"):
                            raise PermissionError("This specialist supports validation/read-only operations. Protected deployment or active scanning requires a separately authorized execution adapter.")
                    result = toolset.execute_terminal_command(args["command"],workspace,native)
                    verified = verified or result.startswith("Exit code: 0\n")
                elif name == "write_file":
                    result = toolset.write_file(workspace_dir=workspace, files_modified=files, **args)
                    verified = False
                elif name == "replace_file_content":
                    result = toolset.replace_file_content(workspace_dir=workspace, **args)
                    verified = False
                else:
                    result = getattr(toolset,name)(workspace_dir=workspace, **args)
                    if kind in ("writing","rag","frontend") and name in ("read_file","list_dir") and not result.startswith("Error"):
                        verified = True
            except Exception as exc:
                result = f"Error: {exc}"
            messages.append({"role":"tool","tool_call_id":call.id,"content":str(result)[:20000]})
    raise RuntimeError("Agent iteration budget exhausted without verified completion")

if __name__ == "__main__":
    raise SystemExit("Import run() from an agent entrypoint")
