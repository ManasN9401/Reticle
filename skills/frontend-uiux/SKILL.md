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
- **Patience:** Asset generation takes time. You may request multiple assets, but wait patiently for each tool call to complete.

## 3. Technology Stack
- You are free to use Vanilla HTML/CSS/JS for simple immersive pages, or Next.js/React/Tailwind if a full web app architecture is required by the prompt.
- If using Vanilla, utilize CDNs for external animation libraries (e.g., Anime.js).
