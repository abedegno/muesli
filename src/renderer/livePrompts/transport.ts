import { muesli } from '../api'
import type { LivePromptsEvent, LivePromptsItem } from '../../shared/ipc'

export type { LivePromptsEvent, LivePromptsItem }

/**
 * One transport-neutral live-prompts subscription (issue #764). The
 * Electron bridge owns the actual connection lifecycle (authenticated HTTP
 * stream + reconnect-with-backoff in `src/main/livePromptRelay.ts`); this
 * function only starts/stops it for one note and forwards its typed events,
 * so a caller (LivePromptsPanel) never needs to know which transport is
 * underneath.
 *
 * Callers must call the returned cleanup function on note change/unmount --
 * it stops the underlying subscription (releasing the server's capacity
 * lease) and detaches the event listener.
 *
 * Deployment shapes. The renderer only ever runs inside the desktop app:
 * there is no browser-served client (docs/ARCHITECTURE.md lists a web client
 * as later work), so "hosted" means the desktop app connected to a
 * self-hosted server (ConnectScreen), and both the embedded and the hosted
 * shapes reach the server the same way -- Electron main's authenticated
 * `fetch` + `ReadableStream` against the configured server URL, in
 * `src/main/livePromptRelay.ts`. The no-bridge branch below is not a
 * deployment shape; it is the test-double affordance described there.
 */
export function subscribeLivePrompts(noteId: string, callback: (event: LivePromptsEvent) => void): () => void {
  // Defensive: a lightweight test double for `window.muesli` (see many
  // existing component tests) commonly implements only the bridge members
  // it exercises. Treat a bridge missing these members the same as "no
  // transport available" rather than throwing, mirroring
  // LiveTranscriptPanel's own optional-source handling.
  if (typeof muesli.onLivePromptsEvent !== 'function' || typeof muesli.startLivePrompts !== 'function') {
    return () => {}
  }

  let stopped = false
  const detach = muesli.onLivePromptsEvent((event) => {
    if (stopped) return
    if (event.noteId !== noteId) return // note scoping: ignore stale events from a prior subscription
    callback(event)
  })
  void muesli.startLivePrompts(noteId)

  return () => {
    if (stopped) return
    stopped = true
    detach()
    void muesli.stopLivePrompts?.(noteId)
  }
}
