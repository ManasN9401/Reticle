"""Local image editing tools (resize, thumbnails, convert, crop) built on Pillow.

Every path is workspace-relative and resolved with safe_path, like the other file
tools. Pillow is imported lazily so a worker environment without it reports a
clear error instead of failing at import time.
"""
import json
from forge_utils import safe_path

MAX_INPUT_BYTES = 50 * 1024 * 1024
MAX_INPUT_PIXELS = 64_000_000
MAX_OUTPUT_DIMENSION = 4096
MAX_THUMBNAILS = 100
IMAGE_FORMATS = {".png": "PNG", ".jpg": "JPEG", ".jpeg": "JPEG", ".webp": "WEBP"}

def _pillow():
    try:
        from PIL import Image, ImageOps
    except ImportError:
        raise ValueError("Pillow is not installed in this agent's environment; add the 'Pillow' dependency to one of its skills") from None
    Image.MAX_IMAGE_PIXELS = MAX_INPUT_PIXELS
    return Image, ImageOps

def _format_for(path):
    suffix = path.suffix.lower()
    if suffix not in IMAGE_FORMATS:
        raise ValueError("Image files must end in .png, .jpg, .jpeg or .webp")
    return IMAGE_FORMATS[suffix]

def _dimension(value, name):
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= MAX_OUTPUT_DIMENSION:
        raise ValueError(f"{name} must be an integer from 1 to {MAX_OUTPUT_DIMENSION}")
    return value

def _load(workspace_dir, relative):
    Image, ImageOps = _pillow()
    source = safe_path(workspace_dir, relative)
    _format_for(source)
    if not source.is_file():
        raise ValueError(f"Source image not found: {relative}")
    if source.stat().st_size > MAX_INPUT_BYTES:
        raise ValueError("Source image is larger than 50 MiB")
    try:
        image = Image.open(source)
    except Image.DecompressionBombError:
        raise ValueError(f"Source image exceeds {MAX_INPUT_PIXELS:,} pixels") from None
    if image.width * image.height > MAX_INPUT_PIXELS:
        raise ValueError(f"Source image exceeds {MAX_INPUT_PIXELS:,} pixels")
    image.load()
    return ImageOps.exif_transpose(image), Image, ImageOps

def _save(image, workspace_dir, relative, quality=85):
    target = safe_path(workspace_dir, relative)
    fmt = _format_for(target)
    if fmt == "JPEG" and image.mode not in ("RGB", "L"):
        image = image.convert("RGB")
    target.parent.mkdir(parents=True, exist_ok=True)
    options = {"quality": quality} if fmt in ("JPEG", "WEBP") else {"optimize": True}
    image.save(target, format=fmt, **options)

def resize_image(source_path: str, output_path: str, width: int, workspace_dir: str, height: int = None, mode: str = "fit") -> str:
    """fit keeps the aspect ratio inside width x height (height optional);
    cover fills width x height exactly, cropping the overflow."""
    _dimension(width, "width")
    if height is not None:
        _dimension(height, "height")
    if mode not in ("fit", "cover"):
        raise ValueError("mode must be 'fit' or 'cover'")
    if mode == "cover" and height is None:
        raise ValueError("mode 'cover' needs a height")
    image, Image, ImageOps = _load(workspace_dir, source_path)
    if mode == "cover":
        resized = ImageOps.fit(image, (width, height), Image.LANCZOS)
    elif height is None:
        resized = image.resize((width, max(1, round(image.height * width / image.width))), Image.LANCZOS)
    else:
        resized = ImageOps.contain(image, (width, height), Image.LANCZOS)
    _save(resized, workspace_dir, output_path)
    return f"Saved {output_path} ({resized.width}x{resized.height})"

def convert_image(source_path: str, output_path: str, workspace_dir: str, quality: int = 85) -> str:
    """Re-encode an image; the output extension picks the format."""
    if isinstance(quality, bool) or not isinstance(quality, int) or not 1 <= quality <= 100:
        raise ValueError("quality must be an integer from 1 to 100")
    image, _, _ = _load(workspace_dir, source_path)
    _save(image, workspace_dir, output_path, quality)
    return f"Saved {output_path} ({image.width}x{image.height})"

def crop_image(source_path: str, output_path: str, left: int, top: int, right: int, bottom: int, workspace_dir: str) -> str:
    """Crop to the box (left, top, right, bottom) in pixels of the source image."""
    image, _, _ = _load(workspace_dir, source_path)
    for name, value in (("left", left), ("top", top), ("right", right), ("bottom", bottom)):
        if isinstance(value, bool) or not isinstance(value, int):
            raise ValueError(f"{name} must be an integer")
    if not (0 <= left < right <= image.width and 0 <= top < bottom <= image.height):
        raise ValueError(f"Crop box must satisfy 0 <= left < right <= {image.width} and 0 <= top < bottom <= {image.height}")
    cropped = image.crop((left, top, right, bottom))
    _save(cropped, workspace_dir, output_path)
    return f"Saved {output_path} ({cropped.width}x{cropped.height})"

def make_thumbnails(source_dir: str, output_dir: str, workspace_dir: str, max_size: int = 256) -> str:
    """Write a thumbnail (longest side <= max_size, same file name) for every image
    directly inside source_dir. Returns JSON {"created": [...], "failed": [...]}."""
    _dimension(max_size, "max_size")
    _pillow()
    source = safe_path(workspace_dir, source_dir)
    destination = safe_path(workspace_dir, output_dir)
    if not source.is_dir():
        raise ValueError(f"Source directory not found: {source_dir}")
    if source.resolve() == destination.resolve():
        raise ValueError("output_dir must differ from source_dir")
    names = sorted(entry.name for entry in source.iterdir() if entry.is_file() and entry.suffix.lower() in IMAGE_FORMATS)
    if not names:
        raise ValueError(f"No .png, .jpg, .jpeg or .webp images in {source_dir}")
    if len(names) > MAX_THUMBNAILS:
        raise ValueError(f"At most {MAX_THUMBNAILS} images per call; found {len(names)}")
    created, failed = [], []
    for name in names:
        source_relative = f"{source_dir.rstrip('/')}/{name}"
        output_relative = f"{output_dir.rstrip('/')}/{name}"
        try:
            image, Image, ImageOps = _load(workspace_dir, source_relative)
            _save(ImageOps.contain(image, (max_size, max_size), Image.LANCZOS), workspace_dir, output_relative)
        except Exception as error:
            failed.append({"source": source_relative, "error": str(error)})
        else:
            created.append(output_relative)
    if not created:
        return "Error: no thumbnail was created. " + json.dumps(failed)
    return json.dumps({"created": created, "failed": failed})
