import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { makeSystemAudioPermission } from './systemAudioPermission'

const { openExternal } = vi.hoisted(() => ({
  openExternal: vi.fn(),
}))

vi.mock('electron', () => ({
  shell: { openExternal },
}))

beforeEach(() => {
  openExternal.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('makeSystemAudioPermission', () => {
  it('returns unknown on darwin and does not deep-link when not denied', async () => {
    const systemAudio = makeSystemAudioPermission({
      platform: 'darwin',
      shell: { openExternal },
    })

    expect(systemAudio.status()).toBe('unknown')
    expect(await systemAudio.request()).toBe('unknown')
    expect(openExternal).not.toHaveBeenCalled()

    systemAudio.openSettings()
    expect(openExternal).toHaveBeenCalledWith(
      'x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture',
    )
  })

  it.each([
    ['win32' as const],
    ['linux' as const],
  ])('treats %s as granted without touching Electron APIs', async (platform) => {
    const systemAudio = makeSystemAudioPermission({
      platform,
      shell: { openExternal },
    })

    expect(systemAudio.status()).toBe('granted')
    expect(await systemAudio.request()).toBe('granted')
    expect(openExternal).not.toHaveBeenCalled()
  })
})
