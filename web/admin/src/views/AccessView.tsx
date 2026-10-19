import { uiText } from '@/i18n'
import { useState } from 'react'
import type { ApiClient } from '../api/client'
export function AccessView({
  client,
  onChanged,
}: {
  client: ApiClient
  onChanged: () => Promise<void>
}) {
  const [email, setEmail] = useState(''),
    [role, setRole] = useState<'admin' | 'member'>('member'),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState('')
  return (
    <section>
      <h1>{uiText('Account access')}</h1>
      <p>
        {uiText(
          'Members can record and manage their own notes. Administrators can also manage server services, jobs and backups. Changes apply immediately. The last administrator cannot be removed.'
        )}
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          if (busy) return
          setBusy(true)
          setMessage('')
          void client
            .setUserRole(email.trim(), role)
            .then(async () => {
              setMessage('Account access updated.')
              await onChanged()
            })
            .catch((error) =>
              setMessage(
                error instanceof Error ? error.message : 'Could not update account access.'
              )
            )
            .finally(() => setBusy(false))
        }}
      >
        <label>
          {uiText('Existing account email')}
          <input
            type="email"
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </label>
        <label>
          {uiText('Role')}
          <select
            aria-label={uiText('Role')}
            value={role}
            onChange={(event) => setRole(event.target.value as 'admin' | 'member')}
          >
            <option value="member">{uiText('Member')}</option>
            <option value="admin">{uiText('Administrator')}</option>
          </select>
        </label>
        <button disabled={busy} type="submit">
          {busy ? uiText('Saving…') : uiText('Save account access')}
        </button>
      </form>
      {message && <p role="status">{message}</p>}
    </section>
  )
}
