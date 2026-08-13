import { useEffect, useRef, useState } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { defaultMeetingTitle } from '@/lib/datetime'

interface Ctx { refresh: () => void }

// NewMeetingScreen creates a note immediately and routes into it in capture mode.
export function NewMeetingScreen() {
  const navigate = useNavigate()
  const { refresh } = useOutletContext<Ctx>()
  const started = useRef(false)
  const [error, setError] = useState<string | null>(null)

  function start() {
    started.current = true
    setError(null)
    const defaultTitle = defaultMeetingTitle(new Date())
    muesli.createNote(defaultTitle)
      .then((n) => {
        refresh()
        navigate(`/notes/${n.id}?capture=1`, { replace: true })
      })
      .catch((err) => {
        started.current = false
        setError(err instanceof Error ? err.message : 'Could not start a new meeting')
      })
  }

  useEffect(() => {
    if (started.current) return
    start()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (error) {
    return (
      <div className="p-8">
        <p role="alert" className="mb-3 text-sm text-destructive">{error}</p>
        <Button variant="secondary" onClick={start}>Try again</Button>
      </div>
    )
  }
  return <div className="p-8 text-muted-foreground">Starting a new meeting…</div>
}
