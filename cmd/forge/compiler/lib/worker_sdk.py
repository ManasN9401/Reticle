"""One worker protocol and tool loop for all generated/specialist workers."""
import json
import hashlib
import os
from pathlib import Path
import queue
import re
import shlex
import sys
import threading
import time
import http.client
import urllib.parse
from contextlib import nullcontext
import forge_utils as toolset

def _broker_request(path, method="GET", payload=None, timeout=35):
    """Call the attempt-scoped loopback broker without exposing credentials to the model."""
    base = os.environ.get("RETICLE_TOOL_BROKER_URL", "")
    token = os.environ.get("RETICLE_TOOL_BROKER_TOKEN", "")
    attempt = os.environ.get("RETICLE_ATTEMPT_ID", "")
    if not base or not token or not attempt:
        return None
    parsed = urllib.parse.urlparse(base)
    if parsed.scheme != "http" or parsed.hostname not in ("127.0.0.1", "::1", "localhost"):
        raise RuntimeError("Tool broker URL must be loopback HTTP")
    data = None if payload is None else json.dumps(payload, separators=(",", ":")).encode("utf-8")
    headers = {
        "Authorization": "Bearer " + token,
        "X-Reticle-Attempt-ID": attempt,
        "Content-Type": "application/json",
    }
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=timeout)
    try:
        connection.request(method, (parsed.path.rstrip("/") + path) or path, body=data, headers=headers)
        response = connection.getresponse()
        raw = response.read(1024 * 1024 + 1)
        status = response.status
        reason = response.reason
    finally:
        connection.close()
    if status >= 400:
        try:
            detail = json.loads(raw).get("error", {}).get("message", reason)
        except Exception:
            detail = reason
        raise RuntimeError(f"Tool broker rejected the request: {detail}") from None
    if len(raw) > 1024 * 1024:
        raise RuntimeError("Tool broker response exceeds the worker limit")
    return json.loads(raw)

def _broker_catalog():
    response = _broker_request("/v1/tools")
    if response is None:
        return {}
    tools = response.get("tools", [])
    catalog = {}
    for descriptor in tools:
        name = descriptor.get("name")
        schema = descriptor.get("schema")
        if not isinstance(name, str) or not isinstance(schema, dict) or schema.get("type") != "object":
            continue
        catalog[name] = descriptor
    return catalog

def _broker_call(descriptor, call_id, arguments):
    call_id = str(call_id or "")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,119}", call_id):
        digest = hashlib.sha256(call_id.encode("utf-8", errors="replace")).hexdigest()
        call_id = "provider-" + digest
    timeout = min(7205, max(5, int(descriptor.get("timeout_ms", 30000)) / 1000 + 5))
    response = _broker_request("/v1/calls", "POST", {
        "callId": call_id,
        "tool": descriptor["id"],
        "arguments": arguments,
    }, timeout=timeout)
    if not response or not response.get("ok"):
        error = (response or {}).get("error", {})
        raise RuntimeError(error.get("message", "Brokered tool call failed"))
    return response.get("result"), response.get("effect", "uncertain")

# Tools that change the workspace; replaying a worker after one has run is unsafe.
EFFECTFUL_LOCAL_TOOLS = frozenset({
    "write_file", "replace_file_content", "execute_terminal_command",
    "generate_local_asset", "generate_local_assets", "index_directory", "remove_path_from_index",
    "resize_image", "make_thumbnails", "convert_image", "crop_image",
})

_JSON_TYPES = {"string": str, "integer": int, "number": (int, float), "boolean": bool, "array": list, "object": dict}

def _tool_property(spec):
    """A property spec is a JSON type name, or a schema dict with optional "optional": True."""
    if isinstance(spec, str):
        return {"type": spec}, False
    schema = dict(spec)
    return schema, bool(schema.pop("optional", False))

def _json_type_matches(value, kind):
    if kind in ("integer", "number") and isinstance(value, bool):
        return False
    return isinstance(value, _JSON_TYPES[kind])

