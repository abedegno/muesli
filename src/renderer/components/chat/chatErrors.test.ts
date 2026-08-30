// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { installContextBridgeLike } from '../../test-utils/bridge'
import { parseChatError } from './chatErrors'

async function loadApi() {
  vi.resetModules()
  return import('@/api')
}

async function bridgeChatError(status: number, message: string): Promise<unknown> {
  installContextBridgeLike({
    sendMessage: async () => {
      throw new Error(
        `Error invoking remote method 'muesli:sendMessage': Error: [${status}] ${message}`,
      )
    },
  })

  const { muesli, BridgeError } = await loadApi()
  try {
    await muesli.sendMessage('conversation-1', { content: 'hello' })
  } catch (err) {
    expect(err).toBeInstanceOf(BridgeError)
    return err
  }
  throw new Error('Expected bridge call to reject')
}

describe('parseChatError', () => {
  it('classifies 422 no-agent errors with actionable copy', async () => {
    const err = await bridgeChatError(422, 'no default agent configured')

    expect(parseChatError(err)).toEqual({
      kind: 'no-agent',
      message: 'Chat needs an agent plugin configured. Install Ollama, or ask your administrator to configure one.',
    })
  })

  it('keeps 409 in-flight errors distinct', async () => {
    const err = await bridgeChatError(409, 'message send already in progress')

    expect(parseChatError(err)).toEqual({
      kind: 'inflight',
      message: 'A message is already sending, please wait…',
    })
  })

  it('falls back to generic for real 500 errors', async () => {
    const err = await bridgeChatError(500, 'internal error')

    expect(parseChatError(err)).toEqual({
      kind: 'generic',
      message: 'internal error',
    })
  })
})
