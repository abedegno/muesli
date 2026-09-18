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
 */
export function subscribeLivePrompts(noteId: string, callback: (event: LivePromptsEvent) => void): () => void {
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
    void muesli.stopLivePrompts(noteId)
  }
}
