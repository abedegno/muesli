import { randomBytes } from 'node:crypto'
import { appendFileSync, mkdirSync } from 'node:fs'
import { basename, dirname, join } from 'node:path'
import { writeFile } from 'node:fs/promises'
import { app, BrowserWindow, clipboard, dialog, ipcMain, safeStorage, session, shell } from 'electron'
import { IPC, type ConnectRequest, type CreateConversationRequest, type DiarizationReviewUpdate, type ExportRequestOptions, type SearchOptions, type SendMessageRequest, type UpdateActionItemRequest, type UpdatePersonRequest, type UploadAudioRequest } from '../shared/ipc'
import type { CreateShareRequest, DigestConfig, EmbeddedStartupStatus, RetranscribeNoteRequest, RuleGroup, TemplatePhase, TemplateSection } from '../shared/types'
import { createHandlers } from './ipcHandlers'
import { makeMicPermission } from './micPermission'
import { resolveAudiotapBin } from './resourcePaths'
import { startEmbeddedStartupMonitor } from './embeddedStartupMonitor'
import { MuesliClient } from './muesliClient'
import { NoteStreamRelay } from './noteStreamRelay'
import { SecretStore } from './secretStore'
import { makeSystemAudioPermission } from './systemAudioPermission'
import { makeSystemAudioHelper } from './systemAudioHelper'
import { makeServerLogPath, startServerSupervisor } from './serverSupervisor'
import { TokenStore } from './tokenStore'

let mainWindow: BrowserWindow | null = null
let fatalShutdownRequested = false

function writeFatalMainProcessLog(kind: 'uncaughtException' | 'unhandledRejection', err: unknown) {
  const logPath = makeServerLogPath(app.getPath('userData'))
  const timestamp = new Date().toISOString()
  const message =
    err instanceof Error
      ? err.stack ?? err.message
      : typeof err === 'string'
        ? err
        : (() => {
            try {
              return JSON.stringify(err)
            } catch {
              return String(err)
            }
          })()
  const entry = `${timestamp} [main:${kind}] ${message}\n`

  try {
    mkdirSync(dirname(logPath), { recursive: true })
    appendFileSync(logPath, entry, 'utf8')
  } catch (writeErr) {
    console.error('[muesli-main fatal log]', writeErr)
  }

  console.error(`[muesli-main ${kind}]`, err)
}

function requestFatalMainProcessQuit(kind: 'uncaughtException' | 'unhandledRejection', err: unknown) {
  writeFatalMainProcessLog(kind, err)
  if (fatalShutdownRequested) return
  fatalShutdownRequested = true
  app.quit()
}

process.on('uncaughtException', (err) => {
  requestFatalMainProcessQuit('uncaughtException', err)
})

process.on('unhandledRejection', (reason) => {
  requestFatalMainProcessQuit('unhandledRejection', reason)
})

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1100,
    height: 760,
    minWidth: 400,
    webPreferences: {
      preload: join(__dirname, '../preload/preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      // The preload uses only ipcRenderer + contextBridge (no Node fs/path), so the
      // renderer can stay fully sandboxed. All privileged work lives in main.
      sandbox: true,
    },
  })

  if (process.env.ELECTRON_RENDERER_URL) {
    void mainWindow.loadURL(process.env.ELECTRON_RENDERER_URL)
  } else {
    void mainWindow.loadFile(join(__dirname, '../renderer/index.html'))
  }
}

function focusMainWindow() {
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore()
    mainWindow.show()
    mainWindow.focus()
    return
  }

  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow()
  }
}

