import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { makeMicPermission } from './micPermission'

const { getMediaAccessStatus, askForMediaAccess, openExternal } = vi.hoisted(() => ({
  getMediaAccessStatus: vi.fn(),
  askForMediaAccess: vi.fn(),
  openExternal: vi.fn(),
}))

vi.mock('electron', () => ({
  shell: { openExternal },
  systemPreferences: { getMediaAccessStatus, askForMediaAccess },
}))

beforeEach(() => {
  getMediaAccessStatus.mockReset()
  askForMediaAccess.mockReset()
  openExternal.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('makeMicPermission', () => {
  it('uses macOS media access APIs and opens System Settings on darwin', async () => {
    getMediaAccessStatus.mockReturnValueOnce('not-determined').mockReturnValueOnce('granted')
    askForMediaAccess.mockResolvedValue(true)

    const mic = makeMicPermission({
      platform: 'darwin',
      systemPreferences: { getMediaAccessStatus, askForMediaAccess },
      shell: { openExternal },
    })

    expect(mic.status()).toBe('not-determined')
    expect(await mic.request()).toBe('granted')
    expect(askForMediaAccess).toHaveBeenCalledWith('microphone')
    expect(getMediaAccessStatus).toHaveBeenCalledWith('microphone')
    expect(openExternal).not.toHaveBeenCalled()

    mic.openSettings()
    expect(openExternal).toHaveBeenCalledWith(
      'x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone',
    )
  })

  it('uses the Windows status API, never asks for media access, and opens Windows settings on win32', async () => {
    getMediaAccessStatus.mockReturnValue('denied')

    const mic = makeMicPermission({
      platform: 'win32',
      systemPreferences: { getMediaAccessStatus, askForMediaAccess },
      shell: { openExternal },
    })

    expect(mic.status()).toBe('denied')
    expect(await mic.request()).toBe('denied')
    expect(getMediaAccessStatus).toHaveBeenCalledWith('microphone')
    expect(askForMediaAccess).not.toHaveBeenCalled()

    mic.openSettings()
    expect(openExternal).toHaveBeenCalledWith('ms-settings:privacy-microphone')
  })

  it('treats linux as granted without touching OS mic permission APIs', async () => {
    const mic = makeMicPermission({
      platform: 'linux',
      systemPreferences: { getMediaAccessStatus, askForMediaAccess },
      shell: { openExternal },
    })

    expect(mic.status()).toBe('granted')
    expect(await mic.request()).toBe('granted')
    expect(getMediaAccessStatus).not.toHaveBeenCalled()
    expect(askForMediaAccess).not.toHaveBeenCalled()

    mic.openSettings()
    expect(openExternal).not.toHaveBeenCalled()
  })
})
