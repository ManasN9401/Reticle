import os, json, uuid, urllib.request, urllib.parse, time, sys
from contextlib import contextmanager
from pathlib import Path
from forge_utils import safe_path
def emit_log(msg): print(msg,file=sys.stderr,flush=True)
def ui_state(state): emit_log(f"[UI_STATE: {state}]")

def _local_endpoint(name, default):
    host = os.getenv(name, default).rstrip("/")
    if urllib.parse.urlsplit(host).hostname not in ("localhost", "127.0.0.1", "::1"):
        raise ValueError(f"{name} must identify a local endpoint for GPU coordination")
    return host

def _post_json(url, payload, timeout=5):
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        response.read(1024 * 1024)

def release_comfy_models(log=emit_log):
    """Ask an idle ComfyUI queue to release model and executor caches."""
    if os.getenv("RETICLE_COMFY_AUTO_UNLOAD", "true").lower() == "false":
        return
    try:
        host = _local_endpoint("COMFYUI_HOST", "http://127.0.0.1:8188")
        _post_json(host + "/free", {"unload_models": True, "free_memory": True})
        log("Requested ComfyUI model and cache unload")
    except Exception as exc:
        # Coordination is an optimization. An absent ComfyUI server must not
        # prevent a local language model from running.
        log(f"ComfyUI unload unavailable: {exc}")

def unload_ollama_models(log=emit_log):
    """Unload every currently resident Ollama model before an image job."""
    if os.getenv("RETICLE_OLLAMA_AUTO_UNLOAD", "true").lower() == "false":
        return
    try:
        host = _local_endpoint("OLLAMA_HOST", "http://127.0.0.1:11434")
        with urllib.request.urlopen(host + "/api/ps", timeout=3) as response:
            payload = json.loads(response.read(1024 * 1024))
        names = {
            item.get("name") or item.get("model")
            for item in payload.get("models", [])
            if isinstance(item, dict)
        }
        for name in sorted(value for value in names if isinstance(value, str) and value):
            _post_json(host + "/api/generate", {"model": name, "keep_alive": 0}, timeout=15)
            log(f"Unloaded Ollama model {name} before ComfyUI generation")
    except Exception as exc:
        # As above, do not turn optional resource cleanup into a job failure.
        log(f"Ollama unload unavailable: {exc}")

@contextmanager
def local_gpu_session(kind, log=emit_log):
    """Serialize local Ollama/ComfyUI GPU work across worker processes."""
    if os.getenv("RETICLE_LOCAL_GPU_COORDINATION", "true").lower() == "false":
        yield
        return

    root = Path(os.getenv("RETICLE_ROOT", os.getcwd())) / ".reticle"
    root.mkdir(parents=True, exist_ok=True)
    lock_path = root / "local-gpu.lock"
    stream = lock_path.open("a+b")
    acquired = False
    deadline = time.monotonic() + max(1, int(os.getenv("RETICLE_LOCAL_GPU_LOCK_TIMEOUT", "600")))
    try:
        while not acquired:
            try:
                stream.seek(0)
                if os.name == "nt":
                    import msvcrt
                    if stream.seek(0, os.SEEK_END) == 0:
                        stream.write(b"0")
                        stream.flush()
                    stream.seek(0)
                    msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
                else:
                    import fcntl
                    fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                acquired = True
            except (OSError, BlockingIOError):
                if time.monotonic() >= deadline:
                    raise TimeoutError("Timed out waiting for the shared local GPU")
                time.sleep(0.25)
        if kind == "ollama":
            release_comfy_models(log)
        elif kind == "comfy":
            unload_ollama_models(log)
        yield
    finally:
        if acquired:
            try:
                stream.seek(0)
                if os.name == "nt":
                    import msvcrt
                    msvcrt.locking(stream.fileno(), msvcrt.LK_UNLCK, 1)
                else:
                    import fcntl
                    fcntl.flock(stream.fileno(), fcntl.LOCK_UN)
            except OSError:
                pass
        stream.close()

def ollama_model_loaded(model):
    name = model.split("/", 1)[1] if "/" in model else model
    try:
        host = _local_endpoint("OLLAMA_HOST", "http://127.0.0.1:11434")
        with urllib.request.urlopen(host + "/api/ps", timeout=2) as response:
            payload = json.loads(response.read(1024 * 1024))
        return any(
            isinstance(item, dict) and (item.get("name") == name or item.get("model") == name)
            for item in payload.get("models", [])
        )
    except Exception:
        return False

def generate_local_asset(prompt: str, output_path: str, workspace_dir: str, checkpoint: str = None, width: int = 1024, height: int = 1024) -> str:
    with local_gpu_session("comfy"):
        try:
            return _generate_local_asset(prompt, output_path, workspace_dir, checkpoint, width, height)
        finally:
            release_comfy_models()

