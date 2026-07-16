import { shell } from 'electron'

export type SysAudioStatus = 'granted' | 'denied' | 'not-determined' | 'restricted' | 'unknown'

export interface SystemAudioPermissionDeps {
  platform?: NodeJS.Platform
  shell?: Pick<Electron.Shell, 'openExternal'>
}

export function makeSystemAudioPermission(deps: SystemAudioPermissionDeps = {}) {
  const platform = deps.platform ?? process.platform
  const sh = deps.shell ?? shell
  const api = {
    status(): SysAudioStatus {
      // The tap permission has no Electron query; do NOT report getMediaAccessStatus('screen')
      // (that is the unrelated Screen Recording permission). Confirmed in a signed build.
      if (platform === 'darwin') return 'unknown'
      return 'granted' // win32 loopback + linux monitor need no TCC grant
    },
    async request(): Promise<SysAudioStatus> {
      const s = api.status()
      if (platform === 'darwin' && (s === 'denied' || s === 'restricted')) api.openSettings()
      return s
    },
    openSettings(): void {
      if (platform === 'darwin') void sh.openExternal('x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture')
    },
  }
  return api
}
