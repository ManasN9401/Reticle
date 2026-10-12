/** Which viewer a file needs. Kept free of UI imports so plain node:test can load it. */
export type FileKind = 'image' | 'model' | 'text'

const IMAGE_EXTENSIONS = new Set([
  'png', 'apng', 'jpg', 'jpeg', 'jfif', 'gif', 'webp', 'avif', 'svg', 'ico', 'bmp',
])
// glTF 2.0, the format three.js sites load. .gltf is JSON that may reference other files.
const MODEL_EXTENSIONS = new Set(['glb', 'gltf'])

export function extensionOf(path: string): string {
  const name = path.slice(Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) + 1)
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : ''
}

export function fileKindFor(path: string): FileKind {
  const extension = extensionOf(path)
  if (IMAGE_EXTENSIONS.has(extension)) return 'image'
  if (MODEL_EXTENSIONS.has(extension)) return 'model'
  return 'text'
}

/** Files opened from bytes (an image or a 3D model) rather than as text in the editor. */
export function isBinaryKind(kind: FileKind): boolean {
  return kind !== 'text'
}
