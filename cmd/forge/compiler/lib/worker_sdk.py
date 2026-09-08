"""One worker protocol and tool loop for all generated/specialist workers."""
import json
import os
from pathlib import Path
import shlex
import sys
import time
import forge_utils as toolset

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
    definitions["delegate"] = ("Request a registered agent and then return to this supervisor", {"target_agent":"string"})
    implementations = {}
    if kind == "rag":
        import rag_tools
        for name, key in (("index_directory","path"),("query_knowledge","query"),("remove_path_from_index","path")):
            definitions[name] = (name.replace("_"," "), {key:"string"})
            implementations[name] = getattr(rag_tools,name)
    if kind == "frontend":
        import comfy_tools
        definitions["generate_local_asset"]=("Generate an image using configured local ComfyUI",{"prompt":"string","output_path":"string"})
        implementations["generate_local_asset"] = comfy_tools.generate_local_asset
    tools=[{"type":"function","function":{"name":name,"description":desc,"parameters":{"type":"object","properties":{k:{"type":v} for k,v in props.items()},"required":list(props),"additionalProperties":False}}} for name,(desc,props) in definitions.items()]
    verified = False
    effects_started = False
    messages = [{"role":"system", "content": instructions + "\n" + req.get("parameters",{}).get("system_prompt","") + "\nUse workspace-relative paths. Terminal cwd is src. Finish only after checking your work. An exhausted loop is a failure."},
                {"role":"user", "content":json.dumps({"prompt":req.get("parameters",{}).get("user_prompt",mem.get("user_prompt")),"inputs":req.get("inputs",[]),"context":mem.get("ide_context"),"attachments":mem.get("prompt_attachments"),"history":mem.get("prompt_history")})}]
    kwargs = {}
    key_name = req.get("parameters", {}).get("api_key")
    if model.startswith(("ollama/", "ollama_chat/")):
        kwargs["api_base"] = os.getenv("OLLAMA_HOST", "http://localhost:11434")
    elif key_name:
        kwargs["api_key"] = os.environ[key_name]
    started = time.monotonic()
    for iteration in range(30):
        if time.monotonic() - started > 900:
            raise TimeoutError("Agent time budget exhausted")
        try:
            response = completion(model=model, messages=messages, tools=tools, timeout=90, num_retries=0, **kwargs)
        except Exception:
            if not effects_started:
                print("[RETICLE_RETRY_SAFE: NO_EFFECTS]", file=sys.stderr, flush=True)
            raise
        message = response.choices[0].message
        messages.append(message.model_dump(exclude_none=True))
        if not message.tool_calls:
            messages.append({"role":"user","content":"Use tools to verify and finish with mark_task_complete."})
            continue
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
                if name == "remember":
                    if not args["key"] or len(args["key"]) > 120 or len(args["value_json"]) > 65536:
                        raise ValueError("Memory key/value exceeds the task limit")
                    memory_updates.append({"key":args["key"],"value":json.loads(args["value_json"]),"scope":"execution"})
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
