export type LlmDiagnosticKind = 'reasoning' | 'content' | 'tool' | 'status'

export interface LlmDiagnostic {
  kind: LlmDiagnosticKind
  text: string
  name?: string
  firstSeq: number
  lastSeq: number
}

interface LlmLogRecord {
  seq: number
  message: string
}

const STREAM_MARKER = '[LLM_STREAM] '

function diagnostic(seq: number, kind: LlmDiagnosticKind, text: unknown, name?: unknown): LlmDiagnostic | null {
  if (typeof text !== 'string' || text.length === 0) return null
  return {
    kind,
    text,
    name: typeof name === 'string' && name.length > 0 ? name : undefined,
    firstSeq: seq,
    lastSeq: seq,
  }
}

/** Parse both current structured diagnostics and historical string-only logs. */
export function parseLlmLog(record: LlmLogRecord): LlmDiagnostic | null {
  const markerIndex = record.message.indexOf(STREAM_MARKER)
  if (markerIndex !== -1) {
    const payload = record.message.slice(markerIndex + STREAM_MARKER.length)
    try {
      const parsed: unknown = JSON.parse(payload)
      if (typeof parsed === 'string') return diagnostic(record.seq, 'content', parsed)
      if (typeof parsed !== 'object' || parsed === null) return null
      const event = parsed as Record<string, unknown>
      const kind = event.kind
      if (kind !== 'reasoning' && kind !== 'content' && kind !== 'tool' && kind !== 'status') {
        return null
      }
      return diagnostic(record.seq, kind, event.text, event.name)
    } catch {
      return null
    }
  }

  const legacyIndex = record.message.indexOf('[LLM]')
  if (legacyIndex === -1) return null
  const text = record.message.slice(legacyIndex + '[LLM]'.length).trimStart()
  const kind: LlmDiagnosticKind = /^(error|hard limit)/i.test(text) ? 'status' : 'content'
  return diagnostic(record.seq, kind, `${text}\n`)
}

/** Join token-sized events while preserving changes between reasoning, response and tools. */
export function buildLlmDiagnostics(records: LlmLogRecord[]): LlmDiagnostic[] {
  const result: LlmDiagnostic[] = []
  for (const record of records) {
    const next = parseLlmLog(record)
    if (!next) continue
    const previous = result.at(-1)
    if (previous && previous.kind === next.kind && previous.name === next.name) {
      previous.text += next.text
      previous.lastSeq = next.lastSeq
    } else {
      result.push(next)
    }
  }
  return result
}
