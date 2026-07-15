import { shell, systemPreferences } from 'electron'

export type MicStatus = 'granted' | 'denied' | 'not-determined' | 'restricted' | 'unknown'

export interface MicPermissionDeps {
  platform?: NodeJS.Platform
  systemPreferences?: Pick<Electron.SystemPreferences, 'getMediaAccessStatus' | 'askForMediaAccess'>
  shell?: Pick<Electron.Shell, 'openExternal'>
}

export function makeMicPermission(deps: MicPermissionDeps = {}) {
  const platform = deps.platform ?? process.platform
  const sp = deps.systemPreferences ?? systemPreferences
  const sh = deps.shell ?? shell
  const api = {
    status(): MicStatus {
      if (platform === 'darwin' || platform === 'win32') {
        return sp.getMediaAccessStatus('microphone') as MicStatus
      }
      return 'granted'
    },
    async request(): Promise<MicStatus> {
      if (platform === 'darwin') {
        await sp.askForMediaAccess('microphone')
        return api.status()
      }
      return api.status()
    },
    openSettings(): void {
      if (platform === 'darwin') void sh.openExternal('x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone')
      else if (platform === 'win32') void sh.openExternal('ms-settings:privacy-microphone')
    },
  }
  return api
}
