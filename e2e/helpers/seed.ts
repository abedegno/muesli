import { expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import { makeSilentWav } from './wav'

type NoteStatus = 'recording' | 'uploaded' | 'transcribing' | 'summarizing' | 'ready' | 'failed'

type E2ENote = {
  id: string
  status: NoteStatus
}

type E2EMuesliBridge = {
  getConfig(): Promise<{ serverUrl: string; token: string } | null>
  createNote(title: string): Promise<E2ENote>
  uploadAudio(req: {
    noteId: string
    audio: ArrayBuffer
    audioMimeType: 'audio/wav'
  }): Promise<{ noteId: string }>
  listNotes(folderId?: string): Promise<E2ENote[]>
}

type MuesliWindow = Window & typeof globalThis & { muesli: E2EMuesliBridge }

export async function waitForMuesliConnection(page: Page): Promise<void> {
  await expect
    .poll(
      () =>
        page.evaluate(async () => {
          const config = await (window as MuesliWindow).muesli.getConfig()
          return config !== null
        }),
      { timeout: 30_000 }
    )
    .toBe(true)
}

export async function seedNoteWithAudio(
  page: Page,
  opts: { title: string }
): Promise<{ noteId: string }> {
  await waitForMuesliConnection(page)
  const audioBytes = Array.from(makeSilentWav(1))
  const noteId = await page.evaluate(
    async ({ title, bytes }) => {
      const bridge = (window as MuesliWindow).muesli
      const note = await bridge.createNote(title)
      const audio = Uint8Array.from(bytes).buffer
      await bridge.uploadAudio({ noteId: note.id, audio, audioMimeType: 'audio/wav' })
      return note.id
    },
    { title: opts.title, bytes: audioBytes }
  )

  await expect
    .poll(
      async () =>
        page.evaluate(async (id) => {
          const notes = await (window as MuesliWindow).muesli.listNotes()
          return notes.find((note) => note.id === id)?.status
        }, noteId),
      { timeout: 30_000 }
    )
    .toBe('ready')

  return { noteId }
}
