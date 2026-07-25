import { useState } from 'react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { INSECURE_CONNECTION_CODE } from '../../shared/url'

export function ConnectScreen({ onConnected, message }: { onConnected: () => void; message?: string | null }) {
  const [serverUrl, setServerUrl] = useState('http://localhost:8080')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [isFirstRun, setIsFirstRun] = useState(false)
  const [allowInsecure, setAllowInsecure] = useState(false)
  const [insecureBlocked, setInsecureBlocked] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await muesli.connect({ serverUrl, email, password, isFirstRun, allowInsecure })
      onConnected()
    } catch (err) {
      if (err instanceof Error && err.message.includes(INSECURE_CONNECTION_CODE)) {
        setInsecureBlocked(true)
      } else {
        setError(err instanceof Error ? err.message : 'Could not connect')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex h-full items-center justify-center bg-app-chrome">
      <form onSubmit={submit} className="w-80 rounded-[var(--radius)] border border-border bg-card p-6 shadow-md">
        <h1 className="mb-1 text-lg font-semibold">Connect to Muesli</h1>
        <p className="mb-4 text-sm text-muted-foreground">Your self-hosted server.</p>
        {message ? (
          <div role="alert" className="mb-4 rounded-[var(--radius)] border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-950 dark:text-amber-100">
            {message}
          </div>
        ) : null}
        <div className="flex flex-col gap-3">
          <Field label="Server URL"><Input value={serverUrl} onChange={(e) => setServerUrl(e.target.value)} /></Field>
          <Field label="Email"><Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} /></Field>
          <Field label="Password"><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            <input type="checkbox" checked={isFirstRun} onChange={(e) => setIsFirstRun(e.target.checked)} />
            First run (create the account)
          </label>
          {insecureBlocked && (
            <div role="alert" className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/10 p-2 text-sm">
              <p className="text-destructive">This server uses an unencrypted (HTTP) connection — your audio, notes, and password would be sent in the clear.</p>
              <p className="mt-1 text-muted-foreground">Use an <code>https://</code> URL, or:</p>
              <label className="mt-2 flex items-center gap-2 text-muted-foreground">
                <input type="checkbox" checked={allowInsecure} onChange={(e) => setAllowInsecure(e.target.checked)} />
                Connect anyway over an unencrypted connection
              </label>
            </div>
          )}
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          <Button type="submit" disabled={busy}>{busy ? 'Connecting…' : 'Connect'}</Button>
        </div>
      </form>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block font-medium">{label}</span>
      {children}
    </label>
  )
}
