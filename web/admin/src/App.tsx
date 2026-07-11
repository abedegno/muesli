import { useEffect, useState } from 'react'
import { ApiClient } from './api/client'
import { SessionStore, session as defaultSession } from './auth/session'
import { SetupView } from './views/SetupView'
import { LoginView } from './views/LoginView'
import { PluginsView } from './views/PluginsView'
import { JobsView } from './views/JobsView'
import { WebhookDeliveriesView } from './views/WebhookDeliveriesView'
import { BackupsView } from './views/BackupsView'
import { EmbeddingsStatusView } from './views/EmbeddingsStatusView'
import { EmbeddingsView } from './views/EmbeddingsView'
import { HealthView } from './views/HealthView'
import { ConfigView } from './views/ConfigView'

interface Props {
  client?: ApiClient
  session?: SessionStore
}

type Boot = 'loading' | 'setup' | 'login' | 'console'
type Tab = 'plugins' | 'jobs' | 'webhook-deliveries' | 'backups' | 'embeddings' | 'health' | 'config'

export function App({ client, session }: Props) {
  const sess = session ?? defaultSession
  const api = client ?? new ApiClient(sess)
  const [boot, setBoot] = useState<Boot>('loading')
  const [tab, setTab] = useState<Tab>('plugins')

  useEffect(() => {
    let cancelled = false
    async function decide() {
      if (sess.isAuthenticated()) {
        if (!cancelled) setBoot('console')
        return
      }
      try {
        const status = await api.getSetupStatus()
        if (!cancelled) setBoot(status.needs_setup ? 'setup' : 'login')
      } catch {
        if (!cancelled) setBoot('login')
      }
    }
    void decide()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (boot === 'loading')
    return (
      <main className="shell startup-shell">
        <div className="startup-card">
          <h1 className="startup-heading">Muesli is starting up…</h1>
          <p className="startup-subtitle">This may take a moment on the first boot.</p>
        </div>
      </main>
    )

  if (boot === 'setup') {
    return (
      <SetupView
        onSubmit={async (email, password) => {
          await api.setup(email, password)
          await api.login(email, password)
          setBoot('console')
        }}
      />
    )
  }

  if (boot === 'login') {
    return (
      <LoginView
        onSubmit={async (email, password) => {
          await api.login(email, password)
          setBoot('console')
        }}
      />
    )
  }

  return (
    <main className="shell">
      <nav className="row">
        <button onClick={() => setTab('plugins')}>Plugins</button>
        <button onClick={() => setTab('jobs')}>Jobs</button>
        <button onClick={() => setTab('webhook-deliveries')}>Webhook deliveries</button>
        <button onClick={() => setTab('backups')}>Backups</button>
        <button onClick={() => setTab('embeddings')}>Embeddings</button>
        <button onClick={() => setTab('health')}>Health</button>
        <button onClick={() => setTab('config')}>Config</button>
        <button
          onClick={() => {
            sess.clear()
            setBoot('login')
          }}
        >
          Sign out
        </button>
      </nav>
      {tab === 'plugins' && <PluginsView client={api} />}
      {tab === 'jobs' && <JobsView client={api} />}
      {tab === 'webhook-deliveries' && <WebhookDeliveriesView client={api} />}
      {tab === 'backups' && <BackupsView client={api} />}
      {tab === 'embeddings' && (
        <>
          <EmbeddingsView client={api} />
          <EmbeddingsStatusView client={api} />
        </>
      )}
      {tab === 'health' && <HealthView client={api} />}
      {tab === 'config' && <ConfigView client={api} />}
    </main>
  )
}
