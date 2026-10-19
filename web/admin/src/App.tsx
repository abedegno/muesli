import { uiText } from '@/i18n'
import { useEffect, useState, useMemo } from 'react'
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
import { AccessView } from './views/AccessView'
import { useLocale, languageChoice, setDisplayLanguage, getDisplayLanguage } from './i18n'
import { ConfigView } from './views/ConfigView'

interface Props {
  client?: ApiClient
  session?: SessionStore
}

type Boot = 'loading' | 'setup' | 'login' | 'console' | 'forbidden' | 'access-error'
type Tab =
  | 'plugins'
  | 'jobs'
  | 'webhook-deliveries'
  | 'backups'
  | 'embeddings'
  | 'health'
  | 'config'
  | 'access'

export function App({ client, session }: Props) {
  const sess = session ?? defaultSession
  const locale = useLocale()
  useEffect(() => {
    document.documentElement.lang = getDisplayLanguage()
  }, [locale])
  const api = useMemo(() => client ?? new ApiClient(sess), [client, sess])
  const [boot, setBoot] = useState<Boot>('loading')
  const [tab, setTab] = useState<Tab>('plugins')

  async function authorization(): Promise<Boot> {
    try {
      return (await api.getCapabilities()).is_admin ? 'console' : 'forbidden'
    } catch {
      return sess.isAuthenticated() ? 'access-error' : 'login'
    }
  }

  useEffect(() => {
    let cancelled = false
    async function decide() {
      if (sess.isAuthenticated()) {
        const next = await authorization()
        if (!cancelled) setBoot(next)
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

  const languageControl = (
    <label>
      {uiText('Display language')}{' '}
      <select
        aria-label={uiText('Display language')}
        value={languageChoice()}
        onChange={(event) => setDisplayLanguage(event.target.value)}
      >
        <option value="system">{uiText('Follow system')}</option>
        <option value="en">{uiText('English')}</option>
        <option value="fr">{uiText('Français')}</option>
        <option value="de">{uiText('Deutsch')}</option>
        <option value="es">{uiText('Español')}</option>
      </select>
    </label>
  )
  const signOut = () => {
    sess.clear()
    setBoot('login')
  }
  if (boot === 'forbidden' || boot === 'access-error')
    return (
      <main className="shell">
        {languageControl}
        <h1>
          {boot === 'forbidden'
            ? uiText('Administrator access required')
            : uiText('Could not check your access')}
        </h1>
        <p>
          {boot === 'forbidden'
            ? uiText(
                'Your account can use Muesli. Ask a server administrator to grant access to this console.'
              )
            : uiText('Check your connection and try again.')}
        </p>
        {boot === 'access-error' && (
          <button
            onClick={() => {
              setBoot('loading')
              void authorization().then(setBoot)
            }}
          >
            {uiText('Try again')}
          </button>
        )}
        <button onClick={signOut}>{uiText('Sign out')}</button>
      </main>
    )

  if (boot === 'loading')
    return (
      <main className="shell startup-shell">
        <div className="startup-card">
          <h1 className="startup-heading">{uiText('Muesli is starting up…')}</h1>
          <p className="startup-subtitle">{uiText('This may take a moment on the first boot.')}</p>
        </div>
      </main>
    )

  if (boot === 'setup') {
    return (
      <SetupView
        onSubmit={async (email, password) => {
          await api.setup(email, password)
          await api.login(email, password)
          setBoot(await authorization())
        }}
      />
    )
  }

  if (boot === 'login') {
    return (
      <LoginView
        onSubmit={async (email, password) => {
          await api.login(email, password)
          setBoot(await authorization())
        }}
      />
    )
  }

  return (
    <main className="shell">
      {languageControl}
      <nav className="row">
        <button onClick={() => setTab('access')}>{uiText('Account access')}</button>
        <button onClick={() => setTab('plugins')}>{uiText('Plugins')}</button>
        <button onClick={() => setTab('jobs')}>{uiText('Jobs')}</button>
        <button onClick={() => setTab('webhook-deliveries')}>{uiText('Webhook deliveries')}</button>
        <button onClick={() => setTab('backups')}>{uiText('Backups')}</button>
        <button onClick={() => setTab('embeddings')}>{uiText('Embeddings')}</button>
        <button onClick={() => setTab('health')}>{uiText('Health')}</button>
        <button onClick={() => setTab('config')}>{uiText('Config')}</button>
        <button
          onClick={() => {
            sess.clear()
            setBoot('login')
          }}
        >
          {uiText('Sign out')}
        </button>
      </nav>
      {tab === 'access' && (
        <AccessView client={api} onChanged={async () => setBoot(await authorization())} />
      )}
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
