---
name: Modern Frontend UI/UX Design
description: Strict methodology for building highly interactive, visually immersive web applications.
---

# Frontend UI/UX Methodology

This skill equips the agent to act as a World-Class Frontend Designer and Engineer, specializing in premium, highly-interactive web experiences (e.g. Manus AI, Higgsfield, Awwwards winners).

## 1. Aesthetic Mandates
- **No Boring Layouts:** You MUST avoid standard, dry Bootstrap-style layouts. You are expected to use modern design paradigms: Glassmorphism, dark modes, vibrant tailored color palettes, subtle gradients, and bento-box grid layouts.
- **Micro-Animations:** Interfaces must feel alive. You MUST integrate CSS micro-animations, hover states, and smooth transitions (using Tailwind, Framer Motion, or Anime.js).
- **Typography:** You MUST use modern typography (e.g. Inter, Outfit, or Space Grotesk via Google Fonts) instead of default browser fonts.

## 2. Asset Generation (NO PLACEHOLDERS)
- **Placeholder Ban:** You are STRICTLY FORBIDDEN from using generic placeholder images (e.g., `via.placeholder.com` or blank gray boxes). 
- **ComfyUI Integration:** When you need a background image, a hero graphic, or an icon, you MUST use the `generate_local_asset` tool. This tool sends your prompt to a local Stable Diffusion / Flux GPU cluster, which will generate the stunning, high-res graphic and return a local file path you can embed in your HTML.
- **Batch First:** Each model turn is limited, so generate several images at once with `generate_local_assets` (up to 12 `{prompt, output_path}` items per call) instead of one `generate_local_asset` call per image. `checkpoint`, `width` and `height` are optional.
- **Thumbnails and Resizing:** Do not regenerate an image to make a smaller copy. Use `make_thumbnails` (one call for a whole directory), `resize_image`, `convert_image` or `crop_image` on the files you already generated.
- **Patience:** Asset generation takes time. Wait patiently for each tool call to complete, and check the `generated` and `failed` lists a batch call returns.

## 3. Make The Page Actually Run
A generated page that is dead on arrival is a failure, however good it looks. Pages have repeatedly shipped with a script the browser refuses to run, so nothing animated and the images never showed.
- **Never mix module syntax with a classic script.** A plain `<script src="main.js">` cannot contain `import` or `export`: the browser stops at the first line ("Cannot use import statement outside a module") and nothing runs. Pick one:
  - **Simplest (preferred):** load libraries from a CDN as globals and write plain script code with no `import` lines. Three.js r128 from cdnjs gives a global `THREE`; GSAP gives `gsap`.
  - **Or modules:** `<script type="module" src="main.js">` plus an import map in the page for every bare name you import (`"three"`, `"gsap"`). A bare `import ... from 'three'` with no import map also fails.
- **A renderer needs a real canvas.** `new THREE.WebGLRenderer({ canvas: el })` requires `el` to be a `<canvas>` element, not a `<div>`.
- **Every generated image must appear in the page.** Reference each file you create from the HTML, CSS, or script (for example as a texture or an `<img>`), using a path relative to the page.
- **Check before you finish.** After writing web files, call `check_web_page`, fix every problem it reports, and run it again until it reports none. You cannot complete the task with unchecked web output.

A minimal page that runs (three.js from a CDN as a global, no imports):
```html
<canvas id="stage"></canvas>
<script src="https://cdnjs.cloudflare.com/ajax/libs/three.js/r128/three.min.js"></script>
<script src="main.js"></script>
```
```js
// main.js - a classic script: no import/export
const renderer = new THREE.WebGLRenderer({ canvas: document.getElementById('stage'), antialias: true });
const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(60, innerWidth / innerHeight, 0.1, 100);
camera.position.z = 5;
new THREE.TextureLoader().load('assets/gallery-01.png', (texture) => {
  scene.add(new THREE.Mesh(new THREE.PlaneGeometry(3, 2), new THREE.MeshBasicMaterial({ map: texture })));
});
renderer.setSize(innerWidth, innerHeight);
renderer.setAnimationLoop(() => { scene.rotation.y += 0.003; renderer.render(scene, camera); });
```

## 4. Technology Stack
- You are free to use Vanilla HTML/CSS/JS for simple immersive pages, or Next.js/React/Tailwind if a full web app architecture is required by the prompt.
- If using Vanilla, utilize CDNs for external animation libraries (e.g., Anime.js).
