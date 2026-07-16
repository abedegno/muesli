import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { makeSystemAudioPermission } from './systemAudioPermission'

const { getMediaAccessStatus, openExternal } = vi.hoisted(() => ({
  getMediaAccessStatus: vi.fn(),
  openExternal: vi.fn(),
}))

vi.mock('electron', () => ({
  shell: { openExternal },
  systemPreferences: { getMediaAccessStatus },
}))

beforeEach(() => {
  getMediaAccessStatus.mockReset()
  openExternal.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('makeSystemAudioPermission', () => {
  it('uses macOS media access APIs and opens System Settings on darwin', async () => {
    getMediaAccessStatus
      .mockReturnValueOnce('not-determined')
      .mockReturnValueOnce('not-determined')
      .mockReturnValue('granted')

    const systemAudio = makeSystemAudioPermission({
      platform: 'darwin',
      systemPreferences: { getMediaAccessStatus },
      shell: { openExternal },
    })

    expect(systemAudio.status()).toBe('not-determined')
    expect(await systemAudio.request()).toBe('granted')
    expect(getMediaAccessStatus).toHaveBeenCalledWith('screen')
    expect(openExternal).toHaveBeenCalledWith(
      'x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture',
    )

    systemAudio.openSettings()
    expect(openExternal).toHaveBeenCalledWith(
      'x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture',
    )
  })

  it('does not call askForMediaAccess on darwin request', async () => {
    const askForMediaAccess = vi.fn()
    getMediaAccessStatus.mockReturnValue('denied')

    const systemAudio = makeSystemAudioPermission({
      platform: 'darwin',
      systemPreferences: {
        getMediaAccessStatus,
      } as unknown as Pick<Electron.SystemPreferences, 'getMediaAccessStatus'> & {
        askForMediaAccess: typeof askForMediaAccess
      },
      shell: { openExternal },
    })

    expect(await systemAudio.request()).toBe('denied')
    expect(askForMediaAccess).not.toHaveBeenCalled()
  })

  it.each([
    ['win32' as const],
    ['linux' as const],
  ])('treats %s as granted without touching Electron APIs', async (platform) => {
    const systemAudio = makeSystemAudioPermission({
      platform,
      systemPreferences: { getMediaAccessStatus },
      shell: { openExternal },
    })

    expect(systemAudio.status()).toBe('granted')
    expect(await systemAudio.request()).toBe('granted')
    expect(getMediaAccessStatus).not.toHaveBeenCalled()
    expect(openExternal).not.toHaveBeenCalled()
  })
})
