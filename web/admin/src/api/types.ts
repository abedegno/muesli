// Mirrors the server's HTTP/JSON shapes. Plan 1a owns setup/login;
// Plan 1b owns plugins/jobs. Keep these in sync with those plans.

export interface SetupStatus {
  needs_setup: boolean
}

export interface LoginResponse {
  token: string
}

type PluginKind = 'transcriber' | 'agent'

export interface Plugin {
  id: string
  kind: PluginKind
  name: string
  endpoint_url: string
  enabled: boolean
  is_default: boolean
  // JSON Schema describing the plugin's config fields (optional).
  config_schema?: JsonSchema
  // Current config values (secret fields redacted to "*" by the server).
  config?: Record<string, unknown>
}

export interface PluginHealth {
  healthy: boolean
  error?: string
}

export interface PluginInput {
  kind: PluginKind
  name: string
  endpoint_url: string
  // Write-only per-plugin shared auth secret (server→plugin auth), distinct
  // from `config`. Required on create; never returned by the server.
  token: string
  config: Record<string, unknown>
  is_default: boolean
}

export type PluginPatch = Partial<{
  name: string
  endpoint_url: string
  // Write-only server→plugin auth secret. Omit (or empty) to leave the stored
  // token unchanged; provide a non-empty value to rotate it.
  token: string
  config: Record<string, unknown>
  enabled: boolean
  is_default: boolean
}>

export type JobStatus = 'pending' | 'running' | 'done' | 'failed' | 'cancelled'

export interface Job {
  id: string
  note_id: string
  type: 'transcribe' | 'summarize' | 'embed'
  status: JobStatus
  attempts: number
  last_error: string | null
  priority: number
  // ADM04: the current attempt's start/end, used to render the per-note
  // pipeline timeline. Reset (started_at set, finished_at cleared) on every
  // reclaim, so these always describe only the most recent attempt.
  started_at: string | null
  finished_at: string | null
}

// EXT01e: webhook delivery status + manual retry.
export type WebhookDeliveryStatus = 'pending' | 'in_flight' | 'delivered' | 'failed'

export interface WebhookDelivery {
  id: string
  webhook_id: string
  status: WebhookDeliveryStatus
  attempts: number
  max_attempts: number
  next_attempt_at?: string | null
  last_error?: string
  created_at: string
  updated_at: string
}

export interface RetryWebhookDeliveryResponse {
  status: 'queued' | 'already_delivered'
}

// Minimal JSON Schema subset used to render config forms.
export interface JsonSchema {
  type?: string
  properties?: Record<string, JsonSchemaProperty>
  required?: string[]
}

export interface JsonSchemaProperty {
  type?: 'string' | 'number' | 'integer' | 'boolean' | string
  title?: string
  description?: string
  enum?: string[]
  default?: unknown
  // Custom hint: secret fields are write-only (never displayed when stored).
  writeOnly?: boolean
  format?: string
}

// BAK01: in-app Postgres backup.
export interface BackupInfo {
  filename: string
  size_bytes: number
  created_at: string
}

// EMB01: embeddings configuration and state.
export interface EmbeddingsStatus {
  enabled: boolean
  model: string
  dim: number
  minScore: number
  docPrefix: string
  queryPrefix: string
}

// EMB02: live on-demand re-embed progress + trigger response. Distinct from
// EmbeddingsStatus (EMB01) above, which reports static config only; this
// reports live done/total counts and backs the admin re-embed panel.
export interface EmbeddingStatus {
  enabled: boolean
  model: string
  dim: number
  done: number
  total: number
}

export interface ReembedResponse {
  status: 'queued'
  enqueued: number
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

// ADM08: backup integrity verification result.
export interface BackupVerifyResult {
  ok: boolean
  error?: string
  size_bytes: number
  table_count: number
}

// ADM05: aggregated admin health panel.
export type HealthBadgeStatus = 'ok' | 'warn' | 'error' | 'disabled'

export interface ServerInfoHealth {
  version: string
  commit: string
  goVersion: string
  // "ok" when full build info was available, "warn" when one or more
  // fields fell back to "dev"/"unknown" - never "error".
  status: 'ok' | 'warn'
}

export type PluginHealthStatus = 'ok' | 'error' | 'disabled'

export interface PluginHealthEntry {
  id: string
  kind: string
  name: string
  status: PluginHealthStatus
  error?: string
}

export interface JobQueueHealth {
  counts: Record<string, number>
  // "error" when the store lookup itself failed, "warn" when it succeeded
  // but at least one job is terminally failed, "ok" otherwise.
  status: 'ok' | 'warn' | 'error'
  error?: string
}

// Extends EMB01/EMB02's shapes with the health panel's own error field.
export interface EmbeddingHealth {
  enabled: boolean
  model: string
  dim: number
  minScore: number
  docPrefix: string
  queryPrefix: string
  done: number
  total: number
  error?: string
}

export interface StorageDiskUsage {
  path: string
  totalBytes: number
  freeBytes: number
  error?: string
}

export interface AdminHealthResponse {
  server: ServerInfoHealth
  plugins: PluginHealthEntry[]
  jobs: JobQueueHealth
  embedding: EmbeddingHealth
  storage: StorageDiskUsage
}

// ADM06: read-only, redacted effective-configuration view (MUESLI_* only).
export interface ConfigEntry {
  name: string
  envVar: string
  value: string
  source: 'env' | 'default'
}
