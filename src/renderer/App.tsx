import { lazy, Suspense, useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { muesli } from './api'
import { AppLayout } from './components/shell/AppLayout'
import { ConnectScreen } from './components/ConnectScreen'
import { NotesListScreen } from './components/NotesListScreen'
import { AnnouncerProvider, AriaAnnouncer } from './components/AriaAnnouncer'

const NewMeetingScreen = lazy(() => import('./components/NewMeetingScreen').then(m => ({ default: m.NewMeetingScreen })))
const SettingsScreen = lazy(() => import('./components/SettingsScreen').then(m => ({ default: m.SettingsScreen })))
const TemplatesScreen = lazy(() => import('./components/TemplatesScreen').then(m => ({ default: m.TemplatesScreen })))
const TrashScreen = lazy(() => import('./components/TrashScreen').then(m => ({ default: m.TrashScreen })))
const TagsPage = lazy(() => import('./components/TagsPage').then(m => ({ default: m.TagsPage })))
const ChatScreen = lazy(() => import('./components/chat/ChatScreen').then(m => ({ default: m.ChatScreen })))
const ComingUpScreen = lazy(() => import('./components/ComingUpScreen').then(m => ({ default: m.ComingUpScreen })))
const PeopleScreen = lazy(() => import('./components/PeopleScreen').then(m => ({ default: m.PeopleScreen })))
const NoteScreen = lazy(() => import('./components/NoteScreen').then((m) => ({ default: m.NoteScreen })))

export function App() {
  const [connected, setConnected] = useState<boolean | null>(null)
  useEffect(() => {
    muesli.getConfig?.()?.then((cfg) => setConnected(!!cfg)).catch(() => setConnected(false))
  }, [])

  return (
    <AnnouncerProvider>
      <AriaAnnouncer />
      {connected === null && (
        <div className="p-8 text-muted-foreground">Loading…</div>
      )}
      {connected === false && (
        <ConnectScreen onConnected={() => setConnected(true)} />
      )}
      {connected === true && (
        <Suspense fallback={<div className="p-8 text-muted-foreground">Loading…</div>}>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/" element={<NotesListScreen />} />
              <Route path="/notes/:id" element={<NoteScreen />} />
              <Route path="/new" element={<NewMeetingScreen />} />
              <Route path="/settings" element={<SettingsScreen onDisconnected={() => setConnected(false)} />} />
              <Route path="/templates" element={<TemplatesScreen />} />
              <Route path="/chat" element={<ChatScreen />} />
              <Route path="/settings/tags" element={<TagsPage />} />
              <Route path="/trash" element={<TrashScreen />} />
              <Route path="/coming-up" element={<ComingUpScreen />} />
              <Route path="/people" element={<PeopleScreen />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </Suspense>
      )}
    </AnnouncerProvider>
  )
}
