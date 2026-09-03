/**
 * RFC-022 architectural law 6: the detail panel must compact verbose `[TOOL]`
 * JSON emitted by workers into readable semantic action descriptors.
 *
 * Workers print lines like:
 *   [TOOL] {"tool":"write_file","args":{"path":"src/main.py","content":"...5kb..."}}
 * Rendering that raw makes the node log unreadable, so it becomes:
 *   [TOOL] write_file  src/main.py
 */

const TOOL_PREFIX = /^\s*\[TOOL\]\s*/

/** Argument keys worth surfacing, in the order we prefer them. */
const SALIENT_KEYS = ['path', 'file', 'filename', 'query', 'url', 'command', 'name', 'id']

export function compactToolLog(line: string): string {
  if (!TOOL_PREFIX.test(line)) return line

  const body = line.replace(TOOL_PREFIX, '').trim()
  let parsed: unknown
  try {
    parsed = JSON.parse(body)
  } catch {
    // Not JSON after all — leave it alone rather than mangling it.
    return line
  }
  if (!parsed || typeof parsed !== 'object') return line

  const record = parsed as Record<string, unknown>
  const tool =
    pickString(record, ['tool', 'name', 'action', 'function']) ?? 'tool'
  const args = (record.args ?? record.arguments ?? record.input) as
    | Record<string, unknown>
    | undefined

  const detail = args ? summarizeArgs(args) : ''
  return detail ? `[TOOL] ${tool}  ${detail}` : `[TOOL] ${tool}`
}

function pickString(
  record: Record<string, unknown>,
  keys: string[],
): string | undefined {
  for (const key of keys) {
    const value = record[key]
    if (typeof value === 'string' && value) return value
  }
  return undefined
}

function summarizeArgs(args: Record<string, unknown>): string {
  const salient = pickString(args, SALIENT_KEYS)
  if (salient) return truncate(salient, 80)

  // Fall back to the shape rather than the contents: a 5KB `content` field is
  // exactly what this function exists to keep off the screen.
  const parts: string[] = []
  for (const [key, value] of Object.entries(args)) {
    if (typeof value === 'string') {
      parts.push(`${key}=${value.length > 40 ? `<${value.length} chars>` : value}`)
    } else if (value === null || typeof value !== 'object') {
      parts.push(`${key}=${String(value)}`)
    } else if (Array.isArray(value)) {
      parts.push(`${key}=[${value.length}]`)
    } else {
      parts.push(`${key}={…}`)
    }
    if (parts.length === 3) break
  }
  return parts.join(' ')
}

function truncate(value: string, max: number): string {
  return value.length <= max ? value : `${value.slice(0, max - 1)}…`
}
