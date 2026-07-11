import { useCallback, useEffect, useState } from 'react'
import type { ApiClient } from '../api/client'
import type { EmbeddingStatus } from '../api/types'

interface Props {
  client: Pick<ApiClient, 'getEmbeddingStatus' | 'reembedAll'>
}

// EmbeddingsView is the admin on-demand re-embed panel (EMB02): it shows live
// done/total progress for the currently configured (model, dim) and lets an
// admin trigger a full re-embed, reusing the same store/worker machinery as
// the `muesli reembed` CLI. Distinct from EmbeddingsStatusView (EMB01), which
// only reports static config (minScore/prefixes) with no progress or action.
export function EmbeddingsView({ client }: Props) {
  const [status, setStatus] = useState<EmbeddingStatus | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [reembedError, setReembedError] = useState<string | null>(null)
  const [reembedding, setReembedding] = useState(false)

  const refresh = useCallback(async () => {
    try {
      setStatus(await client.getEmbeddingStatus())
      setStatusError(null)
    } catch (err) {
      setStatusError(err instanceof Error ? err.message : 'failed to load embedding status')
    }
  }, [client])

  useEffect(() => {
    void refresh()
  }, [refresh])

  async function reembedAll() {
    setReembedding(true)
    setReembedError(null)
    try {
      await client.reembedAll()
      await refresh()
    } catch (err) {
      setReembedError(err instanceof Error ? err.message : 'failed to start re-embed')
    } finally {
      setReembedding(false)
    }
  }

  const disabled = !status?.enabled || reembedding

  return (
    <section>
      <div className="row">
        <h1>Re-embed notes</h1>
        <button onClick={() => void refresh()} disabled={reembedding}>
          Refresh
        </button>
        <button onClick={() => void reembedAll()} disabled={disabled}>
          {reembedding ? 'Re-embedding…' : 'Re-embed all notes'}
        </button>
      </div>

      {statusError && <p className="error">{statusError}</p>}
      {reembedError && <p className="error">{reembedError}</p>}

      {status && (
        <table>
          <tbody>
            <tr>
              <th>Status</th>
              <td>
                <strong>{status.enabled ? 'Enabled' : 'Disabled'}</strong>
              </td>
            </tr>
            <tr>
              <th>Model</th>
              <td>{status.model || '(not set)'}</td>
            </tr>
            <tr>
              <th>Dimension</th>
              <td>{status.dim}</td>
            </tr>
            <tr>
              <th>Progress</th>
              <td>
                {status.done} / {status.total} notes embedded
              </td>
            </tr>
          </tbody>
        </table>
      )}
    </section>
  )
}
