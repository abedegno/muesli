import { shell, systemPreferences } from 'electron'

export type SysAudioStatus = 'granted' | 'denied' | 'not-determined' | 'restricted' | 'unknown'

export interface SystemAudioPermissionDeps {
  platform?: NodeJS.Platform
  systemPreferences?: Pick<Electron.SystemPreferences, 'getMediaAccessStatus'>
  shell?: Pick<Electron.Shell, 'openExternal'>
}

export function makeSystemAudioPermission(deps: SystemAudioPermissionDeps = {}) {
  const platform = deps.platform ?? process.platform
  const sp = deps.systemPreferences ?? systemPreferences
  const sh = deps.shell ?? shell
  const api = {
    status(): SysAudioStatus {
      if (platform === 'darwin') return sp.getMediaAccessStatus('screen') as SysAudioStatus
      return 'granted' // win32 loopback + linux monitor need no TCC grant
    },
    async request(): Promise<SysAudioStatus> {
      if (platform === 'darwin' && api.status() !== 'granted') api.openSettings()
      return api.status()
    },
    openSettings(): void {
      if (platform === 'darwin') void sh.openExternal('x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture')
    },
  }
  return api
}
