import { lazy, Suspense, useEffect, useState } from 'react'
import { Routes, Route, Navigate, useLocation, useNavigate } from 'react-router-dom'
import { muesli } from './api'
import { EmbeddedStartupGate } from './components/StartupScreen'
import { SetupWizard } from './components/SetupWizard'
import { AppLayout } from './components/shell/AppLayout'
import { ConnectScreen } from './components/ConnectScreen'
import { NotesListScreen } from './components/NotesListScreen'
import { AnnouncerProvider, AriaAnnouncer } from './components/AriaAnnouncer'

const NewMeetingScreen = lazy(() => import('./components/NewMeetingScreen').then(m => ({ default: m.NewMeetingScreen })))
const HomeScreen = lazy(() => import('./components/HomeScreen').then(m => ({ default: m.HomeScreen })))
const SettingsScreen = lazy(() => import('./components/SettingsScreen').then(m => ({ default: m.SettingsScreen })))
const TemplatesScreen = lazy(() => import('./components/TemplatesScreen').then(m => ({ default: m.TemplatesScreen })))
const TrashScreen = lazy(() => import('./components/TrashScreen').then(m => ({ default: m.TrashScreen })))
const TagsPage = lazy(() => import('./components/TagsPage').then(m => ({ default: m.TagsPage })))
const ChatScreen = lazy(() => import('./components/chat/ChatScreen').then(m => ({ default: m.ChatScreen })))
const PeopleScreen = lazy(() => import('./components/PeopleScreen').then(m => ({ default: m.PeopleScreen })))
const InsightsScreen = lazy(() => import('./components/InsightsScreen').then(m => ({ default: m.InsightsScreen })))
const SearchScreen = lazy(() => import('./components/SearchScreen').then(m => ({ default: m.SearchScreen })))
const ActionItemsScreen = lazy(() => import('./components/ActionItemsScreen').then(m => ({ default: m.ActionItemsScreen })))
const PersonDetailScreen = lazy(() => import('./components/PersonDetailScreen').then(m => ({ default: m.PersonDetailScreen })))
const CompanyDetailScreen = lazy(() => import('./components/CompanyDetailScreen').then(m => ({ default: m.CompanyDetailScreen })))
const NoteScreen = lazy(() => import('./components/NoteScreen').then((m) => ({ default: m.NoteScreen })))

