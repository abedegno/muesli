import { describe, expect, it } from 'vitest'
import { uploadAudioToNote } from './uploadMachine'

describe('uploadAudioToNote', () => {
  it('presigns, PUTs bytes, and confirms — without creating a note or body', async () => {
    const phases: string[] = []
    const calls: string[] = []
    const client = {
      getAudioUploadUrl: async (id: string) => { calls.push(`grant:${id}`); return { url: 'http://s/put', method: 'PUT' as const, key: 'k1', expires_at: '' } },
      putAudio: async () => { calls.push('put') },
      markAudioUploaded: async (id: string, key: string) => { calls.push(`confirm:${id}:${key}`); return { status: 'uploaded' } },
    } as unknown as import('./muesliClient').MuesliClient
    await uploadAudioToNote(client, { noteId: 'n1', audio: new Uint8Array([1,2,3]), audioMimeType: 'audio/webm' }, (p) => phases.push(p.phase))
    expect(calls).toEqual(['grant:n1', 'put', 'confirm:n1:k1'])
    expect(phases).toEqual(['requesting-url', 'uploading-audio', 'confirming-upload', 'done'])
  })
  it('does nothing but report done when there is no audio', async () => {
    const phases: string[] = []
    const client = {} as unknown as import('./muesliClient').MuesliClient
    await uploadAudioToNote(client, { noteId: 'n1', audio: null }, (p) => phases.push(p.phase))
    expect(phases).toEqual(['done'])
  })
})
