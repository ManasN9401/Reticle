import type { LogRecord } from '../../shared/ipc'

type DependencyStatus = 'installing' | 'ready' | 'failed'

export interface DependencyOperation {
  key: string
  agent?: string
  dependencies: string[]
  manager: 'uv' | 'pip'
  cached: boolean
  status: DependencyStatus
  startedAt?: number
  updatedAt: number
  durationMs?: number
}

const DEPENDENCY_MESSAGE = /(?:installing (?:base|agent) dependencies|(?:base|agent) dependencies ready|(?:base|agent) dependency installation failed)/i

export function dependencyOperationCount(logs: readonly LogRecord[]): number {
  return buildDependencyOperations(logs).length
}

export function buildDependencyOperations(logs: readonly LogRecord[]): DependencyOperation[] {
  const operations = new Map<string, DependencyOperation>()
  for (const record of logs) {
    if (!DEPENDENCY_MESSAGE.test(record.message)) continue
    const lower = record.message.toLowerCase()
    const agent = field(record.message, 'agent_id')
    const key = agent ? `agent:${agent}` : 'base'
    const existing = operations.get(key)
    const dependencies = listField(record.message, 'deps')
    const duration = numberField(record.message, 'duration_ms')
    const cached = field(record.message, 'cached') === 'true'
    const status: DependencyStatus = lower.includes('failed')
      ? 'failed'
      : lower.includes('ready')
        ? 'ready'
        : 'installing'
    operations.set(key, {
      key,
      agent,
      dependencies: dependencies.length > 0 ? dependencies : existing?.dependencies ?? [],
      manager: field(record.message, 'using_uv') === 'true' ? 'uv' : existing?.manager ?? 'pip',
      cached: status === 'ready' ? cached : false,
      status,
      startedAt: status === 'installing' ? record.at : existing?.startedAt,
      updatedAt: record.at,
      durationMs: duration ?? existing?.durationMs,
    })
  }
  return [...operations.values()].sort((a, b) => b.updatedAt - a.updatedAt)
}

function field(message: string, name: string): string | undefined {
  const match = new RegExp(`${name}:\\s*(\\[[^\\]]*\\]|[^,)]+)`, 'i').exec(message)
  return match?.[1]?.trim().replace(/^"|"$/g, '')
}

function listField(message: string, name: string): string[] {
  const value = field(message, name)
  if (!value?.startsWith('[') || !value.endsWith(']')) return []
  return value.slice(1, -1).trim().split(/\s+/).filter(Boolean)
}

function numberField(message: string, name: string): number | undefined {
  const value = Number(field(message, name))
  return Number.isFinite(value) ? value : undefined
}
