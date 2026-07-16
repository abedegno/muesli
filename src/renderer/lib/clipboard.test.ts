// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { writeClipboardText } from './clipboard'

const writeClipboardTextMock = vi.fn()
vi.mock('@/api', () => ({
  muesli: { writeClipboardText: (text: string) => writeClipboardTextMock(text) },
}))

beforeEach(() => {
  writeClipboardTextMock.mockReset()
})

describe('writeClipboardText', () => {
  it('delegates to the muesli bridge (Electron main-process clipboard.writeText via IPC)', async () => {
    writeClipboardTextMock.mockResolvedValue(undefined)
    await writeClipboardText('hello world')
    expect(writeClipboardTextMock).toHaveBeenCalledWith('hello world')
  })

  it('throws when the bridge method is unavailable', async () => {
    const mod = await import('@/api')
    const saved = mod.muesli.writeClipboardText
    delete (mod.muesli as { writeClipboardText?: unknown }).writeClipboardText
    try {
      await expect(writeClipboardText('x')).rejects.toThrow('Clipboard is unavailable')
    } finally {
      mod.muesli.writeClipboardText = saved
    }
  })
})