app.whenReady().then(async () => {
  try {
    if (!app.requestSingleInstanceLock()) {
      app.quit()
      return
    }

    createWindow()
    session.defaultSession.setPermissionRequestHandler((_wc, permission, cb) => {
      const perm = permission as string
      cb(perm === 'media' || perm === 'microphone' || perm === 'audioCapture')
    })

    const supervisor = await startServerSupervisor({
      onSecondInstance: focusMainWindow,
      logPath: makeServerLogPath(app.getPath('userData')),
      waitForHealthy: false,
    })
    if (!supervisor) return
    process.env.MUESLI_SERVER_URL = supervisor.baseUrl

    const pushStartupStatus = (status: EmbeddedStartupStatus) => {
      mainWindow?.webContents.send(IPC.embeddedStartupStatus, status)
    }

    startEmbeddedStartupMonitor({
      supervisor,
      onStatus: pushStartupStatus,
    })

    const userDataDir = app.getPath('userData')
    const tokenStore = new TokenStore(userDataDir, safeStorage)
    const secretStore = new SecretStore(userDataDir, safeStorage)
    const fetchImpl = globalThis.fetch?.bind(globalThis)
    const micPermission = makeMicPermission()
    const systemAudioPermission = makeSystemAudioPermission()
    const systemAudioHelper = makeSystemAudioHelper({
      binPath: resolveAudiotapBin({
        isPackaged: app.isPackaged,
        resourcesPath: process.resourcesPath,
        env: process.env,
      }),
    })
    const noteStream = new NoteStreamRelay({
      getConfig: () => tokenStore.load(),
      emit: (event) => mainWindow?.webContents.send(IPC.noteStreamEvent, event),
    })
    const handlers = createHandlers({
      tokenStore,
      fetch: fetchImpl,
      embedded: true,
      embeddedBaseUrl: supervisor.baseUrl,
      secretStore,
      makeClient: (baseUrl: string) => new MuesliClient({ baseUrl, fetch: fetchImpl }),
      generatePassword: () => randomBytes(32).toString('base64url'),
      log: (msg: string, err?: unknown) => {
        console.warn(msg, err)
      },
      openExternal: async (url: string) => {
        await shell.openExternal(url)
      },
      onProgress: (p) => mainWindow?.webContents.send(IPC.uploadProgress, p),
    })

    ipcMain.handle(IPC.getConfig, () => handlers.getConfig())
    ipcMain.handle(IPC.getOnboarded, () => handlers.getOnboarded())
    ipcMain.handle(IPC.setOnboarded, (_e, onboarded: boolean) => handlers.setOnboarded(onboarded))
    ipcMain.handle(IPC.getReadyz, () => handlers.getReadyz())
    ipcMain.handle(IPC.getServerHealth, () => handlers.getServerHealth())
    ipcMain.handle(IPC.connect, (_e, req: ConnectRequest) => handlers.connect(req))
    ipcMain.handle(IPC.disconnect, () => handlers.disconnect())
    ipcMain.handle(IPC.resetToBuiltIn, () => handlers.resetToBuiltIn())
    ipcMain.handle(IPC.listNotes, (_e, folderId?: string) => handlers.listNotes(folderId))
    ipcMain.handle(IPC.listPeople, () => handlers.listPeople())
    ipcMain.handle(IPC.listCompanies, () => handlers.listCompanies())
    ipcMain.handle(IPC.getInsights, (_e, from?: string, to?: string) => handlers.getInsights(from, to))
    ipcMain.handle(IPC.listNoteActionItems, (_e, noteId: string) => handlers.listNoteActionItems(noteId))
    ipcMain.handle(IPC.listNoteLinks, (_e, id: string) => handlers.listNoteLinks(id))
    ipcMain.handle(IPC.listRelatedNotes, (_e, id: string) => handlers.listRelatedNotes(id))
    ipcMain.handle(IPC.addNoteLink, (_e, id: string, toNoteId: string) => handlers.addNoteLink(id, toNoteId))
    ipcMain.handle(IPC.listActionItems, (_e, status?: string) => handlers.listActionItems(status))
    ipcMain.handle(IPC.getPerson, (_e, id: string) => handlers.getPerson(id))
    ipcMain.handle(IPC.getPersonNotes, (_e, id: string) => handlers.getPersonNotes(id))
    ipcMain.handle(IPC.updatePerson, (_e, id: string, req: UpdatePersonRequest) => handlers.updatePerson(id, req))
    ipcMain.handle(IPC.updateActionItem, (_e, id: string, req: UpdateActionItemRequest) => handlers.updateActionItem(id, req))
    ipcMain.handle(IPC.mergePeople, (_e, fromId: string, intoId: string) => handlers.mergePeople(fromId, intoId))
    ipcMain.handle(IPC.deletePerson, (_e, id: string) => handlers.deletePerson(id))
    ipcMain.handle(IPC.getCompany, (_e, id: string) => handlers.getCompany(id))
    ipcMain.handle(IPC.getCalendarEvents, (_e, from: string, to: string) => handlers.getCalendarEvents(from, to))
    ipcMain.handle(IPC.getGoogleCalendarOAuthStatus, () => handlers.getGoogleCalendarOAuthStatus())
    ipcMain.handle(IPC.openGoogleCalendarOAuthStart, () => handlers.openGoogleCalendarOAuthStart())
    ipcMain.handle(IPC.getMicrosoftCalendarOAuthStatus, () => handlers.getMicrosoftCalendarOAuthStatus())
    ipcMain.handle(IPC.openMicrosoftCalendarOAuthStart, () => handlers.openMicrosoftCalendarOAuthStart())
    ipcMain.handle(IPC.micStatus, () => micPermission.status())
    ipcMain.handle(IPC.micRequest, () => micPermission.request())
    ipcMain.handle(IPC.micOpenSettings, () => micPermission.openSettings())
    ipcMain.handle(IPC.systemAudioStatus, () => systemAudioPermission.status())
    ipcMain.handle(IPC.systemAudioRequest, () => systemAudioPermission.request())
    ipcMain.handle(IPC.systemAudioOpenSettings, () => systemAudioPermission.openSettings())
    ipcMain.handle(IPC.systemAudioAvailable, () => systemAudioHelper.available())
    ipcMain.handle(IPC.systemAudioStart, () =>
      systemAudioHelper.start((chunk) => mainWindow?.webContents.send(IPC.systemAudioPcm, chunk)),
    )
    ipcMain.handle(IPC.systemAudioStop, () => systemAudioHelper.stop())
    ipcMain.handle(IPC.writeClipboardText, (_e, text: string) => {
      clipboard.writeText(text)
    })
    ipcMain.handle(IPC.getDigestConfig, () => handlers.getDigestConfig())
    ipcMain.handle(IPC.updateDigestConfig, (_e, cadence: DigestConfig['cadence']) => handlers.updateDigestConfig(cadence))
    ipcMain.handle(IPC.getFull, (_e, id: string) => handlers.getFull(id))
    ipcMain.handle(IPC.createNote, (_e, title: string) => handlers.createNote(title))
    ipcMain.handle(IPC.updateBody, (_e, id: string, content: string) => handlers.updateBody(id, content))
    ipcMain.handle(IPC.updateTitle, (_e, id: string, title: string) => handlers.updateTitle(id, title))
    ipcMain.handle(IPC.deleteNote, (_e, id: string) => handlers.deleteNote(id))
    ipcMain.handle(IPC.duplicateNote, (_e, id: string) => handlers.duplicateNote(id))
    ipcMain.handle(IPC.pinNote, (_e, id: string) => handlers.pinNote(id))
    ipcMain.handle(IPC.unpinNote, (_e, id: string) => handlers.unpinNote(id))
    ipcMain.handle(IPC.linkNoteEvent, (_e, id: string, eventId: string) => handlers.linkNoteEvent(id, eventId))
    ipcMain.handle(IPC.unlinkNoteEvent, (_e, id: string) => handlers.unlinkNoteEvent(id))
    ipcMain.handle(IPC.listTrash, () => handlers.listTrash())
    ipcMain.handle(IPC.restoreNote, (_e, id: string) => handlers.restoreNote(id))
    ipcMain.handle(IPC.retranscribeNote, (_e, id: string, options?: RetranscribeNoteRequest) => handlers.retranscribeNote(id, options))
    ipcMain.handle(IPC.createShare, (_e, noteId: string, options?: CreateShareRequest) => handlers.createShare(noteId, options))
    ipcMain.handle(IPC.listNoteShares, (_e, noteId: string) => handlers.listNoteShares(noteId))
    ipcMain.handle(IPC.revokeShare, (_e, token: string) => handlers.revokeShare(token))
    ipcMain.handle(IPC.permanentDeleteNote, (_e, id: string) => handlers.permanentDeleteNote(id))
    ipcMain.handle(IPC.getNoteAudioUrl, (_e, noteId: string) => handlers.getNoteAudioUrl(noteId))
    ipcMain.handle(IPC.uploadAudio, (_e, req: UploadAudioRequest) => handlers.uploadAudio(req))
    ipcMain.handle(IPC.startNoteStream, (_e, noteId: string) => noteStream.start(noteId))
    ipcMain.handle(IPC.stopNoteStream, (_e, noteId: string) => noteStream.stop(noteId))
    ipcMain.handle(IPC.sendNoteStreamAudio, (_e, noteId: string, audio: ArrayBuffer) => noteStream.sendAudio(noteId, audio))
    ipcMain.handle(IPC.addTag, (_e, noteId: string, name: string) => handlers.addTag(noteId, name))
    ipcMain.handle(IPC.removeTag, (_e, noteId: string, name: string) => handlers.removeTag(noteId, name))
    ipcMain.handle(IPC.renameTag, (_e, id: string, name: string) => handlers.renameTag(id, name))
    ipcMain.handle(IPC.deleteTag, (_e, id: string) => handlers.deleteTag(id))
    ipcMain.handle(IPC.listTags, () => handlers.listTags())
    ipcMain.handle(IPC.listSmartLists, () => handlers.listSmartLists())
    ipcMain.handle(IPC.createSmartList, (_e, name: string, rule: RuleGroup) => handlers.createSmartList(name, rule))
    ipcMain.handle(IPC.updateSmartList, (_e, id: string, name: string, rule: RuleGroup) => handlers.updateSmartList(id, name, rule))
    ipcMain.handle(IPC.deleteSmartList, (_e, id: string) => handlers.deleteSmartList(id))
    ipcMain.handle(IPC.listTrashedSmartLists, () => handlers.listTrashedSmartLists())
    ipcMain.handle(IPC.restoreSmartList, (_e, id: string) => handlers.restoreSmartList(id))
    ipcMain.handle(IPC.permanentDeleteSmartList, (_e, id: string) => handlers.permanentDeleteSmartList(id))
    ipcMain.handle(IPC.listFolders, () => handlers.listFolders())
    ipcMain.handle(IPC.createFolder, (_e, name: string, parentId?: string | null) => handlers.createFolder(name, parentId))
    ipcMain.handle(IPC.updateFolder, (_e, id: string, name: string, parentId?: string | null) => handlers.updateFolder(id, name, parentId))
    ipcMain.handle(IPC.deleteFolder, (_e, id: string) => handlers.deleteFolder(id))
    ipcMain.handle(IPC.reorderFolder, (_e, id: string, afterId: string | null) => handlers.reorderFolder(id, afterId))
    ipcMain.handle(IPC.reorderNoteInFolder, (_e, folderId: string, noteId: string, afterId: string | null) => handlers.reorderNoteInFolder(folderId, noteId, afterId))
    ipcMain.handle(IPC.listTrashedFolders, () => handlers.listTrashedFolders())
    ipcMain.handle(IPC.restoreFolder, (_e, id: string) => handlers.restoreFolder(id))
    ipcMain.handle(IPC.permanentDeleteFolder, (_e, id: string) => handlers.permanentDeleteFolder(id))
    ipcMain.handle(IPC.addNoteFolder, (_e, noteId: string, folderId: string) => handlers.addNoteFolder(noteId, folderId))
    ipcMain.handle(IPC.removeNoteFolder, (_e, noteId: string, folderId: string) => handlers.removeNoteFolder(noteId, folderId))
    ipcMain.handle(IPC.listTemplates, () => handlers.listTemplates())
    ipcMain.handle(IPC.createTemplate, (_e, name: string, phase: TemplatePhase, sections: TemplateSection[], autoRun: boolean) => handlers.createTemplate(name, phase, sections, autoRun))
    ipcMain.handle(IPC.updateTemplate, (_e, id: string, name: string, phase: TemplatePhase, sections: TemplateSection[], autoRun: boolean) => handlers.updateTemplate(id, name, phase, sections, autoRun))
    ipcMain.handle(IPC.deleteTemplate, (_e, id: string) => handlers.deleteTemplate(id))
    ipcMain.handle(IPC.retryNote, (_, id: string) => handlers.retryNote(id))
    ipcMain.handle(IPC.processNextNote, (_, id: string) => handlers.processNextNote(id))
    ipcMain.handle(IPC.getDefaultTranscriberStatus, () => handlers.getDefaultTranscriberStatus())
    ipcMain.handle(IPC.checkAudioDedup, (_e, audio: ArrayBuffer) => handlers.checkAudioDedup(audio))
    ipcMain.handle(IPC.listSpeakerAliases, (_e, noteId: string) => handlers.listSpeakerAliases(noteId))
    ipcMain.handle(IPC.upsertSpeakerAlias, (_e, noteId: string, label: string, aliasName: string) => handlers.upsertSpeakerAlias(noteId, label, aliasName))
    ipcMain.handle(IPC.getDiarizationReview, (_e, noteId: string) => handlers.getDiarizationReview(noteId))
    ipcMain.handle(IPC.postDiarizationReview, (_e, noteId: string, body: DiarizationReviewUpdate) => handlers.postDiarizationReview(noteId, body))
    ipcMain.handle(IPC.listConversations, (_e, noteId?: string) => handlers.listConversations(noteId))
    ipcMain.handle(IPC.createConversation, (_e, req: CreateConversationRequest) => handlers.createConversation(req))
    ipcMain.handle(IPC.getConversation, (_e, id: string) => handlers.getConversation(id))
    ipcMain.handle(IPC.deleteConversation, (_e, id: string) => handlers.deleteConversation(id))
    ipcMain.handle(IPC.listMessages, (_e, conversationId: string) => handlers.listMessages(conversationId))
    ipcMain.handle(IPC.sendMessage, (_e, conversationId: string, req: SendMessageRequest) => handlers.sendMessage(conversationId, req))
    ipcMain.handle(IPC.resummarize, (_e, id: string) => handlers.resummarize(id))
    ipcMain.handle(IPC.regenerateSummary, (_e, noteId: string, templateId: string) => handlers.regenerateSummary(noteId, templateId))
    ipcMain.handle(IPC.search, (_e, q: string, opts?: SearchOptions) => handlers.search(q, opts))
    ipcMain.handle(IPC.exportFile, async (_e, defaultName: string, content: string) => {
      const opts = {
        defaultPath: defaultName,
        filters: [{ name: 'Markdown', extensions: ['md'] }],
      }
      const res = mainWindow
        ? await dialog.showSaveDialog(mainWindow, opts)
        : await dialog.showSaveDialog(opts)
      if (res.canceled || !res.filePath) return null
      await writeFile(res.filePath, content, 'utf8')
      return res.filePath
    })

    ipcMain.handle(IPC.exportNote, async (_e, noteId: string, format: string, options?: ExportRequestOptions) => {
      try {
        const exported = await handlers.exportNote(noteId, format, options)
        const fallbackName = basename(`${noteId}.${format}`)
        const defaultPath = basename(exported.filename ?? fallbackName)
        const filterName = format === 'md' ? 'Markdown' : format.toUpperCase()
        const opts = {
          defaultPath,
          filters: [{ name: filterName, extensions: [format] }],
        }
        const res = mainWindow
          ? await dialog.showSaveDialog(mainWindow, opts)
          : await dialog.showSaveDialog(opts)
        if (res.canceled || !res.filePath) return { success: false as const, error: 'cancelled' }
        await writeFile(res.filePath, exported.bytes)
        return { success: true as const, path: res.filePath }
      } catch (err) {
        return { success: false as const, error: err instanceof Error ? err.message : String(err) }
      }
    })

    ipcMain.handle(IPC.exportFolder, async (_e, folderId: string, format: string, options?: ExportRequestOptions) => {
      try {
        const exported = await handlers.exportFolder(folderId, format, options)
        const fallbackName = basename(`${folderId}.zip`)
        const defaultPath = basename(exported.filename ?? fallbackName)
        const opts = {
          defaultPath,
          filters: [{ name: 'Zip archive', extensions: ['zip'] }],
        }
        const res = mainWindow
          ? await dialog.showSaveDialog(mainWindow, opts)
          : await dialog.showSaveDialog(opts)
        if (res.canceled || !res.filePath) return { success: false as const, error: 'cancelled' }
        await writeFile(res.filePath, exported.bytes)
        return { success: true as const, path: res.filePath }
      } catch (err) {
        return { success: false as const, error: err instanceof Error ? err.message : String(err) }
      }
    })

    ipcMain.handle(IPC.exportAllNotes, async () => {
      const today = new Date().toISOString().slice(0, 10)
      const defaultName = `muesli-export-${today}.zip`
      const opts = {
        defaultPath: defaultName,
        filters: [{ name: 'Zip archive', extensions: ['zip'] }],
      }
      const res = mainWindow
        ? await dialog.showSaveDialog(mainWindow, opts)
        : await dialog.showSaveDialog(opts)
      if (res.canceled || !res.filePath) return { success: false, error: 'cancelled' }
      return handlers.exportAllNotes(res.filePath)
    })

  } catch (err) {
    console.error('failed to start embedded supervisor', err)
    mainWindow?.webContents.send(IPC.embeddedStartupStatus, {
      status: 'error',
      message: err instanceof Error ? err.message : String(err),
      logPath: makeServerLogPath(app.getPath('userData')),
    })
  }
})

app.on('activate', () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow()
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})
