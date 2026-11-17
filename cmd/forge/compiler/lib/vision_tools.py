"""Bounded workspace images for model input; original artifacts are never changed."""
import base64
import hashlib
import io
import warnings

from forge_utils import safe_path

MAX_INPUT_BYTES = 16 * 1024 * 1024
MAX_PIXELS = 32_000_000
MAX_ENCODED_BYTES = 1024 * 1024


def view_image(workspace_dir, path, max_side=1280):
    if isinstance(max_side, bool) or not isinstance(max_side, int) or not 256 <= max_side <= 2048:
        raise ValueError("max_side must be between 256 and 2048")
    target = safe_path(workspace_dir, path)
    if target.suffix.lower() not in {".png", ".jpg", ".jpeg", ".webp"}:
        raise ValueError("Only local PNG, JPEG and WebP images are supported")
    if not target.is_file():
        raise ValueError("Image must be a regular workspace file")
    with target.open("rb") as stream:
        raw = stream.read(MAX_INPUT_BYTES+1)
    if len(raw) > MAX_INPUT_BYTES:
        raise ValueError("Image exceeds the 16 MiB input budget")
    try:
        from PIL import Image, ImageOps
    except ImportError:
        raise ValueError("Pillow is unavailable; use a registered skill declaring Pillow (such as frontend-uiux)") from None
    with warnings.catch_warnings():
        warnings.simplefilter("error", Image.DecompressionBombWarning)
        with Image.open(io.BytesIO(raw)) as original:
            if original.format not in {"PNG", "JPEG", "WEBP"} or original.width*original.height > MAX_PIXELS:
                raise ValueError("Unsupported image format or more than 32 million pixels")
            original.seek(0)
            image = ImageOps.exif_transpose(original)
            image.thumbnail((max_side, max_side))
            # Render transparency against white instead of discarding its alpha channel.
            rgba = image.convert("RGBA")
            background = Image.new("RGBA", rgba.size, "white")
            background.alpha_composite(rgba)
            image = background.convert("RGB")
    quality = 85
    while True:
        output = io.BytesIO()
        image.save(output, format="JPEG", quality=quality)
        encoded = base64.b64encode(output.getvalue()).decode("ascii")
        if len(encoded) <= MAX_ENCODED_BYTES:
            break
        if quality > 45:
            quality -= 20
        else:
            image.thumbnail((max(1, image.width//2), max(1, image.height//2)))
    metadata = {"path": path, "source_sha256": hashlib.sha256(raw).hexdigest(),
                "width": image.width, "height": image.height, "encoded_bytes": len(encoded)}
    part = {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,"+encoded}}
    return metadata, part


def discard_images(messages):
    """Keep textual provenance while removing image payloads before text compaction."""
    saved = 0
    for message in messages:
        if not isinstance(message.get("content"), list):
            continue
        parts = []
        for part in message["content"]:
            if part.get("type") == "image_url":
                saved += len(part.get("image_url", {}).get("url", ""))
                parts.append({"type": "text", "text": "[Earlier image removed to fit the request; use view_image again if needed.]"})
            else:
                parts.append(part)
        message["content"] = parts
    return saved
