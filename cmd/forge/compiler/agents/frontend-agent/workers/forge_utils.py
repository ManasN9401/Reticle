import os
import json
import uuid
import base64
import urllib.request
import time

def emit_log(msg):
    print(f"[TOOL] {msg}")

def ui_state(state):
    print(f"[UI_STATE: {state}]", flush=True)

def write_file(path: str, content: str) -> str:
    emit_log(f"Executing write_file: {path}")
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(content)
    return f"Successfully wrote to {path}"

def read_file(path: str) -> str:
    emit_log(f"Executing read_file: {path}")
    if not os.path.exists(path):
        return f"Error: {path} not found."
    with open(path, "r", encoding="utf-8") as f:
        return f.read()

def generate_local_asset(prompt: str, output_path: str, width: int = 1024, height: int = 1024) -> str:
    """
    Sends a prompt to a local ComfyUI instance (http://localhost:8188) to generate an image.
    The image is saved directly to output_path.
    """
    emit_log(f"Executing generate_local_asset for prompt: '{prompt[:30]}...' -> {output_path}")
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
            "inputs": { "ckpt_name": "DreamShaperXL_Turbo_v2_1.safetensors" }
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
    req = urllib.request.Request("http://127.0.0.1:8188/prompt", data=data)
    
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            res_data = json.loads(response.read())
            prompt_id = res_data.get('prompt_id')
    except Exception as e:
        ui_state("RESUMED")
        return f"Error contacting ComfyUI on port 8188: {str(e)}. Is it running?"
    
    while True:
        try:
            req_hist = urllib.request.Request(f"http://127.0.0.1:8188/history/{prompt_id}")
            with urllib.request.urlopen(req_hist) as h_res:
                hist = json.loads(h_res.read())
                if prompt_id in hist:
                    outputs = hist[prompt_id].get('outputs', {})
                    for node_id, node_output in outputs.items():
                        if 'images' in node_output:
                            img_data = node_output['images'][0]
                            filename = img_data.get('filename')
                            subfolder = img_data.get('subfolder', '')
                            img_url = f"http://127.0.0.1:8188/view?filename={filename}&subfolder={subfolder}&type=output"
                            req_img = urllib.request.Request(img_url)
                            with urllib.request.urlopen(req_img) as i_res:
                                img_bytes = i_res.read()
                                os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)
                                with open(output_path, "wb") as f:
                                    f.write(img_bytes)
                            ui_state("RESUMED")
                            return f"Asset successfully generated and saved to {output_path}"
                    break
        except Exception:
            pass
        
        time.sleep(2)
        
    ui_state("RESUMED")
    return "Generation completed but image output was not found."

TOOLS_REGISTRY = [
    {
        "type": "function",
        "function": {
            "name": "write_file",
            "description": "Write content to a local file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"},
                    "content": {"type": "string"}
                },
                "required": ["path", "content"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "read_file",
            "description": "Read content from a local file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"}
                },
                "required": ["path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "generate_local_asset",
            "description": "Generate a custom image/asset using the local GPU and ComfyUI. Provide a descriptive text prompt and an absolute path to save the resulting image file (e.g., C:/workspace/src/assets/hero.png).",
            "parameters": {
                "type": "object",
                "properties": {
                    "prompt": {"type": "string", "description": "Highly descriptive text prompt for the image."},
                    "output_path": {"type": "string", "description": "Absolute path to save the .png file."},
                    "width": {"type": "integer", "description": "Image width (default 1024)"},
                    "height": {"type": "integer", "description": "Image height (default 1024)"}
                },
                "required": ["prompt", "output_path"]
            }
        }
    }
]

def execute_tool(name: str, args: dict) -> str:
    if name == "write_file":
        return write_file(args.get("path"), args.get("content"))
    elif name == "read_file":
        return read_file(args.get("path"))
    elif name == "generate_local_asset":
        return generate_local_asset(args.get("prompt"), args.get("output_path"), args.get("width", 1024), args.get("height", 1024))
    else:
        return f"Unknown tool: {name}"
