/**
 * What a file's bytes really are, from its magic numbers.
 *
 * A worker saves whatever it is given under whatever name the model chose, and a local image
 * generator only produces PNG: one run had five `.jpg` textures and two `.glb` models that were
 * all PNG data. Trusting the extension gives a broken viewer for exactly those files.
 * Free of UI and Node imports so the preview server, the renderer and node:test can share it.
 */
export type SniffedKind = 'png' | 'jpeg' | 'gif' | 'webp' | 'bmp' | 'ico' | 'glb' | 'gltf' | 'unknown'

export interface Sniffed {
  kind: SniffedKind
  mime: string
  /** True for raster images a browser can display directly. */
  image: boolean
}

const UNKNOWN: Sniffed = { kind: 'unknown', mime: 'application/octet-stream', image: false }

function startsWith(bytes: ArrayLike<number>, signature: number[], offset = 0): boolean {
  if (bytes.length < offset + signature.length) return false
  return signature.every((value, index) => bytes[offset + index] === value)
}

const ascii = (text: string) => Array.from(text, (char) => char.charCodeAt(0))

export function sniffBinary(bytes: ArrayLike<number>): Sniffed {
  if (startsWith(bytes, [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])) return { kind: 'png', mime: 'image/png', image: true }
  if (startsWith(bytes, [0xff, 0xd8, 0xff])) return { kind: 'jpeg', mime: 'image/jpeg', image: true }
  if (startsWith(bytes, ascii('GIF87a')) || startsWith(bytes, ascii('GIF89a'))) return { kind: 'gif', mime: 'image/gif', image: true }
  if (startsWith(bytes, ascii('RIFF')) && startsWith(bytes, ascii('WEBP'), 8)) return { kind: 'webp', mime: 'image/webp', image: true }
  if (startsWith(bytes, ascii('BM'))) return { kind: 'bmp', mime: 'image/bmp', image: true }
  if (startsWith(bytes, [0x00, 0x00, 0x01, 0x00])) return { kind: 'ico', mime: 'image/x-icon', image: true }
  if (startsWith(bytes, ascii('glTF'))) return { kind: 'glb', mime: 'model/gltf-binary', image: false }
  // A .gltf is JSON with an "asset" block near the top.
  let at = 0
  while (at < bytes.length && at < 64 && [0x20, 0x09, 0x0a, 0x0d, 0xef, 0xbb, 0xbf].includes(bytes[at])) at += 1
  if (bytes[at] === 0x7b) {
    const head = String.fromCharCode(...Array.from({ length: Math.min(bytes.length, 4096) }, (_, index) => bytes[index]))
    if (/"asset"\s*:/.test(head)) return { kind: 'gltf', mime: 'model/gltf+json', image: false }
  }
  return UNKNOWN
}

/** "PNG", "JPEG", ... for messages. */
export function describeSniffed(sniffed: Sniffed): string {
  return sniffed.kind === 'unknown' ? 'unrecognised data' : sniffed.kind.toUpperCase()
}
