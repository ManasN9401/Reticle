import type { LogRecord } from './ipc'

/**
 * Merge two batches of log records: one entry per seq, oldest first, keeping the newest
 * `limit`. Used to fold a run's backfilled history into the live stream, where the two
 * can overlap.
 */
export function mergeLogRecords(
  existing: readonly LogRecord[],
  incoming: readonly LogRecord[],
  limit: number,
): LogRecord[] {
  if (incoming.length === 0) return existing as LogRecord[]
  const bySeq = new Map<number, LogRecord>()
  for (const record of existing) bySeq.set(record.seq, record)
  for (const record of incoming) bySeq.set(record.seq, record)
  const merged = [...bySeq.values()].sort((a, b) => a.seq - b.seq)
  return merged.length > limit ? merged.slice(merged.length - limit) : merged
}
