// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'

import { installContextBridgeLike } from './bridge'

describe('installContextBridgeLike', () => {
  it('installs bridge members with readonly, non-configurable descriptors', () => {
    installContextBridgeLike({
      listNotes: () => 'notes',
    })

    const bridge = window.muesli as unknown as Record<string, unknown>
    const memberDescriptor = Object.getOwnPropertyDescriptor(bridge, 'listNotes')
    const windowDescriptor = Object.getOwnPropertyDescriptor(window, 'muesli')

    expect(memberDescriptor).toMatchObject({
      writable: false,
      configurable: false,
      enumerable: true,
    })
    expect(windowDescriptor).toMatchObject({
      writable: false,
      configurable: true,
      enumerable: true,
    })

    expect(() => {
      'use strict'
      ;(bridge as { listNotes: unknown }).listNotes = () => 'changed'
    }).toThrow(TypeError)

    expect(bridge.listNotes).toBeDefined()
    expect(bridge.listNotes).toBeInstanceOf(Function)
  })
})
