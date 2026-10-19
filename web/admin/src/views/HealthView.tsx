import { uiText } from '@/i18n'
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
        <h1>{uiText('Health')}</h1>
        <p className="error">{error}</p>
        <button onClick={() => void refresh()}>{uiText('Refresh')}</button>
      </section>
    )
  }

  if (!health) {
    return (
      <section>
        <h1>{uiText('Health')}</h1>
        <p>{uiText('Loading…')}</p>
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
        <h1>{uiText('Health')}</h1>
        <button onClick={() => void refresh()} disabled={refreshing}>
          {refreshing ? uiText('Refreshing…') : uiText('Refresh')}
        </button>
      </div>
      {error && <p className="error">{error}</p>}

      <h2>
        {uiText('Server')} <Badge status={health.server.status}>{health.server.status}</Badge>
      </h2>
      <table>
        <tbody>
          <tr>
            <th>{uiText('Version')}</th>
            <td>{health.server.version}</td>
          </tr>
          <tr>
            <th>{uiText('Commit')}</th>
            <td>
              <code>{health.server.commit}</code>
            </td>
          </tr>
          <tr>
            <th>{uiText('Go version')}</th>
            <td>{health.server.goVersion}</td>
          </tr>
        </tbody>
      </table>

      <h2>
        {uiText('Plugins')}{' '}
        <Badge status={pluginsOk ? 'ok' : 'error'}>
          {pluginsOk ? uiText('OK') : uiText('Error')}
        </Badge>
      </h2>
      {health.plugins.length === 0 ? (
        <p>{uiText('No plugins registered.')}</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>{uiText('Name')}</th>
              <th>{uiText('Kind')}</th>
              <th>{uiText('Status')}</th>
              <th>{uiText('Error')}</th>
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
        {uiText('Job queue')} <Badge status={jobsStatus}>{jobsStatus}</Badge>
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
        {uiText('Embeddings')}{' '}
        <Badge status={!embeddingOk ? 'error' : health.embedding.enabled ? 'ok' : 'disabled'}>
          {!embeddingOk
            ? uiText('Error')
            : health.embedding.enabled
              ? uiText('Enabled')
              : uiText('Disabled')}
        </Badge>
      </h2>
      {health.embedding.error && <p className="error">{health.embedding.error}</p>}
      <table>
        <tbody>
          <tr>
            <th>{uiText('Model')}</th>
            <td>{health.embedding.model || uiText('(not set)')}</td>
          </tr>
          <tr>
            <th>{uiText('Dimension')}</th>
            <td>{health.embedding.dim}</td>
          </tr>
          <tr>
            <th>{uiText('Coverage')}</th>
            <td>
              {health.embedding.done} {uiText('/')} {health.embedding.total}
            </td>
          </tr>
        </tbody>
      </table>

      <h2>
        {uiText('Storage')}{' '}
        <Badge status={storageOk ? 'ok' : 'error'}>
          {storageOk ? uiText('OK') : uiText('Error')}
        </Badge>
      </h2>
      {health.storage.error ? (
        <p className="error">{health.storage.error}</p>
      ) : (
        <table>
          <tbody>
            <tr>
              <th>{uiText('Path')}</th>
              <td>
                <code>{health.storage.path}</code>
              </td>
            </tr>
            <tr>
              <th>{uiText('Free / Total')}</th>
              <td>
                {formatBytes(health.storage.freeBytes)} {uiText('/')}{' '}
                {formatBytes(health.storage.totalBytes)}
              </td>
            </tr>
          </tbody>
        </table>
      )}
    </section>
  )
}