function AppContent() {
  const [connection, setConnection] = useState<'starting' | 'connected' | 'needs-setup' | 'server-unreachable'>('starting')
  const [onboarded, setOnboarded] = useState<boolean | null>(null)
  const [connectMessage, setConnectMessage] = useState<string | null>(null)
  const navigate = useNavigate()
  async function refreshConnection(): Promise<boolean> {
    try {
      console.log('[muesli-debug] connection -> starting')
      setConnection('starting')
      const onboardedPromise = muesli.getOnboarded
        ? muesli.getOnboarded().catch(() => false)
        : Promise.resolve(false)
      const localStatus = muesli.getLocalSessionStatus
        ? await muesli.getLocalSessionStatus()
        : null
      if (localStatus === 'server-unreachable') {
        console.log('[muesli-debug] connection -> server-unreachable')
        setConnection('server-unreachable')
        setOnboarded(false)
        return true
      }
      const [cfg, nextOnboarded] = await Promise.all([muesli.getConfig?.(), onboardedPromise])
      const isConnected = !!cfg && localStatus !== 'needs-setup'
      console.log(`[muesli-debug] connection -> ${isConnected ? 'connected' : 'needs-setup'}`, {
        localStatus,
        hasConfig: !!cfg,
        onboarded: nextOnboarded,
      })
      setConnection(isConnected ? 'connected' : 'needs-setup')
      setOnboarded(nextOnboarded ?? false)
      if (cfg) setConnectMessage(null)
      return false
    } catch (err) {
      console.error('[muesli-debug] connection -> server-unreachable (error)', err)
      setConnection('server-unreachable')
      setOnboarded(false)
      return true
    }
  }

  useEffect(() => {
    let cancelled = false
    let retryTimer: number | null = null

    const connectWithRetries = async (attempt: number) => {
      const shouldRetry = await refreshConnection()
      if (cancelled || !shouldRetry || attempt >= 2) return
      retryTimer = window.setTimeout(() => {
        void connectWithRetries(attempt + 1)
      }, 5000)
    }

    void connectWithRetries(0)
    return () => {
      cancelled = true
      if (retryTimer !== null) window.clearTimeout(retryTimer)
    }
  }, [])

  useEffect(() => {
    const unsubscribe = muesli.onAuthInvalidated?.((notice) => {
      setConnectMessage(notice.message)
      setConnection('needs-setup')
    })
    return unsubscribe
  }, [])

  useEffect(() => {
    const unsubscribe = muesli.onTrayNavigate?.((target) => {
      navigate(target)
    })
    return unsubscribe
  }, [navigate])

  return (
    <AnnouncerProvider>
      <AriaAnnouncer />
      {connection === 'starting' && (
        <div className="flex h-full items-center justify-center p-8 text-muted-foreground">Starting up the local server…</div>
      )}
      {connection === 'server-unreachable' && (
        <div className="flex h-full items-center justify-center p-8">
          <section className="max-w-xl rounded-[var(--radius)] border border-destructive/30 bg-card px-8 py-7 shadow-md">
            <h1 className="text-2xl font-semibold tracking-tight">Couldn't reach the local server</h1>
            <p className="mt-3 text-sm text-muted-foreground">Muesli could not finish starting its local server. Your saved account and notes have not been changed. Restart Muesli to try again.</p>
          </section>
        </div>
      )}
      {connection === 'needs-setup' && (
        <ConnectScreen
          onConnected={() => {
            setConnectMessage(null)
            setConnection('connected')
          }}
          message={connectMessage}
        />
      )}
      {connection === 'connected' && onboarded === false && (
        <SetupWizard
          onDone={() => {
            void muesli.setOnboarded(true)
            setOnboarded(true)
          }}
        />
      )}
      {connection === 'connected' && onboarded === true && (
        <Suspense fallback={<div className="p-8 text-muted-foreground">Loading…</div>}>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/" element={<HomeScreen />} />
              <Route path="/notes" element={<NotesListScreen />} />
              <Route path="/notes/:id" element={<NoteScreen />} />
              <Route path="/new" element={<NewMeetingScreen />} />
              <Route
                path="/settings"
                element={
                  <SettingsScreen
                    onDisconnected={() => {
                      setConnectMessage(null)
                      setConnection('needs-setup')
                    }}
                    onResetToBuiltIn={async () => {
                      setConnectMessage(null)
                      await refreshConnection()
                    }}
                  />
                }
              />
              <Route path="/templates" element={<TemplatesScreen />} />
              <Route path="/chat" element={<ChatScreen />} />
              <Route path="/settings/tags" element={<TagsPage />} />
              <Route path="/trash" element={<TrashScreen />} />
              <Route path="/coming-up" element={<Navigate to="/" replace />} />
              <Route path="/action-items" element={<ActionItemsScreen />} />
              <Route path="/people/:id" element={<PersonDetailScreen />} />
              <Route path="/companies/:id" element={<CompanyDetailScreen />} />
              <Route path="/people" element={<PeopleScreen />} />
              <Route path="/insights" element={<InsightsScreen />} />
              <Route path="/search" element={<SearchScreen />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </Suspense>
      )}
      {connection === 'connected' && onboarded === null && (
        <div className="p-8 text-muted-foreground">Loading…</div>
      )}
    </AnnouncerProvider>
  )
}

export function App() {
  const navigate = useNavigate()
  const location = useLocation()
  return (
    <EmbeddedStartupGate
      hideDegradedBanner={location.pathname === '/settings'}
      onOpenAiSettings={() => navigate('/settings#ai-transcription')}
    >
      <AppContent />
    </EmbeddedStartupGate>
  )
}
