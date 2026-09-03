import { useEffect, useState } from 'react'
import { Check, FileWarning, X } from 'lucide-react'
import { Button, Spinner } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { resolveApproval } from '@/state/actions'
import { useActiveRun } from '@/state/store'
import { useUi } from '@/state/ui'
import type { HitlCheckpoint } from '@shared/ipc'

/**
 * Human-in-the-loop approval (RFC-038).
 *
 * The hitl-agent writes a markdown checkpoint, prints `[UI_STATE: WAITING_HUMAN]`
 * and polls that file every 2s for a `STATUS:` change. Approving here writes
 * `STATUS: APPROVED` back to the file the agent named in its own log, so the
 * DAG resumes within one poll interval — no backend change required.
 */
export function ReviewSheet({ nodeId }: { nodeId: string }) {
  const run = useActiveRun()
  const setReviewNode = useUi((s) => s.setReviewNode)
  const node = run?.nodes[nodeId]
  const path = node?.waiting?.checkpointPath

  const [checkpoint, setCheckpoint] = useState<HitlCheckpoint | null>(null)
  const [feedback, setFeedback] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!path || !bridge) return
    let cancelled = false
    void bridge.hitl.read(path).then((result) => {
      if (!cancelled) setCheckpoint(result)
    })
    return () => {
      cancelled = true
    }
  }, [path])

  const close = () => setReviewNode(null)

  const decide = async (decision: 'APPROVED' | 'REJECTED') => {
    if (!path) return
    setBusy(true)
    setError(null)
    const result = await resolveApproval({ path, decision, feedback })
    setBusy(false)
    if (!result.ok) setError(result.error ?? 'Failed to write the checkpoint file.')
  }

  return (
    <div
      className="absolute inset-0 z-40 flex items-center justify-center bg-black/45 p-6"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) close()
      }}
    >
      <div
        role="dialog"
        aria-label="Approval checkpoint"
        className="flex max-h-full w-[min(760px,100%)] flex-col overflow-hidden rounded-[var(--radius-card)] border border-line-2 bg-bg-2 shadow-[var(--shadow-modal)]"
      >
        <header className="flex shrink-0 items-center gap-2 border-b border-line-1 bg-st-waiting-weak px-4 py-2.5">
          <FileWarning size={14} strokeWidth={1.8} className="text-st-waiting" />
          <span className="text-sm font-medium text-fg-1">Human approval required</span>
          <span className="mono text-2xs text-fg-3">{node?.label ?? nodeId}</span>
          <button
            type="button"
            onClick={close}
            aria-label="Close"
            className="ml-auto flex h-6 w-6 items-center justify-center rounded-[3px] text-fg-3 hover:bg-bg-3 hover:text-fg-1"
          >
            <X size={14} strokeWidth={1.8} />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-auto bg-inset px-4 py-3">
          {!path ? (
            <p className="pretty text-xs text-fg-3">
              This node is blocked on a human decision, but Studio has not seen the
              checkpoint path yet. The path is read from the agent's own log line
              (&ldquo;Checkpoint file created at: …&rdquo;) — it should appear within a
              couple of seconds.
            </p>
          ) : checkpoint === null ? (
            <div className="flex items-center gap-2 text-xs text-fg-3">
              <Spinner /> Reading checkpoint…
            </div>
          ) : !checkpoint.exists ? (
            <p className="pretty text-xs text-fg-3">
              The checkpoint file is gone, which means the agent has already accepted a
              decision and deleted it. This node should resume shortly.
            </p>
          ) : (
            <pre className="mono text-code leading-relaxed whitespace-pre-wrap text-fg-2">
              {checkpoint.content}
            </pre>
          )}
        </div>

        <div className="shrink-0 border-t border-line-1 px-4 py-3">
          <label
            className="mb-1 block text-2xs font-medium text-fg-3"
            htmlFor="hitl-feedback"
          >
            Feedback {feedback ? '' : '(required when rejecting)'}
          </label>
          <textarea
            id="hitl-feedback"
            value={feedback}
            onChange={(event) => setFeedback(event.target.value)}
            rows={3}
            placeholder="Explain what should change. On rejection this is returned to the architect for re-planning."
            className="w-full resize-none rounded-[var(--radius-control)] border border-line-2 bg-inset px-2 py-1.5 text-xs text-fg-1 placeholder:text-fg-4 focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-1"
          />

          {error ? <p className="mt-2 text-2xs text-st-failed">{error}</p> : null}

          <div className="mt-3 flex items-center gap-2">
            <span className="mono truncate-1 min-w-0 flex-1 text-[10px] text-fg-4" title={path}>
              {path ?? 'checkpoint path unknown'}
            </span>
            <Button
              size="sm"
              variant="danger"
              disabled={busy || !path || !feedback.trim()}
              title={
                !feedback.trim()
                  ? 'Rejection feedback is returned to the architect, so it cannot be empty'
                  : 'Halt this branch and return feedback'
              }
              icon={<X size={12} strokeWidth={2} />}
              onClick={() => decide('REJECTED')}
            >
              Reject
            </Button>
            <Button
              size="sm"
              variant="primary"
              disabled={busy || !path}
              icon={busy ? <Spinner size={10} /> : <Check size={12} strokeWidth={2.2} />}
              onClick={() => decide('APPROVED')}
            >
              Approve
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
