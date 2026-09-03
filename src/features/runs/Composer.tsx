import { useEffect, useRef, useState } from 'react'
import { Paperclip, SendHorizontal, X } from 'lucide-react'
import { cn } from '@/design/cn'
import { Button, IconButton, Select, Spinner, Tooltip } from '@/design/primitives'
import { canSubmit, enqueue, uploadAttachments } from '@/state/actions'
import { useStudio } from '@/state/store'
import { useUi } from '@/state/ui'
import type { Attachment } from '@shared/events'

/**
 * Effort tiers, matching the runtime's own vocabulary
 * (runtime/telemetry/ui/index.html:2494). `agent_complexity` is deliberately
 * not offered: the backend parses and stores it but never reads it
 * (cmd/forge/waitlist.go:233), so a control for it would be theatre.
 */
const EFFORT = ['auto', 'minimal', 'low', 'standard', 'elevated', 'high', 'absolute']

export function Composer() {
  const connected = useStudio((s) => s.connection.phase === 'connected')
  const tabs = useUi((s) => s.tabs)

  const [prompt, setPrompt] = useState('')
  const [mode, setMode] = useState<'parallel' | 'sequential'>('parallel')
  const [effort, setEffort] = useState('auto')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [uploading, setUploading] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Grow with content up to a ceiling, so short prompts don't waste space and
  // long ones stay readable without the panel jumping.
  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(180, el.scrollHeight)}px`
  }, [prompt])

  const submit = async () => {
    if (!prompt.trim() || !canSubmit()) return

    // RFC "IDE Context": a pre-formatted string the agents consume directly.
    const openFiles = tabs.filter((t) => t.kind === 'file' && t.path).map((t) => t.path!)
    const ideContext =
      openFiles.length > 0 ? `Open Files: ${openFiles.join(', ')}` : undefined

    const sent = await enqueue({ prompt, mode, effort, ideContext, attachments })
    if (sent) {
      setPrompt('')
      setAttachments([])
      setStatus(null)
    } else {
      setStatus('Not connected — the command was not sent.')
    }
  }

  const attach = async () => {
    setUploading(true)
    const result = await uploadAttachments()
    setUploading(false)
    if (result.ok && result.data) setAttachments((prev) => [...prev, ...result.data!])
    else if (!result.ok) setStatus(result.error ?? 'Upload failed.')
  }

  return (
    <div className="shrink-0 border-t border-line-1 bg-bg-1 p-2">
      {attachments.length > 0 ? (
        <div className="mb-1.5 flex flex-wrap gap-1">
          {attachments.map((file) => (
            <span
              key={file.id}
              className="flex max-w-full items-center gap-1 rounded-[3px] border border-line-2 bg-bg-2 py-0.5 pr-0.5 pl-1.5 text-2xs text-fg-2"
            >
              <span className="truncate-1 max-w-[15ch]">{file.filename}</span>
              <button
                type="button"
                aria-label={`Remove ${file.filename}`}
                onClick={() =>
                  setAttachments((prev) => prev.filter((f) => f.id !== file.id))
                }
                className="flex h-3.5 w-3.5 items-center justify-center rounded-[2px] text-fg-4 hover:bg-bg-3 hover:text-fg-1"
              >
                <X size={9} strokeWidth={2.4} />
              </button>
            </span>
          ))}
        </div>
      ) : null}

      <div
        className={cn(
          'rounded-[var(--radius-card)] border bg-inset',
          '[transition-property:border-color] duration-[var(--dur-fast)]',
          'focus-within:border-accent',
          connected ? 'border-line-2' : 'border-line-1',
        )}
      >
        <textarea
          ref={textareaRef}
          data-composer-input
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              void submit()
            }
          }}
          rows={2}
          disabled={!connected}
          placeholder={
            connected
              ? 'Describe what to build. Enter to run, Shift+Enter for a new line.'
              : 'Connect to a forge instance to submit work.'
          }
          className="w-full resize-none bg-transparent px-2.5 py-2 text-sm text-fg-1 outline-none placeholder:text-fg-4 disabled:opacity-60"
        />

        <div className="flex items-center gap-1 border-t border-line-1 px-1.5 py-1">
          <Select
            aria-label="Execution mode"
            value={mode}
            disabled={!connected}
            onChange={(event) =>
              setMode(event.target.value as 'parallel' | 'sequential')
            }
            className="h-6 w-auto border-none bg-transparent px-1 text-2xs"
          >
            <option value="parallel">parallel</option>
            <option value="sequential">sequential</option>
          </Select>

          <Select
            aria-label="Effort"
            value={effort}
            disabled={!connected}
            onChange={(event) => setEffort(event.target.value)}
            className="h-6 w-auto border-none bg-transparent px-1 text-2xs"
          >
            {EFFORT.map((tier) => (
              <option key={tier} value={tier}>
                {tier}
              </option>
            ))}
          </Select>

          <Tooltip content="Attach files to the prompt">
            <IconButton
              label="Attach files"
              size="sm"
              disabled={!connected || uploading}
              onClick={attach}
            >
              {uploading ? <Spinner size={11} /> : <Paperclip size={13} strokeWidth={1.7} />}
            </IconButton>
          </Tooltip>

          <Button
            size="sm"
            variant="primary"
            className="ml-auto"
            disabled={!connected || !prompt.trim()}
            title={connected ? 'Enqueue this prompt' : 'Not connected to an orchestrator'}
            icon={<SendHorizontal size={12} strokeWidth={2} />}
            onClick={submit}
          >
            Run
          </Button>
        </div>
      </div>

      {status ? <p className="mt-1.5 text-2xs text-st-failed">{status}</p> : null}
    </div>
  )
}
