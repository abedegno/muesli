import { expect, test } from '../fixtures/app'
import { waitForMuesliConnection } from '../helpers/seed'

// Embedded-shape end-to-end for live in-meeting prompts (issue #764): the
// desktop app's own transport -- preload IPC, the main-process relay, an
// authenticated HTTP stream to the embedded Go server, SSE parsing, typed
// events back to the renderer -- against a real server, template and note.
//
// The desktop E2E tier has no live audio source (its streaming transcriber is
// the real whisper binary without a model, so recording degrades to batch),
// so the meeting itself is not driven here; the hosted-shaped two-process
// meeting scenario lives in internal/api/live_prompts_e2e_test.go. What this
// spec proves is the layer no Go test can reach: the packaged transport
// reaches the server as the signed-in user and the subscription lifecycle
// (snapshot on connect, silence after stop, a fresh subscription after the
// lease is released) holds end to end.

type LivePromptsBridgeEvent = { noteId: string; type: string; active?: boolean; items?: unknown[] }

type LivePromptsBridge = {
  createNote(title: string): Promise<{ id: string }>
  createTemplate(
    name: string,
    phase: string,
    sections: { heading: string; instruction: string }[],
    autoRun: boolean
  ): Promise<{ id: string }>
  startLivePrompts(noteId: string): Promise<void>
  stopLivePrompts(noteId: string): Promise<void>
  onLivePromptsEvent(cb: (event: LivePromptsBridgeEvent) => void): () => void
}

test('live prompts: the desktop transport delivers a snapshot and releases on stop', async ({
  page,
}) => {
  await waitForMuesliConnection(page)

  const result = await page.evaluate(async () => {
    const bridge = (window as unknown as { muesli: LivePromptsBridge }).muesli
    await bridge.createTemplate(
      'Live e2e prompt',
      'during',
      [{ heading: 'Live', instruction: 'Summarize.' }],
      true
    )
    const note = await bridge.createNote('Live prompts e2e')

    const events: LivePromptsBridgeEvent[] = []
    const detach = bridge.onLivePromptsEvent((event) => {
      if (event.noteId === note.id) events.push(event)
    })
    const waitForType = (type: string) =>
      new Promise<void>((resolve, reject) => {
        const deadline = setTimeout(
          () =>
            reject(new Error(`no ${type} event; saw ${JSON.stringify(events.map((e) => e.type))}`)),
          15_000
        )
        const tick = () => {
          if (events.some((e) => e.type === type)) {
            clearTimeout(deadline)
            resolve()
          } else {
            setTimeout(tick, 50)
          }
        }
        tick()
      })

    await bridge.startLivePrompts(note.id)
    await waitForType('snapshot')
    const firstTypes = events.map((e) => e.type)
    const snapshot = events.find((e) => e.type === 'snapshot')

    await bridge.stopLivePrompts(note.id)
    const seenAtStop = events.length
    await new Promise((resolve) => setTimeout(resolve, 500))
    const quietAfterStop = events.length === seenAtStop

    events.length = 0
    await bridge.startLivePrompts(note.id)
    await waitForType('snapshot')
    await bridge.stopLivePrompts(note.id)
    detach()

    return {
      firstTypes,
      snapshot: snapshot ? { active: snapshot.active, items: snapshot.items } : null,
      quietAfterStop,
      resubscribed: events.some((e) => e.type === 'snapshot'),
    }
  })

  expect(result.firstTypes.slice(0, 3)).toEqual(['connecting', 'live', 'snapshot'])
  expect(result.snapshot).toEqual({ active: false, items: [] })
  expect(result.quietAfterStop).toBe(true)
  expect(result.resubscribed).toBe(true)
})
