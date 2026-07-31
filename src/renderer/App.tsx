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
  const [connected, setConnected] = useState<boolean | null>(null)
  const [onboarded, setOnboarded] = useState<boolean | null>(null)
  const [connectMessage, setConnectMessage] = useState<string | null>(null)
  async function refreshConnection() {
    try {
      const onboardedPromise = muesli.getOnboarded
        ? muesli.getOnboarded().catch(() => false)
        : Promise.resolve(false)
      const [cfg, nextOnboarded] = await Promise.all([muesli.getConfig?.(), onboardedPromise])
      setConnected(!!cfg)
      setOnboarded(nextOnboarded ?? false)
      if (cfg) setConnectMessage(null)
    } catch {
      setConnected(false)
      setOnboarded(false)
    }
  }

  useEffect(() => {
    void refreshConnection()
  }, [])

  useEffect(() => {
    const unsubscribe = muesli.onAuthInvalidated?.((notice) => {
      setConnectMessage(notice.message)
      setConnected(false)
    })
    return unsubscribe
  }, [])

  return (
    <AnnouncerProvider>
      <AriaAnnouncer />
      {connected === null && (
        <div className="p-8 text-muted-foreground">Loading…</div>
      )}
      {connected === false && (
        <ConnectScreen
          onConnected={() => {
            setConnectMessage(null)
            setConnected(true)
          }}
          message={connectMessage}
        />
      )}
      {connected === true && onboarded === false && (
        <SetupWizard
          onDone={() => {
            void muesli.setOnboarded(true)
            setOnboarded(true)
          }}
        />
      )}
      {connected === true && onboarded === true && (
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
                      setConnected(false)
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
      {connected === true && onboarded === null && (
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