def _tool_argument_error(name, properties, args):
    """Describe how args violate a local tool's declared properties, or return None."""
    declared = {key: _tool_property(spec) for key, spec in properties.items()}
    required = sorted(key for key, (_, optional) in declared.items() if not optional)
    optional = sorted(key for key, (_, optional) in declared.items() if optional)
    problems = [f"missing '{key}'" for key in required if key not in args]
    problems += [f"unknown argument '{key}'" for key in sorted(args) if key not in declared]
    for key, (schema, _) in declared.items():
        if key in args and not _json_type_matches(args[key], schema["type"]):
            problems.append(f"'{key}' must be {schema['type']}")
    if not problems:
        return None
    def listing(keys):
        return ", ".join(f"{key} ({declared[key][0]['type']})" for key in keys) or "(none)"
    return f"Invalid arguments for '{name}': {'; '.join(problems)}. Required: {listing(required)}. Optional: {listing(optional)}."

def _local_tool_descriptor(name, description, properties, required_capability):
    declared = {key: _tool_property(spec) for key, spec in properties.items()}
    schema = {
        "type": "object",
        "properties": {key: prop for key, (prop, _) in declared.items()},
        "required": [key for key, (_, optional) in declared.items() if not optional],
        "additionalProperties": False,
    }
    return {
        "id": "builtin." + name,
        "name": name,
        "description": description,
        "schema": schema,
        "required_capability": required_capability or "builtin.unrestricted",
        "adapter": "python-local",
        "timeout_ms": 0,
        "output_limit": 20000,
        "effect": "effect_started" if name in EFFECTFUL_LOCAL_TOOLS else "no_effect",
        "available": True,
    }

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

def _without_src_prefix(path):
    # The terminal and tools work inside src/, so "src/index.html" and "index.html"
    # name the same deliverable to a model even though they are different paths.
    return path[4:] if path.startswith("src/") else path

def _output_satisfied(required, written_paths):
    required = _without_src_prefix(required)
    return any(
        w == required or w.startswith(required + "/") or required.startswith(w + "/")
        for w in map(_without_src_prefix, written_paths)
    )

def shared_memory_context(memory):
    """Expose dispatched facts, excluding runtime controls, with an explicit size limit."""
    controls = {"user_prompt", "workspace_dir", "max_retries", "allow_native_execution",
                "ide_context", "prompt_attachments", "prompt_history", "global_effort",
                "agent_complexity", "task_timeout_seconds", "llm_num_ctx",
                "llm_max_tokens", "llm_temperature", "llm_first_token_timeout_seconds",
                "llm_max_iterations", "ollama_keep_alive"}
    facts = {key: value for key, value in memory.items() if key not in controls}
    if len(json.dumps(facts, ensure_ascii=False).encode("utf-8")) > 65536:
        raise ValueError("Shared memory exceeds 64 KiB: select fewer required_memory keys or use summaries/artifact references")
    return facts

def build_user_context(req, memory):
    return {"prompt":req.get("parameters",{}).get("user_prompt",memory.get("user_prompt")),
            "inputs":req.get("inputs",[]), "context":memory.get("ide_context"),
            "attachments":memory.get("prompt_attachments"), "history":memory.get("prompt_history"),
            "shared_memory":shared_memory_context(memory), "memory_metadata":req.get("memory_metadata",{})}

