import { describe, expect, it } from 'vitest'
import { MuesliClient, type FetchLike } from './muesliClient'
import type { Plugin } from '../shared/types'

function fixture() {
  const batch: Plugin = {
    id: 'batch',
    kind: 'transcriber',
    name: 'Embedded whisper.cpp transcriber',
    endpoint_url: 'http://127.0.0.1:8001',
    enabled: true,
    is_default: true,
  }
  const live: Plugin = {
    id: 'live',
    kind: 'streaming-transcriber',
    name: 'Embedded whisper.cpp streaming transcriber',
    endpoint_url: 'http://127.0.0.1:8002',
    enabled: true,
    is_default: true,
  }
  const alternative: Plugin = {
    id: 'other',
    kind: 'transcriber',
    name: 'Alternative',
    endpoint_url: 'http://other:9000',
    enabled: true,
    is_default: false,
  }
  const plugins = [batch, live, alternative]
  // Match admin_plugins.go's actual response contract: POST returns {id},
  // PATCH returns {status:"ok"}; only GET returns complete plugin records.
  const fetch: FetchLike = async (input, init) => {
    const path = new URL(String(input)).pathname
    const method = init?.method ?? 'GET'
    if (path === '/api/transcription/services' && method === 'GET') return Response.json(plugins)
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    if (path === '/api/admin/plugins' && method === 'POST') {
      if (body.is_default)
        plugins
          .filter((p) => p.kind === body.kind)
          .forEach((p) => {
            p.is_default = false
          })
      const created = { ...body, id: 'custom' } as Plugin
      plugins.push(created)
      return Response.json({ id: created.id }, { status: 201 })
    }
    const existing = plugins.find((p) => path === `/api/admin/plugins/${p.id}`)
    if (existing && method === 'PATCH') {
      if (body.is_default)
        plugins
          .filter((p) => p.kind === existing.kind)
          .forEach((p) => {
            p.is_default = false
          })
      Object.assign(existing, body)
      return Response.json({ status: 'ok' })
    }
    return new Response(`Unexpected request: ${method} ${path}`, { status: 500 })
  }
  return {
    client: new MuesliClient({ baseUrl: 'http://muesli', token: 'test-token', fetch }),
    plugins,
    batch,
    live,
    alternative,
  }
}

describe('transcription selection client', () => {
  it('selects one final transcriber without changing the live choice', async () => {
    const { client, batch, live, alternative } = fixture()
    await client.selectTranscriptionPlugin({ kind: 'transcriber', pluginId: 'other' })
    expect(alternative.is_default).toBe(true)
    expect(batch.is_default).toBe(false)
    expect(live.is_default).toBe(true)
  })
  it('turns live transcription off without deleting its saved service', async () => {
    const { client, plugins, live } = fixture()
    await client.clearStreamingTranscriber()
    expect(plugins).toHaveLength(3)
    expect(live).toMatchObject({ enabled: false, is_default: true })
    await client.selectTranscriptionPlugin({ kind: 'streaming-transcriber', pluginId: 'live' })
    expect(live).toMatchObject({ enabled: true, is_default: true })
  })
  it('rejects a choice from the wrong kind and a missing final transcriber', async () => {
    const { client, batch } = fixture()
    await expect(
      client.selectTranscriptionPlugin({ kind: 'transcriber', pluginId: 'live' })
    ).rejects.toThrow(/not available/i)
    await expect(
      client.selectTranscriptionPlugin({ kind: 'transcriber', pluginId: null })
    ).rejects.toThrow(/choose a final/i)
    expect(batch.is_default).toBe(true)
  })
  it('adds a custom service without overwriting bundled endpoints and returns the server record', async () => {
    const { client, live } = fixture()
    const saved = await client.setStreamingTranscriber({
      url: 'http://custom:9000',
      token: 'custom-token',
    })
    expect(saved).toMatchObject({
      id: 'custom',
      endpoint_url: 'http://custom:9000',
      kind: 'streaming-transcriber',
      is_default: true,
    })
    expect(live.endpoint_url).toBe('http://127.0.0.1:8002')
    const updated = await client.setStreamingTranscriber({ url: 'http://custom:9001', token: '' })
    expect(updated.endpoint_url).toBe('http://custom:9001')
  })
})
