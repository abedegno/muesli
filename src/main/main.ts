import { basename, join } from 'node:path'
import { writeFile } from 'node:fs/promises'
import { app, BrowserWindow, dialog, ipcMain, safeStorage, shell } from 'electron'
import { IPC, type ConnectRequest, type CreateConversationRequest, type DiarizationReviewUpdate, type SearchOptions, type SendMessageRequest, type UploadAudioRequest } from '../shared/ipc'
import type { RetranscribeNoteRequest, RuleGroup, TemplateSection } from '../shared/types'
import type { UpdatePersonRequest } from '../shared/ipc'
import { createHandlers } from './ipcHandlers'
import { NoteStreamRelay } from './noteStreamRelay'
import { TokenStore } from './tokenStore'

let mainWindow: BrowserWindow | null = null

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

app.whenReady().then(() => {
  const tokenStore = new TokenStore(app.getPath('userData'), safeStorage)
  const noteStream = new NoteStreamRelay({
    getConfig: () => tokenStore.load(),
    emit: (event) => mainWindow?.webContents.send(IPC.noteStreamEvent, event),
  })
  const handlers = createHandlers({
    tokenStore,
    openExternal: async (url: string) => {
      await shell.openExternal(url)
    },
    onProgress: (p) => mainWindow?.webContents.send(IPC.uploadProgress, p),
  })

  ipcMain.handle(IPC.getConfig, () => handlers.getConfig())
  ipcMain.handle(IPC.connect, (_e, req: ConnectRequest) => handlers.connect(req))
  ipcMain.handle(IPC.disconnect, () => handlers.disconnect())
  ipcMain.handle(IPC.listNotes, (_e, folderId?: string) => handlers.listNotes(folderId))
  ipcMain.handle(IPC.listPeople, () => handlers.listPeople())
  ipcMain.handle(IPC.listCompanies, () => handlers.listCompanies())
  ipcMain.handle(IPC.getPerson, (_e, id: string) => handlers.getPerson(id))
  ipcMain.handle(IPC.getPersonNotes, (_e, id: string) => handlers.getPersonNotes(id))
  ipcMain.handle(IPC.updatePerson, (_e, id: string, req: UpdatePersonRequest) => handlers.updatePerson(id, req))
  ipcMain.handle(IPC.mergePeople, (_e, fromId: string, intoId: string) => handlers.mergePeople(fromId, intoId))
  ipcMain.handle(IPC.deletePerson, (_e, id: string) => handlers.deletePerson(id))
  ipcMain.handle(IPC.getCompany, (_e, id: string) => handlers.getCompany(id))
  ipcMain.handle(IPC.getCalendarEvents, (_e, from: string, to: string) => handlers.getCalendarEvents(from, to))
  ipcMain.handle(IPC.getGoogleCalendarOAuthStatus, () => handlers.getGoogleCalendarOAuthStatus())
  ipcMain.handle(IPC.openGoogleCalendarOAuthStart, () => handlers.openGoogleCalendarOAuthStart())
  ipcMain.handle(IPC.getMicrosoftCalendarOAuthStatus, () => handlers.getMicrosoftCalendarOAuthStatus())
  ipcMain.handle(IPC.openMicrosoftCalendarOAuthStart, () => handlers.openMicrosoftCalendarOAuthStart())
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
  ipcMain.handle(IPC.createTemplate, (_e, name: string, sections: TemplateSection[]) => handlers.createTemplate(name, sections))
  ipcMain.handle(IPC.updateTemplate, (_e, id: string, name: string, sections: TemplateSection[]) => handlers.updateTemplate(id, name, sections))
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

  ipcMain.handle(IPC.exportNote, async (_e, noteId: string, format: string) => {
    try {
      const exported = await handlers.exportNote(noteId, format)
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

  createWindow()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})