# A 429 rejects the request before any generation, so waiting and repeating the
# same request is safe even after earlier tool rounds changed the workspace.
# Only the pre-stream call is retried; a failure while reading a stream may have
# produced partial output and keeps the dispatcher's no-effects rules.
RATE_LIMIT_WAITS = (10, 20, 40)
MAX_RATE_LIMIT_ADVICE = 120
# Phrases that mean waiting will not help. Keep them specific: provider messages
# often end with an "upgrade your plan" link, so a bare word such as "billing"
# (https://console.groq.com/settings/billing) once made a per-minute token limit
# look like an exhausted account.
NON_TRANSIENT_LIMITS = (
    "free-models-per-day", "openrouter_free_tier_daily", "insufficient",
    "exceeded your current quota", "credit balance",
)
_RETRY_HINT = re.compile(r"try again in ((?:\d+(?:\.\d+)?(?:ms|h|m|s))+)", re.IGNORECASE)
_DURATION_PART = re.compile(r"(\d+(?:\.\d+)?)(ms|h|m|s)", re.IGNORECASE)
_UNIT_SECONDS = {"ms": 0.001, "s": 1.0, "m": 60.0, "h": 3600.0}

def _advised_wait(error):
    """The provider's own wait: a Retry-After header, else a "try again in 8.4s" message."""
    headers = getattr(getattr(error, "response", None), "headers", None)
    try:
        return max(1.0, float(headers.get("retry-after")))
    except (AttributeError, TypeError, ValueError):
        pass
    hint = _RETRY_HINT.search(str(error))
    if not hint:
        return None
    seconds = sum(float(amount) * _UNIT_SECONDS[unit.lower()] for amount, unit in _DURATION_PART.findall(hint.group(1)))
    # The window rarely clears the instant the provider says, so allow a margin.
    return max(1.0, seconds) + 1.0

def _rate_limit_wait(error, attempt):
    """Seconds to wait before repeating a transient 429, or None to give up."""
    if attempt >= len(RATE_LIMIT_WAITS):
        return None
    if "RateLimitError" not in {cls.__name__ for cls in type(error).__mro__}:
        return None
    if any(marker in str(error).lower() for marker in NON_TRANSIENT_LIMITS):
        return None
    advised = _advised_wait(error)
    if advised is None:
        return RATE_LIMIT_WAITS[attempt]
    # A daily limit reports minutes or hours; only short waits are worth holding the worker.
    return advised if advised <= MAX_RATE_LIMIT_ADVICE else None

# Gateway and connection failures are as safe to repeat as a 429: the request never
# produced anything we kept. They are only retried here once the worker has changed
# the workspace, because before that the dispatcher can reroute to another model,
# which beats hammering a provider that is down or hung.
TRANSIENT_WAITS = (5, 15)
_TRANSIENT_CLASSES = frozenset({
    "Timeout", "APITimeoutError", "APIConnectionError", "ServiceUnavailableError",
    "BadGatewayError", "TimeoutError", "ConnectionError",
})
_TRANSIENT_STATUS = re.compile(r"(?:error code|status code|status)[: ]+50[234]\b|\b50[234] (?:bad gateway|service unavailable|gateway time-?out)", re.IGNORECASE)

def _transient_wait(error, attempt):
    """Seconds to wait before repeating a gateway timeout or connection failure, or None."""
    if attempt >= len(TRANSIENT_WAITS):
        return None
    if _TRANSIENT_CLASSES & {cls.__name__ for cls in type(error).__mro__} or _TRANSIENT_STATUS.search(str(error)):
        return TRANSIENT_WAITS[attempt]
    return None

def _start_within(completion, seconds, **request):
    """completion(**request), failing if the provider has not begun responding in time.

    The stream-silence timeout only starts once the response is open. A provider
    that queues the request before sending headers was bounded only by the 1800s
    request timeout, and one such request held a node for 28 minutes."""
    outcome = {}

    def call():
        try:
            outcome["value"] = completion(**request)
        except BaseException as exc:
            outcome["error"] = exc

    thread = threading.Thread(target=call, name="reticle-llm-request", daemon=True)
    thread.start()
    thread.join(seconds)
    if thread.is_alive():
        raise TimeoutError(f"Timeout Error: the provider did not start responding within {seconds}s (timed out)")
    if "error" in outcome:
        raise outcome["error"]
    return outcome["value"]

