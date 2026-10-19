import { uiText } from '@/i18n'
import { useCallback, useEffect, useState } from 'react'
import type { ApiClient } from '../api/client'
import type { ConfigEntry } from '../api/types'

interface Props {
  client: Pick<ApiClient, 'getAdminConfig'>
}

// ADM06: read-only, redacted effective-configuration view. Secret-shaped
// values arrive from the server already collapsed to "(set)"/"(unset)" —
// this view never has access to a real secret value, so Copy is always safe.
export function ConfigView({ client }: Props) {
  const [entries, setEntries] = useState<ConfigEntry[]>([])
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [copiedEnvVar, setCopiedEnvVar] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setRefreshing(true)
    try {
      setEntries(await client.getAdminConfig())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load config')
    } finally {
      setRefreshing(false)
    }
  }, [client])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const handleCopy = useCallback((entry: ConfigEntry) => {
    const clipboard = typeof navigator !== 'undefined' ? navigator.clipboard : undefined
    if (!clipboard) return
    void clipboard.writeText(entry.value).then(() => {
      setCopiedEnvVar(entry.envVar)
    })
  }, [])

  return (
    <section>
      <div className="row">
        <h1>{uiText('Config')}</h1>
        <button onClick={() => void refresh()} disabled={refreshing}>
          {refreshing ? uiText('Refreshing…') : uiText('Refresh')}
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>{uiText('Name')}</th>
            <th>{uiText('Env var')}</th>
            <th>{uiText('Value')}</th>
            <th>{uiText('Source')}</th>
            <th>{uiText('Actions')}</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => (
            <tr key={entry.envVar}>
              <td>{entry.name}</td>
              <td>
                <code>{entry.envVar}</code>
              </td>
              <td>{entry.value}</td>
              <td>{entry.source}</td>
              <td>
                <button onClick={() => handleCopy(entry)}>{uiText('Copy')}</button>
                {copiedEnvVar === entry.envVar && <span> {uiText('Copied')}</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
