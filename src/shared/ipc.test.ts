import { describe, expect, it } from 'vitest'
import { IPC } from './ipc'

describe('IPC channels', () => {
  it('maps every key to the expected muesli:camelCase channel name', () => {
    const entries = Object.entries(IPC)

    expect(IPC.listNotes).toBe('muesli:listNotes')
    expect(IPC.getConfig).toBe('muesli:getConfig')
    expect(IPC.openMicrosoftCalendarOAuthStart).toBe('muesli:openMicrosoftCalendarOAuthStart')

    for (const [key, value] of entries) {
      expect(value).toBe(`muesli:${key}`)
      expect(value.startsWith('muesli:')).toBe(true)
    }
  })

  it('uses unique channel names across the whole surface', () => {
    const values = Object.values(IPC)
    expect(new Set(values).size).toBe(values.length)
  })
})
