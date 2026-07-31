import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => {
  const permissionHandler = vi.fn()
  const appHandlers = new Map<string, (...args: unknown[]) => void>()
  const browserWindow = {
    loadURL: vi.fn(),
    loadFile: vi.fn(),
    isDestroyed: vi.fn(() => false),
    isMinimized: vi.fn(() => false),
    restore: vi.fn(),
    show: vi.fn(),
    focus: vi.fn(),
    on: vi.fn(),
    once: vi.fn(),
    webContents: {
      send: vi.fn(),
    },
  }

  const browserWindowCtor = Object.assign(
    vi.fn().mockImplementation((options: unknown) => {
      ;(browserWindow as { constructorOptions?: unknown }).constructorOptions = options
      return browserWindow
    }),
    {
      getAllWindows: vi.fn(() => []),
    },
  )
  const notification = {
    on: vi.fn(),
    show: vi.fn(),
  }
  const tray = {
    setToolTip: vi.fn(),
    setContextMenu: vi.fn(),
    destroy: vi.fn(),
  }
  const nativeImage = {
    createFromPath: vi.fn(() => ({
      isEmpty: () => false,
      setTemplateImage: vi.fn(),
    })),
  }
  const menu = {
    buildFromTemplate: vi.fn((template: unknown) => template),
  }
  const trayCtor = vi.fn().mockImplementation(() => tray)

  const app = {
    isPackaged: false,
    resourcesPath: '/tmp/resources',
    getAppPath: vi.fn(() => '/tmp/app'),
    getPath: vi.fn((name: string) => `/tmp/${name}`),
    quit: vi.fn(),
    requestSingleInstanceLock: vi.fn(() => true),
    whenReady: vi.fn(() => Promise.resolve()),
    on: vi.fn((event: string, handler: (...args: unknown[]) => void) => {
      appHandlers.set(event, handler)
    }),
    emit: vi.fn(),
  }

  const ipcHandle = vi.fn()
  const secretState = {
    keepRunningInBackground: false,
  }
  const tokenStoreCtor = vi.fn().mockImplementation(() => ({
    load: vi.fn(),
    save: vi.fn(),
  }))
  const secretStoreCtor = vi.fn().mockImplementation(() => ({
    loadCreds: vi.fn(),
    saveCreds: vi.fn(),
    clearCreds: vi.fn(),
    getManualServer: vi.fn(() => false),
    setManualServer: vi.fn(),
    getOnboarded: vi.fn(() => false),
    setOnboarded: vi.fn(),
    getKeepRunningInBackground: vi.fn(() => secretState.keepRunningInBackground),
    setKeepRunningInBackground: vi.fn((next: boolean) => {
      secretState.keepRunningInBackground = next
    }),
  }))
  const makeServerLogPath = vi.fn((userDataPath: string) => `${userDataPath}/logs/server.log`)
  const startServerSupervisor = vi.fn(async (opts: { logPath: string }) => ({
    baseUrl: 'http://127.0.0.1:4567',
    logPath: opts.logPath,
    waitUntilHealthy: vi.fn(),
    shutdown: vi.fn(),
  }))
  const startEmbeddedStartupMonitor = vi.fn()
  const createHandlers = vi.fn(() => new Proxy({}, { get: () => vi.fn() }))
  const meetingDetectionManagerCtor = vi.fn().mockImplementation(() => ({
    start: vi.fn(),
    stop: vi.fn(),
    windowClosed: vi.fn(),
    rendererReadyForWindow: vi.fn(async () => {}),
    acceptPrompt: vi.fn(),
    dismissPrompt: vi.fn(),
  }))

  return {
    app,
    appHandlers,
    browserWindow,
    browserWindowCtor,
    createHandlers,
    ipcHandle,
    makeServerLogPath,
    permissionHandler,
    menu,
    notification,
    meetingDetectionManagerCtor,
    nativeImage,
    secretState,
    secretStoreCtor,
    trayCtor,
    tray,
    startEmbeddedStartupMonitor,
    startServerSupervisor,
    tokenStoreCtor,
  }
})