def _complete_with_retries(completion, started, start_timeout, can_reroute, **request):
    rate_limited = transient = 0
    while True:
        try:
            return _start_within(completion, start_timeout, **request)
        except Exception as error:
            wait = _rate_limit_wait(error, rate_limited)
            if wait is not None:
                rate_limited += 1
                label = f"Rate limited by the provider; repeating the same request in {wait:.0f}s (retry {rate_limited}/{len(RATE_LIMIT_WAITS)})"
            elif not can_reroute():
                wait = _transient_wait(error, transient)
                transient += 1
                label = f"Provider unavailable ({type(error).__name__}); repeating the same request in {wait or 0:.0f}s (retry {transient}/{len(TRANSIENT_WAITS)})"
            if wait is None or time.monotonic() - started + wait > 3600:
                raise
            _emit_llm("status", label)
            time.sleep(wait)

# Consecutive replies with neither text nor a tool call before the worker gives up.
MAX_EMPTY_REPLIES = 3

DEFAULT_ITERATIONS = 30
# Each model turn may issue several tool calls, but a gallery still needs many turns.
IMAGE_ITERATIONS = 60
MAX_ITERATIONS = 200

def _iteration_limit(memory, capabilities):
    """Model turns allowed per task: llm_max_iterations, clamped, else a capability default."""
    default = IMAGE_ITERATIONS if "image.local" in capabilities else DEFAULT_ITERATIONS
    try:
        requested = int(memory.get("llm_max_iterations", default))
    except (TypeError, ValueError):
        requested = default
    return max(1, min(requested, MAX_ITERATIONS))

# Roles whose purpose is to produce something. With no declared output files the generic
# completion check is satisfied by merely reading a file, which once let a frontend node
# "finish" a website by reading its design spec. They must change something first.
BUILDER_KINDS = frozenset({"frontend"})

# Image tools whose successful calls create workspace files that count as outputs.
IMAGE_OUTPUT_TOOLS = frozenset({
    "generate_local_asset", "generate_local_assets", "resize_image",
    "convert_image", "crop_image", "make_thumbnails",
})

