import { SessionStore } from '../auth/session'
import {
  AdminHealthResponse,
  ApiError,
  BackupInfo,
  BackupVerifyResult,
  ConfigEntry,
  EmbeddingsStatus,
  EmbeddingStatus,
  Job,
  LoginResponse,
  Plugin,
  PluginHealth,
  PluginInput,
  PluginPatch,
  ReembedResponse,
  RetryWebhookDeliveryResponse,
  SetupStatus,
  WebhookDelivery,
} from './types'

/**
 * ApiClient is the single place that talks to the Muesli server. It injects the
 * bearer token, parses JSON, and turns non-2xx responses into ApiError. A 401
 * clears the session so the app falls back to the login view.
 */
export class ApiClient {
  // Required params come first (TS1016: a required param cannot follow an
  // optional one). fetch is bound to globalThis so calling it as `this.fetchFn`
  // doesn't trip the browser's "Illegal invocation" on an unbound window.fetch.
  constructor(
    private session: SessionStore,
    private baseUrl = '',
    private fetchFn: typeof fetch = globalThis.fetch.bind(globalThis)
  ) {}

  // ----- Plan 1a: setup + login -----

  async getSetupStatus(): Promise<SetupStatus> {
    return this.request<SetupStatus>('GET', '/api/setup/status')
  }

  async setup(email: string, password: string): Promise<void> {
    await this.request<unknown>('POST', '/api/setup', { email, password })
  }

  async login(email: string, password: string): Promise<LoginResponse> {
    const out = await this.request<LoginResponse>('POST', '/api/login', { email, password })
    this.session.setToken(out.token)
    return out
  }

  // ----- Plan 1b: plugins -----

  async listPlugins(): Promise<Plugin[]> {
    return this.request<Plugin[]>('GET', '/api/admin/plugins')
  }

  async createPlugin(input: PluginInput): Promise<Plugin> {
    return this.request<Plugin>('POST', '/api/admin/plugins', input)
  }

  async updatePlugin(id: string, patch: PluginPatch): Promise<Plugin> {
    // An empty token means "leave the stored secret unchanged" — drop it so we
    // never overwrite the server-side token with "". A non-empty token rotates it.
    const body: PluginPatch = { ...patch }
    if (body.token === '') delete body.token
    return this.request<Plugin>('PATCH', `/api/admin/plugins/${id}`, body)
  }

  async deletePlugin(id: string): Promise<void> {
    await this.request<unknown>('DELETE', `/api/admin/plugins/${id}`)
  }

  async checkPluginHealth(id: string): Promise<PluginHealth> {
    return this.request<PluginHealth>('POST', `/api/admin/plugins/${id}/health`)
  }

  // ----- Plan 1b: jobs -----

  async listJobs(status: string = ''): Promise<Job[]> {
    const path = status ? `/api/admin/jobs?status=${encodeURIComponent(status)}` : '/api/admin/jobs'
    return this.request<Job[]>('GET', path)
  }

  // ADM04: per-note pipeline timeline (all jobs for one note, in pipeline order).
  async listNoteJobs(noteId: string): Promise<Job[]> {
    return this.request<Job[]>('GET', `/api/admin/notes/${noteId}/jobs`)
  }

  // ----- Plan G05: job retry -----

  async retryJob(jobId: string): Promise<void> {
    await this.request<unknown>('POST', `/api/admin/jobs/${jobId}/retry`)
  }

  async cancelJob(jobId: string): Promise<void> {
    await this.request<unknown>('POST', `/api/admin/jobs/${jobId}/cancel`)
  }

  async processNextJob(jobId: string): Promise<void> {
    await this.request<unknown>('POST', `/api/admin/jobs/${jobId}/process-next`)
  }

  async resummarizeNote(noteId: string): Promise<void> {
    await this.request<unknown>('POST', `/api/notes/${noteId}/resummarize`)
  }

  // ----- EXT01e: webhook delivery status + manual retry -----

  async listWebhookDeliveries(limit?: number): Promise<WebhookDelivery[]> {
    const path = limit
      ? `/api/admin/webhook-deliveries?limit=${encodeURIComponent(String(limit))}`
      : '/api/admin/webhook-deliveries'
    return this.request<WebhookDelivery[]>('GET', path)
  }

  async retryWebhookDelivery(deliveryId: string): Promise<RetryWebhookDeliveryResponse> {
    return this.request<RetryWebhookDeliveryResponse>(
      'POST',
      `/api/admin/webhook-deliveries/${deliveryId}/retry`
    )
  }

  // ----- BAK01: in-app Postgres backup -----

  async listBackups(): Promise<BackupInfo[]> {
    return this.request<BackupInfo[]>('GET', '/api/admin/backups')
  }

  async createBackup(): Promise<BackupInfo> {
    return this.request<BackupInfo>('POST', '/api/admin/backup')
  }

  // ----- EMB01: embeddings status -----

  async getEmbeddingsStatus(): Promise<EmbeddingsStatus> {
    return this.request<EmbeddingsStatus>('GET', '/api/admin/embeddings')
  }

  // ----- EMB02: on-demand admin re-embed-all -----

  async getEmbeddingStatus(): Promise<EmbeddingStatus> {
    return this.request<EmbeddingStatus>('GET', '/api/admin/embeddings/status')
  }

  async reembedAll(): Promise<ReembedResponse> {
    return this.request<ReembedResponse>('POST', '/api/admin/embeddings/reembed')
  }

  // ----- ADM05: aggregated admin health panel -----

  async getAdminHealth(): Promise<AdminHealthResponse> {
    return this.request<AdminHealthResponse>('GET', '/api/admin/health')
  }

  // ----- ADM06: effective-configuration view (read-only, redacted) -----

  async getAdminConfig(): Promise<ConfigEntry[]> {
    return this.request<ConfigEntry[]>('GET', '/api/admin/config')
  }

  // downloadBackup fetches the dump with the bearer token (auth is
  // bearer-token, not cookie-based, so a plain <a href> link can't carry
  // it) and saves the resulting blob via a temporary object-URL anchor click.
  async downloadBackup(filename: string): Promise<void> {
    const headers: Record<string, string> = { ...this.session.authHeader() }
    const resp = await this.fetchFn(
      `${this.baseUrl}/api/admin/backups/${encodeURIComponent(filename)}`,
      { method: 'GET', headers }
    )
    if (resp.status === 401) {
      this.session.clear()
    }
    if (!resp.ok) {
      throw new ApiError(resp.status, await this.errorMessage(resp))
    }
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    try {
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      a.remove()
    } finally {
      URL.revokeObjectURL(url)
    }
  }

  // ----- ADM08: backup integrity verification -----

  async verifyBackup(filename: string): Promise<BackupVerifyResult> {
    return this.request<BackupVerifyResult>(
      'GET',
      `/api/admin/backups/${encodeURIComponent(filename)}/verify`
    )
  }

  // ----- internal -----

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { ...this.session.authHeader() }
    const init: RequestInit = { method, headers }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(body)
    }
    const resp = await this.fetchFn(this.baseUrl + path, init)

    if (resp.status === 401) {
      this.session.clear()
    }
    if (!resp.ok) {
      throw new ApiError(resp.status, await this.errorMessage(resp))
    }
    if (resp.status === 204) {
      return undefined as T
    }
    const text = await resp.text()
    return (text ? JSON.parse(text) : undefined) as T
  }

  private async errorMessage(resp: Response): Promise<string> {
    try {
      const data = (await resp.json()) as { error?: string }
      return data.error ?? `request failed (${resp.status})`
    } catch {
      return `request failed (${resp.status})`
    }
  }
}