vi.mock('electron', () => ({
  app: mocks.app,
  BrowserWindow: mocks.browserWindowCtor,
  Notification: vi.fn().mockImplementation(() => mocks.notification),
  clipboard: {
    writeText: vi.fn(),
  },
  dialog: {
    showSaveDialog: vi.fn(),
  },
  ipcMain: {
    handle: mocks.ipcHandle,
  },
  Menu: mocks.menu,
  Tray: mocks.trayCtor,
  nativeImage: mocks.nativeImage,
  safeStorage: {
    isEncryptionAvailable: vi.fn(() => true),
    encryptString: vi.fn((value: string) => Buffer.from(value, 'utf8')),
    decryptString: vi.fn((value: Buffer) => value.toString('utf8')),
  },
  session: {
    defaultSession: {
      setPermissionRequestHandler: mocks.permissionHandler,
    },
  },
  shell: {
    openExternal: vi.fn(),
  },
  systemPreferences: {
    getMediaAccessStatus: vi.fn(),
    askForMediaAccess: vi.fn(),
  },
}))

vi.mock('./ipcHandlers', () => ({
  createHandlers: mocks.createHandlers,
}))

vi.mock('./serverSupervisor', () => ({
  DEFAULT_HEALTH_TIMEOUT_MS: 120_000,
  makeServerLogPath: mocks.makeServerLogPath,
  startServerSupervisor: mocks.startServerSupervisor,
}))

vi.mock('./meetingDetectionLoop', () => ({
  MeetingDetectionManager: mocks.meetingDetectionManagerCtor,
}))

vi.mock('./embeddedStartupMonitor', () => ({
  startEmbeddedStartupMonitor: mocks.startEmbeddedStartupMonitor,
}))

vi.mock('./tokenStore', () => ({
  TokenStore: mocks.tokenStoreCtor,
}))

vi.mock('./secretStore', () => ({
  SecretStore: mocks.secretStoreCtor,
}))

async function flushBoot() {
  await new Promise<void>((resolve) => setImmediate(resolve))
}

function emitAppEvent(event: string) {
  const handler = mocks.appHandlers.get(event)
  if (!handler) {
    throw new Error(`missing app handler for ${event}`)
  }
  handler()
}

