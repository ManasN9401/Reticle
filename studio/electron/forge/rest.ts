import { controlHeaders, requireLocalHost } from './auth'
import fs from 'node:fs/promises'
import path from 'node:path'
import type { RoutingModel } from '../../src/shared/events'
import type {
  ApiResult,
  McpAction,
  McpServerConfig,
  McpServerStatus,
  OutputFile,
  PluginAction,
  PluginView,
} from '../../src/shared/ipc'
import type { Attachment } from '../../src/shared/events'

/**
 * Thin client for the telemetry server's REST surface
 * (runtime/telemetry/server.go). Lives in main so the renderer never needs
 * network access and so the base URL has exactly one definition.
 */

const TIMEOUT_MS = 15_000

function ok<T>(data: T): ApiResult<T> {
  return { ok: true, data }
}

function fail<T>(error: string): ApiResult<T> {
  return { ok: false, error }
}

async function request<T>(
  baseUrl: string,
  pathname: string,
  init?: RequestInit,
  parse: 'json' | 'void' = 'json',
  timeoutMs = TIMEOUT_MS,
): Promise<ApiResult<T>> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(`${baseUrl}${pathname}`, {
      ...init,
      headers: { ...Object.fromEntries(new Headers(init?.headers)), ...controlHeaders() },
      signal: controller.signal,
    })
    if (!response.ok) {
      const body = await response.text().catch(() => '')
      return fail(`${response.status} ${response.statusText}${body ? `: ${body}` : ''}`)
    }
    if (parse === 'void') return ok(undefined as T)
    return ok((await response.json()) as T)
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') {
      return fail(`request to ${pathname} timed out`)
    }
    return fail(error instanceof Error ? error.message : String(error))
  } finally {
    clearTimeout(timer)
  }
}

export class ForgeRest {
  private baseUrl = 'http://127.0.0.1:8080'

  setTarget(host: string, port: number): void {
    requireLocalHost(host)
    this.baseUrl = `http://${host.includes(':') ? `[${host}]` : host}:${port}`
  }

  models(): Promise<ApiResult<RoutingModel[]>> {
    return request<RoutingModel[]>(this.baseUrl, '/api/models')
  }

  /** `modelKey` is `Model.Key()`: `"<id>|<API_KEY_ENV>"` when the env is set. */
  toggleModel(modelKey: string): Promise<ApiResult<void>> {
    return request<void>(
      this.baseUrl,
      '/api/models/toggle',
      {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ model_key: modelKey }),
      },
      'void',
    )
  }

  /** Returns every file under `.reticle/sessions/{execId}/src`, contents inlined. */
  outputs(execId: string): Promise<ApiResult<OutputFile[]>> {
    if (!execId) return Promise.resolve(fail('missing execution id'))
    return request<OutputFile[]>(
      this.baseUrl,
      `/api/outputs/${encodeURIComponent(execId)}`,
    )
  }

  /**
   * Multipart upload into `.reticle/waitlist_staging`. The returned descriptors
   * are what an `enqueue` command's `attachments` array expects.
   */
  async upload(paths: string[]): Promise<ApiResult<Attachment[]>> {
    if (paths.length === 0) return ok([])
    const form = new FormData()
    try {
      let total = 0
      for (const filePath of paths) {
        const info = await fs.stat(filePath)
        total += info.size
        if (!info.isFile() || total > 49 * 1024 * 1024) return fail('Upload must contain regular files totalling at most 49 MiB')
      }
      for (const filePath of paths) {
        const bytes = await fs.readFile(filePath)
        form.append('files', new Blob([bytes], {type: ({'.png':'image/png','.jpg':'image/jpeg','.jpeg':'image/jpeg','.webp':'image/webp'} as Record<string,string>)[path.extname(filePath).toLowerCase()] ?? 'application/octet-stream'}), path.basename(filePath))
      }
    } catch (error) {
      return fail(error instanceof Error ? error.message : String(error))
    }
    return request<Attachment[]>(this.baseUrl, '/api/upload', {
      method: 'POST',
      body: form,
    })
  }

  mcpServers(): Promise<ApiResult<McpServerStatus[]>> {
    return request<McpServerStatus[]>(this.baseUrl, '/api/mcp/servers')
  }

  saveMcpServer(config: McpServerConfig): Promise<ApiResult<McpServerStatus>> {
    return request<McpServerStatus>(this.baseUrl, '/api/mcp/servers', {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(config),
    }, 'json', 125_000)
  }

  deleteMcpServer(id: string): Promise<ApiResult<void>> {
    return request<void>(this.baseUrl, `/api/mcp/servers/${encodeURIComponent(id)}`, { method: 'DELETE' }, 'void')
  }

  actOnMcpServer(id: string, action: McpAction): Promise<ApiResult<McpServerStatus>> {
    return request<McpServerStatus>(this.baseUrl, `/api/mcp/servers/${encodeURIComponent(id)}/${action}`, { method: 'POST' }, 'json', 125_000)
  }

  plugins(): Promise<ApiResult<PluginView[]>> {
    return request<PluginView[]>(this.baseUrl, '/api/plugins')
  }

  installPlugin(sourcePath: string): Promise<ApiResult<PluginView>> {
    return request<PluginView>(this.baseUrl, '/api/plugins', {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ path: sourcePath }),
    })
  }

  deletePlugin(id: string): Promise<ApiResult<void>> {
    return request<void>(this.baseUrl, `/api/plugins/${encodeURIComponent(id)}`, { method: 'DELETE' }, 'void')
  }

  actOnPlugin(id: string, action: PluginAction): Promise<ApiResult<PluginView>> {
    return request<PluginView>(this.baseUrl, `/api/plugins/${encodeURIComponent(id)}/${action}`, { method: 'POST' })
  }
}
