import { parseLlmLog } from '../graph/llmLog'

export interface ToolRecord {
  seq: number
  at: number
  execId?: string
  nodeId?: string
  agentId?: string
  message: string
  isLlm: boolean
}

export interface ToolCall {
  /** seq of the request, which orders and identifies the call. */
  seq: number
  at: number
  execId?: string
  nodeId?: string
  agentId?: string
  name: string
  status: 'running' | 'done' | 'failed'
  detail?: string
}

const REQUESTED = /^Requested\s+(\S+)/
const COMPLETED = /^Completed\s+(\S+)/
const FAILED = /^Failed\s+([^\s:]+):?\s*([\s\S]*)$/
const LEGACY = /\[TOOL\]\s*(.*)$/i

/**
 * Tool calls, newest data last. Workers report each call as three structured model-stream
 * records (Requested, then Completed or Failed), which the Tools tab used to ignore: it only
 * listed lines containing "[TOOL]", which only the approval step still prints. Records are
 * paired per node in order, so parallel nodes calling the same tool do not mix up.
 */
export function buildToolCalls(records: readonly ToolRecord[]): ToolCall[] {
  const calls: ToolCall[] = []
  const pending = new Map<string, ToolCall[]>()

  for (const record of records) {
    if (!record.isLlm) {
      const legacy = LEGACY.exec(record.message)
      if (legacy) {
        calls.push({ seq: record.seq, at: record.at, execId: record.execId, nodeId: record.nodeId, agentId: record.agentId, name: legacy[1].trim() || 'tool', status: 'done' })
      }
      continue
    }
    const event = parseLlmLog({ seq: record.seq, message: record.message })
    if (!event || event.kind !== 'tool') continue
    const text = event.text.trim()
    const key = `${record.execId ?? ''}|${record.nodeId ?? ''}`
    const base = { at: record.at, execId: record.execId, nodeId: record.nodeId, agentId: record.agentId }

    const requested = REQUESTED.exec(text)
    if (requested) {
      const call: ToolCall = { seq: record.seq, ...base, name: requested[1], status: 'running' }
      calls.push(call)
      pending.set(key, [...(pending.get(key) ?? []), call])
      continue
    }
    const finished = COMPLETED.exec(text) ?? FAILED.exec(text)
    if (!finished) continue
    const failed = text.startsWith('Failed')
    const name = finished[1]
    const queue = pending.get(key) ?? []
    const index = queue.findIndex((call) => call.name === name)
    const detail = failed ? (finished[2] ?? '').trim().slice(0, 300) || undefined : undefined
    if (index === -1) {
      // The request fell out of the log buffer; keep the outcome anyway.
      calls.push({ seq: record.seq, ...base, name, status: failed ? 'failed' : 'done', detail })
      continue
    }
    const call = queue[index]
    call.status = failed ? 'failed' : 'done'
    call.detail = detail
    queue.splice(index, 1)
  }
  return calls
}
