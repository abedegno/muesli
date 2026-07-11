import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { ApiClient } from '../api/client'
import type { AdminHealthResponse, HealthBadgeStatus } from '../api/types'

interface Props {
  client: Pick<ApiClient, 'getAdminHealth'>
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(1)} ${units[unit]}`
}

function Badge({ status, children }: { status: HealthBadgeStatus; children: ReactNode }) {
  return <span className={`pill pill-${status}`}>{children}</span>
}

export function HealthView({ client }: Props) {
  const [health, setHealth] = useState<AdminHealthResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = useCallback(async () => {
    setRefreshing(true)
    try {
      setHealth(await client.getAdminHealth())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load health')
    } finally {
      setRefreshing(false)
    }
  }, [client])

  useEffect(() => {
    void refresh()
  }, [refresh])

  if (error && !health) {
    return (
      <section>
        <h1>Health</h1>
        <p className="error">{error}</p>
        <button onClick={() => void refresh()}>Refresh</button>
      </section>
    )
  }

  if (!health) {
    return (
      <section>
        <h1>Health</h1>
        <p>Loading…</p>
      </section>
    )
  }

  // Server/Jobs statuses come straight from the server (it's the one that
  // knows e.g. whether any job is terminally failed); Embedding/Storage
  // only ever report ok/error today (no server-side warn condition defined
  // for them yet), and Plugins reduces its per-plugin statuses to a single
  // section badge (error if any plugin errored, otherwise ok).
  const jobsStatus: HealthBadgeStatus = health.jobs.error ? 'error' : health.jobs.status
  const embeddingOk = !health.embedding.error
  const storageOk = !health.storage.error
  const pluginsOk = health.plugins.every((p) => p.status !== 'error')

  return (
    <section>
      <div className="row">
        <h1>Health</h1>
        <button onClick={() => void refresh()} disabled={refreshing}>
          {refreshing ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
      {error && <p className="error">{error}</p>}

      <h2>
        Server <Badge status={health.server.status}>{health.server.status}</Badge>
      </h2>
      <table>
        <tbody>
          <tr>
            <th>Version</th>
            <td>{health.server.version}</td>
          </tr>
          <tr>
            <th>Commit</th>
            <td>
              <code>{health.server.commit}</code>
            </td>
          </tr>
          <tr>
            <th>Go version</th>
            <td>{health.server.goVersion}</td>
          </tr>
        </tbody>
      </table>

      <h2>
        Plugins <Badge status={pluginsOk ? 'ok' : 'error'}>{pluginsOk ? 'OK' : 'Error'}</Badge>
      </h2>
      {health.plugins.length === 0 ? (
        <p>No plugins registered.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Kind</th>
              <th>Status</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {health.plugins.map((p) => (
              <tr key={p.id}>
                <td>{p.name}</td>
                <td>{p.kind}</td>
                <td>
                  <Badge status={p.status}>{p.status}</Badge>
                </td>
                <td>{p.error ?? ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>
        Job queue <Badge status={jobsStatus}>{jobsStatus}</Badge>
      </h2>
      {health.jobs.error ? (
        <p className="error">{health.jobs.error}</p>
      ) : (
        <table>
          <tbody>
            {Object.entries(health.jobs.counts).map(([status, count]) => (
              <tr key={status}>
                <th>{status}</th>
                <td>{count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>
        Embeddings{' '}
        <Badge status={!embeddingOk ? 'error' : health.embedding.enabled ? 'ok' : 'disabled'}>
          {!embeddingOk ? 'Error' : health.embedding.enabled ? 'Enabled' : 'Disabled'}
        </Badge>
      </h2>
      {health.embedding.error && <p className="error">{health.embedding.error}</p>}
      <table>
        <tbody>
          <tr>
            <th>Model</th>
            <td>{health.embedding.model || '(not set)'}</td>
          </tr>
          <tr>
            <th>Dimension</th>
            <td>{health.embedding.dim}</td>
          </tr>
          <tr>
            <th>Coverage</th>
            <td>
              {health.embedding.done} / {health.embedding.total}
            </td>
          </tr>
        </tbody>
      </table>

      <h2>
        Storage <Badge status={storageOk ? 'ok' : 'error'}>{storageOk ? 'OK' : 'Error'}</Badge>
      </h2>
      {health.storage.error ? (
        <p className="error">{health.storage.error}</p>
      ) : (
        <table>
          <tbody>
            <tr>
              <th>Path</th>
              <td>
                <code>{health.storage.path}</code>
              </td>
            </tr>
            <tr>
              <th>Free / Total</th>
              <td>
                {formatBytes(health.storage.freeBytes)} / {formatBytes(health.storage.totalBytes)}
              </td>
            </tr>
          </tbody>
        </table>
      )}
    </section>
  )
}