describe('main bootstrap wiring', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
    mocks.appHandlers.clear()
    mocks.secretState.keepRunningInBackground = false
    mocks.app.getPath.mockImplementation((name: string) => `/tmp/${name}`)
    mocks.app.getAppPath.mockReturnValue('/tmp/app')
  })

  it('registers hardened window, permission, IPC, and userData wiring', async () => {
    mocks.app.getPath.mockImplementation((name: string) => `/sentinel/${name}/user-data`)

    await import('./main')
    await flushBoot()

    expect(mocks.app.whenReady).toHaveBeenCalledTimes(1)
    expect(mocks.app.requestSingleInstanceLock).toHaveBeenCalledTimes(1)

    expect(mocks.browserWindowCtor).toHaveBeenCalledTimes(1)
    expect(mocks.browserWindowCtor).toHaveBeenCalledWith(
      expect.objectContaining({
        webPreferences: expect.objectContaining({
          contextIsolation: true,
          nodeIntegration: false,
          sandbox: true,
          preload: expect.stringMatching(/preload[\\/]+preload\.js$/),
        }),
      }),
    )

    expect(mocks.permissionHandler).toHaveBeenCalledTimes(1)
    const permissionHandler = mocks.permissionHandler.mock.calls[0][0] as (
      wc: unknown,
      permission: string,
      cb: (allowed: boolean) => void,
    ) => void

    const grantedPermissions = ['media', 'microphone', 'audioCapture']
    for (const permission of grantedPermissions) {
      const cb = vi.fn()
      permissionHandler({}, permission, cb)
      expect(cb).toHaveBeenCalledWith(true)
    }

    const deniedCallback = vi.fn()
    permissionHandler({}, 'notifications', deniedCallback)
    expect(deniedCallback).toHaveBeenCalledWith(false)

    const registeredChannels = mocks.ipcHandle.mock.calls.map(([channel]) => channel)
    expect(registeredChannels).toEqual(expect.arrayContaining([
      'muesli:getConfig',
      'muesli:listNotes',
      'muesli:uploadAudio',
      'muesli:exportNote',
      'muesli:micStatus',
      'muesli:search',
      'muesli:getCalendarPrefs',
      'muesli:setCalendarPrefs',
      'muesli:getKeepRunningInBackground',
      'muesli:setKeepRunningInBackground',
      'muesli:meetingDetectionRendererReady',
      'muesli:meetingDetectionPromptAccept',
      'muesli:meetingDetectionPromptDismiss',
    ]))
    expect(new Set(registeredChannels).size).toBe(registeredChannels.length)

    expect(mocks.makeServerLogPath).toHaveBeenCalledWith('/sentinel/userData/user-data')
    expect(mocks.tokenStoreCtor).toHaveBeenCalledWith('/sentinel/userData/user-data', expect.any(Object))
    expect(mocks.secretStoreCtor).toHaveBeenCalledWith('/sentinel/userData/user-data', expect.any(Object))
    expect(mocks.startServerSupervisor).toHaveBeenCalledWith(expect.objectContaining({
      userDataPath: '/sentinel/userData/user-data',
      logPath: '/sentinel/userData/user-data/logs/server.log',
    }))
  })

  it('creates the tray from startup state, routes tray actions, and destroys it when disabled', async () => {
    mocks.secretState.keepRunningInBackground = true
    await import('./main')
    await flushBoot()

    expect(mocks.trayCtor).toHaveBeenCalledTimes(1)
    expect(mocks.nativeImage.createFromPath).toHaveBeenCalledWith('/tmp/app/build/tray-iconTemplate.png')
    expect(mocks.menu.buildFromTemplate).toHaveBeenCalledTimes(1)
    expect(mocks.tray.setContextMenu).toHaveBeenCalledWith(expect.arrayContaining([
      expect.objectContaining({ label: 'Open Muesli' }),
      expect.objectContaining({ label: 'New meeting' }),
      expect.objectContaining({ label: 'Settings' }),
      expect.objectContaining({ label: 'Quit Muesli' }),
    ]))

    const template = mocks.menu.buildFromTemplate.mock.calls[0][0] as Array<{
      label?: string
      click?: () => void
      type?: string
    }>
    const openItem = template.find((item) => item.label === 'Open Muesli')
    const newItem = template.find((item) => item.label === 'New meeting')
    const settingsItem = template.find((item) => item.label === 'Settings')
    const quitItem = template.find((item) => item.label === 'Quit Muesli')

    openItem?.click?.()
    expect(mocks.browserWindow.show).toHaveBeenCalled()
    expect(mocks.browserWindow.focus).toHaveBeenCalled()

    newItem?.click?.()
    expect(mocks.browserWindow.webContents.send).toHaveBeenCalledWith('muesli:trayNavigate', '/new')

    settingsItem?.click?.()
    expect(mocks.browserWindow.webContents.send).toHaveBeenCalledWith('muesli:trayNavigate', '/settings')

    quitItem?.click?.()
    expect(mocks.app.quit).toHaveBeenCalled()

    const setKeepRunningHandler = mocks.ipcHandle.mock.calls.find(([channel]) => channel === 'muesli:setKeepRunningInBackground')?.[1] as (
      _e: unknown,
      next: boolean,
    ) => void
    setKeepRunningHandler({}, false)
    expect(mocks.tray.destroy).toHaveBeenCalledTimes(1)
  })

  it('quits on window-all-closed on darwin when background running is disabled', async () => {
    const originalPlatform = process.platform
    Object.defineProperty(process, 'platform', { value: 'darwin' })
    try {
      mocks.secretState.keepRunningInBackground = false
      await import('./main')
      await flushBoot()

      emitAppEvent('window-all-closed')
      expect(mocks.app.quit).toHaveBeenCalledTimes(1)
    } finally {
      Object.defineProperty(process, 'platform', { value: originalPlatform })
    }
  })

  it('keeps the app alive on darwin when background running is enabled and the last window closes', async () => {
    const originalPlatform = process.platform
    Object.defineProperty(process, 'platform', { value: 'darwin' })
    try {
      mocks.secretState.keepRunningInBackground = true
      await import('./main')
      await flushBoot()

      emitAppEvent('window-all-closed')
      expect(mocks.app.quit).not.toHaveBeenCalled()
      expect(mocks.trayCtor).toHaveBeenCalledTimes(1)
    } finally {
      Object.defineProperty(process, 'platform', { value: originalPlatform })
    }
  })

  it('lets an explicit quit proceed on darwin even when background running is enabled', async () => {
    const originalPlatform = process.platform
    Object.defineProperty(process, 'platform', { value: 'darwin' })
    try {
      mocks.secretState.keepRunningInBackground = true
      await import('./main')
      await flushBoot()

      emitAppEvent('before-quit')
      emitAppEvent('window-all-closed')
      expect(mocks.app.quit).toHaveBeenCalledTimes(1)
      expect(mocks.tray.destroy).toHaveBeenCalledTimes(1)
    } finally {
      Object.defineProperty(process, 'platform', { value: originalPlatform })
    }
  })
})