def _generate_local_asset(prompt: str, output_path: str, workspace_dir: str, checkpoint: str = None, width: int = 1024, height: int = 1024) -> str:
    """
    Sends a prompt to a local ComfyUI instance (http://localhost:8188) to generate an image.
    The image is saved directly to output_path.
    """
    emit_log(f"Executing generate_local_asset for prompt: '{prompt[:30]}...' -> {output_path}")
    
    output_path = str(safe_path(workspace_dir, output_path))
    if width < 64 or height < 64 or width > 2048 or height > 2048 or width % 8 or height % 8:
        raise ValueError("Image dimensions must be multiples of 8 from 64 to 2048")
    host = os.getenv("COMFYUI_HOST", "http://127.0.0.1:8188").rstrip("/")
    if urllib.parse.urlsplit(host).hostname not in ("localhost", "127.0.0.1", "::1"):
        raise ValueError("Only a configured local ComfyUI endpoint is supported")
    
    if not checkpoint:
        checkpoint = os.environ.get("COMFYUI_CHECKPOINT")
        
    if not checkpoint:
        # Fallback to fetching the first available checkpoint dynamically
        try:
            req_chk = urllib.request.Request(host+"/object_info/CheckpointLoaderSimple")
            with urllib.request.urlopen(req_chk, timeout=2) as res:
                chk_data = json.loads(res.read(1024*1024))
                ckpt_list = chk_data.get("CheckpointLoaderSimple", {}).get("input", {}).get("required", {}).get("ckpt_name", [[]])[0]
                if ckpt_list:
                    checkpoint = ckpt_list[0]
        except Exception as e:
            pass
            
    if not checkpoint:
        raise ValueError("Configure COMFYUI_CHECKPOINT from the server's installed checkpoints, or pass a valid checkpoint argument.")
    ui_state("WAITING_COMFY")
    client_id = str(uuid.uuid4())
    
    # Standard basic workflow for ComfyUI API (SDXL)
    workflow = {
        "3": {
            "class_type": "KSampler",
            "inputs": {
                "cfg": 8,
                "denoise": 1,
                "latent_image": ["5", 0],
                "model": ["4", 0],
                "positive": ["6", 0],
                "negative": ["7", 0],
                "sampler_name": "euler",
                "scheduler": "normal",
                "seed": int(time.time()),
                "steps": 20
            }
        },
        "4": {
            "class_type": "CheckpointLoaderSimple",
            "inputs": { "ckpt_name": checkpoint }
        },
        "5": {
            "class_type": "EmptyLatentImage",
            "inputs": { "batch_size": 1, "height": height, "width": width }
        },
        "6": {
            "class_type": "CLIPTextEncode",
            "inputs": { "clip": ["4", 1], "text": prompt }
        },
        "7": {
            "class_type": "CLIPTextEncode",
            "inputs": { "clip": ["4", 1], "text": "watermark, text, ugly, blurry" }
        },
        "8": {
            "class_type": "VAEDecode",
            "inputs": { "samples": ["3", 0], "vae": ["4", 2] }
        },
        "9": {
            "class_type": "SaveImage",
            "inputs": { "filename_prefix": "frontend_asset", "images": ["8", 0] }
        }
    }

    data = json.dumps({"prompt": workflow, "client_id": client_id}).encode('utf-8')
    req = urllib.request.Request(host+"/prompt", data=data)
    
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            res_data = json.loads(response.read(1024*1024))
            prompt_id = res_data.get('prompt_id')
            if not isinstance(prompt_id,str) or not prompt_id: raise ValueError('Missing ComfyUI job ID')
    except Exception as e:
        ui_state("RESUMED")
        return f"Error contacting ComfyUI on port 8188: {str(e)}. Is it running?"
    
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        try:
            req_hist = urllib.request.Request(f"{host}/history/{prompt_id}")
            with urllib.request.urlopen(req_hist, timeout=10) as h_res:
                hist = json.loads(h_res.read(1024*1024))
                if prompt_id in hist:
                    outputs = hist[prompt_id].get('outputs', {})
                    for node_id, node_output in outputs.items():
                        if 'images' in node_output:
                            img_data = node_output['images'][0]
                            filename = img_data.get('filename')
                            subfolder = img_data.get('subfolder', '')
                            img_url = host+"/view?"+urllib.parse.urlencode({"filename":filename,"subfolder":subfolder,"type":"output"})
                            req_img = urllib.request.Request(img_url)
                            with urllib.request.urlopen(req_img, timeout=10) as i_res:
                                img_bytes = i_res.read(20*1024*1024+1)
                                if len(img_bytes)>20*1024*1024: raise ValueError("Image exceeds 20 MiB")
                                os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)
                                with open(output_path, "xb") as f:
                                    f.write(img_bytes)
                            ui_state("RESUMED")
                            return f"Asset successfully generated and saved to {output_path}"
                    break
        except Exception as exc:
            emit_log(f"ComfyUI polling failed: {exc}")
        
        time.sleep(2)
        
    ui_state("RESUMED")
    raise TimeoutError("ComfyUI did not return an image within the job deadline; check server queue for unfinished work")
