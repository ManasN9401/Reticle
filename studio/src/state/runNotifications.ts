import { useEffect } from 'react'
import { runTotals } from '@shared/projection'
import { chooseProduct, detectApprovalRequests, detectCompletions, formatRunDuration } from '@shared/runCompletion'
import type { RunCompletion } from '@shared/runCompletion'
import type { RunNode } from '@shared/projection'
import { openApproval, openRunFiles, openRunPreview } from './actions'
import { bridge } from './bridge'
import { useStudio } from './store'
import { useToasts } from './toasts'
import type { Toast } from './toasts'
import { useUi } from './ui'

function shortId(execId: string): string {
  return execId.length > 22 ? `${execId.slice(0, 22)}…` : execId
}

async function announce({ execId, outcome, run }: RunCompletion): Promise<void> {
  const totals = runTotals(run)
  const duration = run.startedAt && run.endedAt ? formatRunDuration(run.endedAt - run.startedAt) : null
  const when = duration ? ` in ${duration}` : ''
  const toasts = useToasts.getState()
  const id = `run:${execId}`
  // The main process drops this while Studio is focused or the preference is off.
  void bridge?.notify.runFinished({
    title: outcome === 'failed' ? `Run failed${when}` : `Run finished${when}`,
    body: outcome === 'failed' ? (run.failureReason ?? shortId(execId)) : `${totals.done}/${totals.total} nodes done. Open Studio to view the result.`,
  })

  if (outcome === 'failed') {
    toasts.push({
      id,
      tone: 'failure',
      title: `Run failed${when}`,
      detail: run.failureReason ? `${shortId(execId)}: ${run.failureReason}` : shortId(execId),
      actions: [
        {
          label: 'View run',
          run: () => {
            useStudio.getState().selectRun(execId)
            useUi.getState().setView('runs')
          },
        },
      ],
    })
    return
  }

  const base: Toast = {
    id,
    tone: 'success',
    title: `Run finished: ${totals.done}/${totals.total} nodes${when}`,
    detail: shortId(execId),
    actions: [],
  }
  // Tell the user straight away, then upgrade the toast once we know what was produced.
  toasts.push(base)

  const result = await bridge?.api.outputs(execId)
  const files = result?.ok ? (result.data ?? []) : []
  if (files.length === 0) {
    toasts.push({ ...base, detail: `${shortId(execId)} · no files were written under src` })
    return
  }
  const product = chooseProduct(files.map((file) => file.path))
  const actions: Toast['actions'] = []
  if (product.previewEntry) {
    const entry = product.previewEntry
    actions.push({
      label: 'Open preview',
      run: () =>
        void openRunPreview(execId, entry).then((opened) => {
          if (opened) return
          useToasts.getState().push({
            id: `${id}:preview`,
            tone: 'failure',
            title: 'Could not open the preview',
            detail: 'The run has no servable files, or the local preview server could not start.',
            actions: [],
          })
        }),
    })
  }
  if (product.codeFiles.length > 0) {
    actions.push({
      label: `View files (${product.codeFiles.length})`,
      run: () => openRunFiles(execId, files, product.codeFiles),
    })
  }
  toasts.push({
    ...base,
    detail: product.previewEntry
      ? `Site ready: ${product.previewEntry} · ${files.length} files`
      : `${files.length} files created`,
    actions,
  })
}

const APPROVAL_TOAST = 'approval:'

/** A node is waiting for a person: say so everywhere it can be missed, once. */
function announceApproval(node: RunNode): void {
  const target = { execId: node.execId, nodeId: node.nodeId }
  const title = 'Approval needed'
  const body = `${node.label} is waiting for your decision. The workflow is paused until you approve or reject it.`
  useToasts.getState().push({
    id: `${APPROVAL_TOAST}${node.execId}|${node.nodeId}`,
    tone: 'attention',
    title,
    detail: `${node.label} · ${shortId(node.execId)}`,
    actions: [{ label: 'Review', run: () => openApproval(target.execId, target.nodeId) }],
  })
  // Dropped by the main process while Studio is focused or the preference is off.
  void bridge?.notify.approval({ title, body, target })
}

/** Remove approval toasts for nodes that have since been decided, killed or replaced. */
function clearDecidedApprovals(): void {
  const { projection } = useStudio.getState()
  const toasts = useToasts.getState()
  for (const toast of toasts.toasts) {
    if (!toast.id.startsWith(APPROVAL_TOAST)) continue
    const [execId, nodeId] = toast.id.slice(APPROVAL_TOAST.length).split('|')
    const node = projection.runs[execId]?.nodes[nodeId]
    if (!node || node.status !== 'waiting' || node.waiting?.kind !== 'human') toasts.dismiss(toast.id)
  }
}

/**
 * Announces runs as they finish and nodes as they start waiting for a decision. Only live
 * transitions count, never a reconnect snapshot.
 */
export function useRunNotifications(): void {
  useEffect(() => {
    const unsubscribe = useStudio.subscribe((state, previous) => {
      if (state.projection === previous.projection) return
      for (const completion of detectCompletions(previous.projection, state.projection)) {
        void announce(completion)
      }
      for (const node of detectApprovalRequests(previous.projection, state.projection)) {
        announceApproval(node)
      }
      clearDecidedApprovals()
    })
    // Clicking the system notification brings Studio forward; open the review it was about.
    const offClick = bridge?.notify.onClick((target) => openApproval(target.execId, target.nodeId))
    return () => {
      unsubscribe()
      offClick?.()
    }
  }, [])
}
