import { describe, expect, it } from 'vitest'
import { parseChatError } from './chatErrors'

describe('parseChatError', () => {
  it('classifies 422 no-agent errors with actionable copy', () => {
    expect(parseChatError(new Error('[422] no default agent configured'))).toEqual({
      kind: 'no-agent',
      message: 'AI features need an agent plugin configured. Install Ollama, or ask your administrator to configure one.',
    })
  })

  it('keeps 409 in-flight errors distinct', () => {
    expect(parseChatError(new Error('[409] message send already in progress'))).toEqual({
      kind: 'inflight',
      message: 'A message is already sending, please wait…',
    })
  })

  it('falls back to generic for real 500 errors', () => {
    expect(parseChatError(new Error('[500] internal error'))).toEqual({
      kind: 'generic',
      message: 'internal error',
    })
  })
})
