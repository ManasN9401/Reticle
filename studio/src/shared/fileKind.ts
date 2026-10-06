/** Which viewer a file needs. Kept free of UI imports so plain node:test can load it. */
export type FileKind = 'image' | 'text'

const IMAGE_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'svg', 'ico', 'bmp'])

export function extensionOf(path: string): string {
  const name = path.slice(Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) + 1)
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : ''
}

export function fileKindFor(path: string): FileKind {
  return IMAGE_EXTENSIONS.has(extensionOf(path)) ? 'image' : 'text'
}
