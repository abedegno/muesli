import { useEffect, useRef } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { muesli } from '@/api'
import { defaultMeetingTitle } from '@/lib/datetime'

interface Ctx { refresh: () => void }

// NewMeetingScreen creates a note immediately and routes into it in capture mode.
export function NewMeetingScreen() {
  const navigate = useNavigate()
  const { refresh } = useOutletContext<Ctx>()
  const started = useRef(false)
  useEffect(() => {
    if (started.current) return
    started.current = true
    const defaultTitle = defaultMeetingTitle(new Date())
    muesli.createNote(defaultTitle).then((n) => {
      refresh()
      navigate(`/notes/${n.id}?capture=1`, { replace: true })
    })
  }, [navigate, refresh])
  return <div className="p-8 text-muted-foreground">Starting a new meeting…</div>
}
