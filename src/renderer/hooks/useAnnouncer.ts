import { createContext, useCallback, useContext, useRef, useState } from 'react'

export interface AnnouncerContextValue {
  /** The current polite (aria-live="polite") message. */
  politeMessage: string
  /** The current assertive (aria-live="assertive") message. */
  assertiveMessage: string
  /** Announce a status message via the polite region. Clears after 5 s. */
  announce: (msg: string) => void
  /** Announce an urgent message via the assertive region. Clears after 5 s. */
  announceAssertive: (msg: string) => void
}

const CLEAR_DELAY_MS = 5000

export const AnnouncerContext = createContext<AnnouncerContextValue>({
  politeMessage: '',
  assertiveMessage: '',
  announce: () => undefined,
  announceAssertive: () => undefined,
})

/** Returns `{ announce, announceAssertive }` for posting screen-reader announcements. */
export function useAnnouncer(): Pick<AnnouncerContextValue, 'announce' | 'announceAssertive'> {
  const { announce, announceAssertive } = useContext(AnnouncerContext)
  return { announce, announceAssertive }
}

/**
 * Internal hook that owns the state and timers; used by `AnnouncerProvider`.
 * Exported so tests can inspect the full context value.
 */
export function useAnnouncerState(): AnnouncerContextValue {
  const [politeMessage, setPoliteMessage] = useState('')
  const [assertiveMessage, setAssertiveMessage] = useState('')
  const politeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const assertiveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const announce = useCallback((msg: string) => {
    if (politeTimer.current) clearTimeout(politeTimer.current)
    setPoliteMessage(msg)
    politeTimer.current = setTimeout(() => setPoliteMessage(''), CLEAR_DELAY_MS)
  }, [])

  const announceAssertive = useCallback((msg: string) => {
    if (assertiveTimer.current) clearTimeout(assertiveTimer.current)
    setAssertiveMessage(msg)
    assertiveTimer.current = setTimeout(() => setAssertiveMessage(''), CLEAR_DELAY_MS)
  }, [])

  return { politeMessage, assertiveMessage, announce, announceAssertive }
}
