import { useEffect, useRef, useState } from 'react'
import { ExternalLink, Maximize2, Minimize2, RotateCw } from 'lucide-react'
import { EmptyState, IconButton, Spinner, Tooltip } from '@/design/primitives'
import { formatBytes } from '@/design/status'
import { bridge } from '@/state/bridge'
import { describeSniffed, sniffBinary } from '@shared/binarySniff'
import { extensionOf } from '@shared/fileKind'

type Loaded =
  | { kind: 'model'; url: string; size: number }
  | { kind: 'image'; url: string; size: number; note: string }
  | { kind: 'error'; message: string }

/** Copy of the bytes as a plain ArrayBuffer, which Blob accepts without a type cast on views. */
function toBuffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

/**
 * Shows a 3D model a run produced (.glb, or a self-contained .gltf).
 *
 * The bytes come over IPC rather than from the preview server: the model viewer fetches its
 * source, which would need the loopback server to allow cross-origin reads from any page.
 * What the bytes really are is decided by their content, not their name: a model that is
 * actually a PNG (an image generator saved a picture under a .glb name) is shown as that image
 * with a note, instead of an empty viewport.
 */
export function ModelViewer({ source }: { source: { execId: string; path: string } }) {
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const [spin, setSpin] = useState(true)
  const [fit, setFit] = useState(true)
  const viewerRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!bridge) return
    let cancelled = false
    let objectUrl: string | undefined
    void (async () => {
      const result = await bridge.preview.readRunFile(source.execId, source.path)
      if (cancelled) return
      if (!result.ok || !result.data) {
        setLoaded({ kind: 'error', message: result.error ?? 'Could not read this file.' })
        return
      }
      const { data, size } = result.data
      const sniffed = sniffBinary(data)
      if (sniffed.kind === 'glb' || sniffed.kind === 'gltf') {
        try {
          // The viewer (and three.js inside it) is loaded on first use, not with the app.
          await import('@google/model-viewer')
        } catch {
          if (!cancelled) setLoaded({ kind: 'error', message: 'The 3D viewer could not be loaded.' })
          return
        }
        if (cancelled) return
        objectUrl = URL.createObjectURL(new Blob([toBuffer(data)], { type: sniffed.mime }))
        setLoaded({ kind: 'model', url: objectUrl, size })
      } else if (sniffed.image) {
        objectUrl = URL.createObjectURL(new Blob([toBuffer(data)], { type: sniffed.mime }))
        setLoaded({
          kind: 'image',
          url: objectUrl,
          size,
          note: `This file is named .${extensionOf(source.path)} but contains ${describeSniffed(sniffed)} image data, not a 3D model, so it is shown as an image. The worker saved a picture under a model name.`,
        })
      } else {
        setLoaded({
          kind: 'error',
          message: `This does not look like a glTF model (${describeSniffed(sniffed)}). Open the run folder to inspect it on disk.`,
        })
      }
    })()
    return () => {
      cancelled = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [source.execId, source.path])

  // The element reports problems with an event; a .gltf that points at separate .bin or texture
  // files cannot be resolved from a blob URL.
  useEffect(() => {
    const element = viewerRef.current
    if (!element || loaded?.kind !== 'model') return
    const onError = () =>
      setLoaded({
        kind: 'error',
        message:
          'The model could not be loaded. A .gltf that references separate .bin or texture files cannot be opened here; a .glb carries everything in one file.',
      })
    element.addEventListener('error', onError)
    return () => element.removeEventListener('error', onError)
  }, [loaded])

  const size = loaded && loaded.kind !== 'error' ? loaded.size : undefined
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-inset">
      <div className="flex h-[var(--h-toolbar)] shrink-0 items-center gap-2 border-b border-line-1 bg-bg-1 px-2">
        <span className="mono truncate-1 min-w-0 text-2xs text-fg-3" title={source.path}>
          {source.path}
        </span>
        {size !== undefined ? <span className="num shrink-0 text-2xs text-fg-4">{formatBytes(size)}</span> : null}
        <div className="ml-auto flex shrink-0 items-center gap-1">
          {loaded?.kind === 'model' ? (
            <Tooltip content={spin ? 'Stop rotating' : 'Rotate slowly'}>
              <IconButton label={spin ? 'Stop rotating' : 'Rotate'} size="sm" active={spin} onClick={() => setSpin((value) => !value)}>
                <RotateCw size={13} strokeWidth={1.7} />
              </IconButton>
            </Tooltip>
          ) : null}
          {loaded?.kind === 'image' ? (
            <>
              <Tooltip content={fit ? 'Show at actual size' : 'Fit to window'}>
                <IconButton label={fit ? 'Actual size' : 'Fit to window'} size="sm" active={!fit} onClick={() => setFit((value) => !value)}>
                  {fit ? <Maximize2 size={13} strokeWidth={1.7} /> : <Minimize2 size={13} strokeWidth={1.7} />}
                </IconButton>
              </Tooltip>
              <IconButton label="Open in default browser" size="sm" onClick={() => void bridge?.workspace.openExternal(loaded.url)}>
                <ExternalLink size={13} strokeWidth={1.7} />
              </IconButton>
            </>
          ) : null}
        </div>
      </div>

      {loaded?.kind === 'image' ? (
        <div className="shrink-0 border-b border-line-1 bg-bg-2 px-3 py-1.5 text-2xs text-st-waiting">{loaded.note}</div>
      ) : null}

      <div className="relative min-h-0 flex-1 overflow-auto">
        {loaded === null ? (
          <div className="flex h-full items-center justify-center gap-2 text-xs text-fg-3">
            <Spinner /> Opening…
          </div>
        ) : loaded.kind === 'error' ? (
          <EmptyState title="Could not open model" description={loaded.message} />
        ) : loaded.kind === 'model' ? (
          <model-viewer
            ref={viewerRef}
            src={loaded.url}
            alt={source.path}
            camera-controls=""
            auto-rotate={spin ? '' : undefined}
            environment-image="neutral"
            shadow-intensity="1"
            touch-action="pan-y"
            style={{ width: '100%', height: '100%', background: 'transparent' }}
          />
        ) : (
          <div className={fit ? 'flex h-full w-full items-center justify-center p-4' : 'inline-block min-h-full min-w-full p-4'}>
            <img
              src={loaded.url}
              alt={source.path}
              className={fit ? 'max-h-full max-w-full object-contain shadow-lg' : 'max-w-none shadow-lg'}
              draggable={false}
            />
          </div>
        )}
      </div>
    </div>
  )
}
