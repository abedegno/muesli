import { type ReactNode } from 'react'
import { AnnouncerContext, useAnnouncerState } from '../hooks/useAnnouncer'

/** SR-only styles: element is hidden visually but readable by screen readers. */
const srOnly: React.CSSProperties = {
  position: 'absolute',
  width: '1px',
  height: '1px',
  padding: 0,
  margin: '-1px',
  overflow: 'hidden',
  clip: 'rect(0,0,0,0)',
  whiteSpace: 'nowrap',
  border: 0,
}

/**
 * Provides `AnnouncerContext` to the tree and renders the two aria-live regions
 * that screen readers watch. Place this at the app root so the regions are
 * always in the DOM.
 */
export function AnnouncerProvider({ children }: { children: ReactNode }) {
  const value = useAnnouncerState()
  return (
    <AnnouncerContext.Provider value={value}>
      {children}
    </AnnouncerContext.Provider>
  )
}

/**
 * Renders the two invisible aria-live regions consumed by screen readers.
 * Must be placed inside `AnnouncerProvider`.
 */
export function AriaAnnouncer() {
  return (
    <AnnouncerContext.Consumer>
      {({ politeMessage, assertiveMessage }) => (
        <>
          <div
            aria-live="polite"
            aria-atomic="true"
            style={srOnly}
          >
            {politeMessage}
          </div>
          <div
            aria-live="assertive"
            aria-atomic="true"
            style={srOnly}
          >
            {assertiveMessage}
          </div>
        </>
      )}
    </AnnouncerContext.Consumer>
  )
}