def _generated_paths(name, args, result):
    """Output paths a successful image tool call actually produced."""
    if name in ("generate_local_asset", "resize_image", "convert_image", "crop_image"):
        return [args["output_path"]]
    key = "created" if name == "make_thumbnails" else "generated"
    try:
        return [path for path in json.loads(result).get(key, []) if isinstance(path, str)]
    except (ValueError, AttributeError):
        return []

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
        tool_capabilities["remember"] = "memory.execution"
        tool_capabilities["remember_if_version"] = "memory.execution"
    if "graph.delegate" in capabilities:
        definitions["delegate"] = ("Request a registered agent and then return to this supervisor", {"target_agent":"string"})
        tool_capabilities["delegate"] = "graph.delegate"
    implementations = {}
    if kind == "rag" and "rag.local" in capabilities:
        import rag_tools
        for name, key in (("index_directory","path"),("query_knowledge","query"),("remove_path_from_index","path")):
            definitions[name] = (name.replace("_"," "), {key:"string"})
            implementations[name] = getattr(rag_tools,name)
            tool_capabilities[name] = "rag.local"
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
        import image_tools
        definitions["generate_local_asset"]=(
            f"Generate an image using configured local ComfyUI. checkpoint, width and height are optional (width/height: multiples of 8 from 64 to 2048).{comfy_checkpoints}",
            {
                "prompt":"string",
                "output_path":"string",
                "checkpoint":{"type":"string","optional":True},
                "width":{"type":"integer","optional":True},
                "height":{"type":"integer","optional":True}
            }
        )
        implementations["generate_local_asset"] = comfy_tools.generate_local_asset
        tool_capabilities["generate_local_asset"] = "image.local"
        definitions["generate_local_assets"]=(
            f"Generate up to {comfy_tools.MAX_BATCH_ASSETS} images in one call (one model turn). Prefer this over repeated generate_local_asset calls. Each item: prompt, output_path, and optional checkpoint, width, height. Items fail independently; the result lists generated and failed paths.{comfy_checkpoints}",
            {
                "items":{
                    "type":"array",
                    "items":{
                        "type":"object",
                        "properties":{
                            "prompt":{"type":"string"},
                            "output_path":{"type":"string"},
                            "checkpoint":{"type":"string"},
                            "width":{"type":"integer"},
                            "height":{"type":"integer"}
                        },
                        "required":["prompt","output_path"],
                        "additionalProperties":False
                    }
                }
            }
        )
        implementations["generate_local_assets"] = comfy_tools.generate_local_assets
        tool_capabilities["generate_local_assets"] = "image.local"
        optional_int = {"type":"integer","optional":True}
        definitions["resize_image"]=(
            "Resize an image file. mode 'fit' (default) keeps the aspect ratio inside width x height (height optional); 'cover' fills width x height exactly and crops the overflow. Paths end in .png, .jpg, .jpeg or .webp.",
            {"source_path":"string","output_path":"string","width":"integer","height":optional_int,"mode":{"type":"string","optional":True,"enum":["fit","cover"]}}
        )
        definitions["make_thumbnails"]=(
            f"Create a thumbnail (longest side max_size, default 256) with the same file name for every image directly inside source_dir, in one call (up to {image_tools.MAX_THUMBNAILS}).",
            {"source_dir":"string","output_dir":"string","max_size":optional_int}
        )
        definitions["convert_image"]=(
            "Re-encode an image; the output extension picks the format (.png, .jpg, .jpeg, .webp). quality 1-100 applies to JPEG and WebP (default 85).",
            {"source_path":"string","output_path":"string","quality":optional_int}
        )
        definitions["crop_image"]=(
            "Crop an image to the pixel box left, top, right, bottom of the source image.",
            {"source_path":"string","output_path":"string","left":"integer","top":"integer","right":"integer","bottom":"integer"}
        )
        for tool in ("resize_image", "make_thumbnails", "convert_image", "crop_image"):
            implementations[tool] = getattr(image_tools, tool)
            tool_capabilities[tool] = "image.local"
    local_descriptors = {
        name: _local_tool_descriptor(name, desc, props, tool_capabilities.get(name))
        for name, (desc, props) in definitions.items()
    }
    tools=[{"type":"function","function":{"name":descriptor["name"],"description":descriptor["description"],"parameters":descriptor["schema"]}} for descriptor in local_descriptors.values()]
    broker_tools = _broker_catalog()
    for name, descriptor in broker_tools.items():
        if name in local_descriptors:
            raise RuntimeError(f"Brokered tool name collides with built-in tool: {name}")
        tools.append({"type":"function","function":{
            "name": name,
            "description": descriptor.get("description", ""),
            "parameters": descriptor["schema"],
        }})
    verified = False
    verification = []
    pending_modified_paths = set()
    written_paths = set()
    # A registered agent keeps its maintained worker, so the architect delivers its
    # node-specific prompt (goal, inputs, declared outputs) as a node parameter.
    # That prompt, when present, is the more specific source of declared outputs.
    required_outputs = _required_output_paths(str(req.get("parameters", {}).get("system_prompt", ""))) or _required_output_paths(instructions)
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
    configured_api_base = req.get("parameters", {}).get("llm_api_base")
    configured_request_model = req.get("parameters", {}).get("llm_request_model")
    if configured_api_base:
        model = "openai/" + str(configured_request_model or model.split("/", 1)[-1])
        kwargs["api_base"] = str(configured_api_base).rstrip("/")
        if key_name and key_name in os.environ:
            kwargs["api_key"] = os.environ[key_name]
        else:
            kwargs["api_key"] = "dummy"
    elif model.startswith(("ollama/", "ollama_chat/")):
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
    consecutive_empty_replies = 0
    for iteration in range(_iteration_limit(mem, capabilities)):
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
                response = _complete_with_retries(completion, started, first_event_timeout, lambda: not effects_started, model=model, messages=messages, tools=tools, timeout=1800, num_retries=0, extra_headers=extra_headers, stream=True, **kwargs)
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
            
            # A reply with no text and no tool call (a reasoning-only turn, or a model
            # that stalled) must never enter the history: strict providers reject an
            # empty assistant message with a 400, which ended a run that had already
            # written files and so could not be retried.
            if "tool_calls" not in message_dict and not str(message_dict.get("content") or "").strip():
                consecutive_empty_replies += 1
                if consecutive_empty_replies >= MAX_EMPTY_REPLIES:
                    _emit_llm("status", f"Stopped: model returned {MAX_EMPTY_REPLIES} consecutive empty responses")
                    raise RuntimeError(
                        f"Agent stalled: model returned empty responses for {MAX_EMPTY_REPLIES} consecutive iterations"
                    )
                messages.append({"role":"user","content":"Your last reply contained no text and no tool call. Call a tool to continue, or call mark_task_complete if the work is finished and verified."})
                continue
            consecutive_empty_replies = 0
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
                broker_descriptor = broker_tools.get(name)
                if expected is None and broker_descriptor is None:
                    # A weak model can hallucinate a plausible-looking tool name
                    # (e.g. echoing a node/agent id from its own prompt) instead of
                    # picking from its actual tool schema. A bare "unsupported"
                    # error gives it nothing to correct against, so list the real
                    # options — the same intent as the write_file/mark_task_complete
                    # error messages already do for their own failure modes.
                    raise ValueError(
                        f"'{name}' is not a real tool. Choose one of the tools actually "
                        f"available to you: {', '.join(sorted(set(definitions) | set(broker_tools)))}."
                    )
                if broker_descriptor is None:
                    if not isinstance(args, dict):
                        raise ValueError(f"Invalid arguments for '{name}': expected an object.")
                    argument_error = _tool_argument_error(name, expected[1], args)
                    if argument_error:
                        raise ValueError(argument_error)
                if name in EFFECTFUL_LOCAL_TOOLS:
                    effects_started = True
                if broker_descriptor is not None:
                    if not isinstance(args, dict):
                        raise ValueError(f"Invalid arguments for '{name}': expected an object.")
                    if broker_descriptor.get("effect") != "no_effect":
                        effects_started = True
                    result, broker_effect = _broker_call(broker_descriptor, call.id, args)
                    if broker_effect != "no_effect":
                        effects_started = True
                    if not isinstance(result, str):
                        result = json.dumps(result, ensure_ascii=False, separators=(",", ":"))
                elif name == "mark_task_complete":
                    if kind in BUILDER_KINDS and not required_outputs and not written_paths and not effects_started:
                        raise ValueError(
                            "This role builds something, but you have not created or changed anything yet; "
                            "reading files does not complete the task. Create the deliverable files with "
                            "write_file (images with generate_local_asset or generate_local_assets), verify them, "
                            "then call mark_task_complete."
                        )
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
                    sys.stdout.write(json.dumps({"id":req["id"],"memory":memory_updates,"graph_mutation":graph_mutation,"verification":verification,"files":sorted(written_paths),"artifact":{"id":req["id"]+"_output","name":kind+" output","type":"document/markdown","data":args["summary"]}})+"\n")
                    return
                elif name in ("remember", "remember_if_version"):
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
                        elif name in IMAGE_OUTPUT_TOOLS:
                            # A generated image is a written output; without this a node
                            # declaring image paths could never satisfy missing_outputs().
                            for output_path in _generated_paths(name, args, result):
                                written_paths.add(_normalized_workspace_path(output_path))
                                record_verification(name, output_path)
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
            # An empty tool message is rejected by some providers just like an empty reply.
            messages.append({"role":"tool","tool_call_id":call.id,"content":result_text[:20000] or "(no output)"})
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
