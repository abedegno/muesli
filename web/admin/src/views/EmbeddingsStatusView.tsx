import { uiText } from '@/i18n'
import { useCallback, useEffect, useState } from 'react'
import type { ApiClient } from '../api/client'
import type { EmbeddingsStatus } from '../api/types'

interface Props {
  client: Pick<ApiClient, 'getEmbeddingsStatus'>
}

export function EmbeddingsStatusView({ client }: Props) {
  const [status, setStatus] = useState<EmbeddingsStatus | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setStatus(await client.getEmbeddingsStatus())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load embeddings status')
    }
  }, [client])

  useEffect(() => {
    void refresh()
  }, [refresh])

  if (error) {
    return (
      <section>
        <h1>{uiText('Embeddings')}</h1>
        <p className="error">{error}</p>
      </section>
    )
  }

  if (!status) {
    return (
      <section>
        <h1>{uiText('Embeddings')}</h1>
        <p>{uiText('Loading…')}</p>
      </section>
    )
  }

  return (
    <section>
      <h1>{uiText('Embeddings')}</h1>
      <table>
        <tbody>
          <tr>
            <th>{uiText('Status')}</th>
            <td>
              <strong>{status.enabled ? uiText('Enabled') : uiText('Disabled')}</strong>
            </td>
          </tr>
          <tr>
            <th>{uiText('Model')}</th>
            <td>{status.model || uiText('(not set)')}</td>
          </tr>
          <tr>
            <th>{uiText('Dimension')}</th>
            <td>{status.dim}</td>
          </tr>
          <tr>
            <th>{uiText('Min Score')}</th>
            <td>{status.minScore}</td>
          </tr>
          <tr>
            <th>{uiText('Doc Prefix')}</th>
            <td>
              <code>{status.docPrefix || '(none)'}</code>
            </td>
          </tr>
          <tr>
            <th>{uiText('Query Prefix')}</th>
            <td>
              <code>{status.queryPrefix || '(none)'}</code>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  )
}
