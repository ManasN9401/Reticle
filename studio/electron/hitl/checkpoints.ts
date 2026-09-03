import fs from 'node:fs/promises'
import path from 'node:path'
import type { ApiResult, HitlCheckpoint, HitlResolveRequest } from '../../src/shared/ipc'

/**
 * Human-in-the-loop approvals (RFC-038).
 *
 * The `hitl-agent` writes a markdown file, prints `[UI_STATE: WAITING_HUMAN]`,
 * and then polls that file every 2s for a `STATUS:` change. Studio drives it by
 * writing the decision back — which is why approve/reject works with no backend
 * change at all.
 *
 * The path is scraped from the agent's own log line rather than reconstructed,
 * because `hitl.py:24` hardcodes `d:\Reticle\runtime\checkpoints` — a directory
 * that does not exist in this repo and that `os.makedirs` therefore creates
 * outside the tree. Trusting the log keeps us correct either way.
 *
 * The parsers below mirror `hitl.py:73-79` exactly:
 *   status   — /(?i)status:\s*(.+)/       first match, to end of line
 *   feedback — /(?i)feedback:\s*(.*)/s    first match, to end of file
 * The DOTALL on feedback is why it must always be written last.
 */

const STATUS_PATTERN = /^([ \t]*)status:[ \t]*(.*)$/im
const FEEDBACK_PATTERN = /^([ \t]*)feedback:[ \t]*[\s\S]*$/im

/** Only ever touch files the hitl-agent could plausibly have written. */
const CHECKPOINT_NAME = /^approval_req_.+\.md$/i

function isCheckpointPath(target: string): boolean {
  return CHECKPOINT_NAME.test(path.basename(target))
}

function parseStatus(content: string): HitlCheckpoint['status'] {
  const match = STATUS_PATTERN.exec(content)
  if (!match) return 'UNKNOWN'
  const value = match[2].trim().toUpperCase()
  if (value === 'PENDING' || value === 'APPROVED' || value === 'REJECTED') return value
  return 'UNKNOWN'
}

function parseFeedback(content: string): string {
  const index = content.search(/(?:^|\n)[ \t]*feedback:/i)
  if (index === -1) return ''
  const after = content.slice(index)
  const colon = after.indexOf(':')
  return colon === -1 ? '' : after.slice(colon + 1).trim()
}

export async function readCheckpoint(target: string): Promise<HitlCheckpoint> {
  const empty: HitlCheckpoint = {
    path: target,
    exists: false,
    content: '',
    status: 'UNKNOWN',
    feedback: '',
  }
  if (!target || !isCheckpointPath(target)) return empty

  try {
    const content = await fs.readFile(target, 'utf8')
    return {
      path: target,
      exists: true,
      content,
      status: parseStatus(content),
      feedback: parseFeedback(content),
    }
  } catch {
    // The agent deletes the file the moment it accepts a decision, so a missing
    // file is the normal terminal state, not an error.
    return empty
  }
}

export async function resolveCheckpoint(
  request: HitlResolveRequest,
): Promise<ApiResult<HitlCheckpoint>> {
  const { path: target, decision, feedback = '' } = request

  if (!target || !isCheckpointPath(target)) {
    return { ok: false, error: 'Refusing to write: not an approval checkpoint file.' }
  }

  let content: string
  try {
    content = await fs.readFile(target, 'utf8')
  } catch {
    return {
      ok: false,
      error: 'Checkpoint file is gone — it was most likely already resolved.',
    }
  }

  let next = content

  if (STATUS_PATTERN.test(next)) {
    next = next.replace(STATUS_PATTERN, (_m, indent: string) => `${indent}STATUS: ${decision}`)
  } else {
    next = `${next.trimEnd()}\n\nSTATUS: ${decision}\n`
  }

  // FEEDBACK must be the final block: the agent's regex is DOTALL and consumes
  // everything after the label to end of file.
  const body = feedback.trim()
  if (FEEDBACK_PATTERN.test(next)) {
    next = next.replace(FEEDBACK_PATTERN, (_m, indent: string) =>
      body ? `${indent}FEEDBACK: ${body}` : `${indent}FEEDBACK: `,
    )
  } else {
    next = `${next.trimEnd()}\nFEEDBACK: ${body}\n`
  }

  if (!next.endsWith('\n')) next += '\n'

  try {
    await fs.writeFile(target, next, 'utf8')
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : String(error) }
  }

  return {
    ok: true,
    data: {
      path: target,
      exists: true,
      content: next,
      status: decision,
      feedback: body,
    },
  }
}
