import { createHash } from 'node:crypto'
import { writeFile } from 'node:fs/promises'
import JSZip from 'jszip'
import { fullNoteToMarkdown } from '../renderer/lib/noteMarkdown'
import type { ActionItem, AudioUrlGrant, CalendarEvent, CompanyWithCount, CompanyWithPeople, Conversation, CreateShareRequest, CreateShareResponse, DigestConfig, DiarizationReview, Folder, FullNote, GoogleOAuthStatus, InsightsResponse, Message, MicrosoftOAuthStatus, Note, NoteLink, NoteLinksResponse, PersonWithCompany, PluginStatus, RelatedNote, RetranscribeNoteRequest, RetranscribeNoteResponse, RuleGroup, SearchMatch, ServerConfig, Share, SmartList, SpeakerAlias, Template, TemplateSection } from '../shared/types'
import type { ConnectRequest, CreateConversationRequest, CreateConversationResponse, DiarizationReviewUpdate, ExportRequestOptions, ListNoteActionItemsResponse, SearchOptions, SendMessageRequest, SendMessageResponse, UpdateActionItemRequest, UpdatePersonRequest, UploadAudioRequest } from '../shared/ipc'
import { INSECURE_CONNECTION_CODE, isInsecureRemote } from '../shared/url'
import type { UploadProgress } from './uploadMachine'
import { ApiError, MuesliClient, type FetchLike, type NoteExportData } from './muesliClient'
import { uploadAudioToNote } from './uploadMachine'
import { TokenStore } from './tokenStore'

interface HandlerDeps {
  tokenStore: TokenStore
  fetch?: FetchLike
  onProgress: (p: UploadProgress) => void
  openExternal?: (url: string) => Promise<void>
}

interface Handlers {
  getConfig(): Promise<ServerConfig | null>
  connect(req: ConnectRequest): Promise<{ serverUrl: string }>
  disconnect(): Promise<void>
  listNotes(folderId?: string): Promise<Note[]>
  listPeople(): Promise<PersonWithCompany[]>
  listCompanies(): Promise<CompanyWithCount[]>
  getInsights(from?: string, to?: string): Promise<InsightsResponse>
  listNoteActionItems(noteId: string): Promise<ListNoteActionItemsResponse>
  listNoteLinks(id: string): Promise<NoteLinksResponse>
  listRelatedNotes(id: string): Promise<RelatedNote[]>
  addNoteLink(id: string, toNoteId: string): Promise<NoteLink>
  listActionItems(status?: string): Promise<ActionItem[]>
  getPerson(id: string): Promise<PersonWithCompany>
  getPersonNotes(id: string): Promise<Note[]>
  updatePerson(id: string, req: UpdatePersonRequest): Promise<PersonWithCompany>
  updateActionItem(id: string, req: UpdateActionItemRequest): Promise<ActionItem>
  mergePeople(fromId: string, intoId: string): Promise<PersonWithCompany>
  deletePerson(id: string): Promise<void>
  getCompany(id: string): Promise<CompanyWithPeople>
  getFull(id: string): Promise<FullNote>
  createNote(title: string): Promise<Note>
  updateBody(id: string, content: string): Promise<void>
  updateTitle(id: string, title: string): Promise<void>
  deleteNote(id: string): Promise<void>
  duplicateNote(id: string): Promise<Note>
  pinNote(id: string): Promise<void>
  unpinNote(id: string): Promise<void>
  linkNoteEvent(id: string, eventId: string): Promise<void>
  unlinkNoteEvent(id: string): Promise<void>
  listTrash(): Promise<Note[]>
  restoreNote(id: string): Promise<void>
  retranscribeNote(id: string, options?: RetranscribeNoteRequest): Promise<RetranscribeNoteResponse>
  createShare(noteId: string, options?: CreateShareRequest): Promise<CreateShareResponse>
  listNoteShares(noteId: string): Promise<Share[]>
  revokeShare(token: string): Promise<void>
  permanentDeleteNote(id: string): Promise<void>
  getNoteAudioUrl(noteId: string): Promise<AudioUrlGrant | null>
  uploadAudio(req: UploadAudioRequest): Promise<{ noteId: string }>
  addTag(noteId: string, name: string): Promise<{ id: string; name: string }>
  removeTag(noteId: string, name: string): Promise<void>
  renameTag(id: string, name: string): Promise<{ id: string; name: string }>
  deleteTag(id: string): Promise<void>
  listTags(): Promise<{ id: string; name: string; count: number }[]>
  listSmartLists(): Promise<SmartList[]>
  createSmartList(name: string, rule: RuleGroup): Promise<SmartList>
  updateSmartList(id: string, name: string, rule: RuleGroup): Promise<void>
  deleteSmartList(id: string): Promise<void>
  listTrashedSmartLists(): Promise<SmartList[]>
  restoreSmartList(id: string): Promise<void>
  permanentDeleteSmartList(id: string): Promise<void>
  listFolders(): Promise<Folder[]>
  createFolder(name: string, parentId?: string | null): Promise<Folder>
  updateFolder(id: string, name: string, parentId?: string | null): Promise<Folder>
  deleteFolder(id: string): Promise<void>
  reorderFolder(id: string, afterId: string | null): Promise<void>
  reorderNoteInFolder(folderId: string, noteId: string, afterId: string | null): Promise<void>
  listTrashedFolders(): Promise<Folder[]>
  restoreFolder(id: string): Promise<void>
  permanentDeleteFolder(id: string): Promise<void>
  addNoteFolder(noteId: string, folderId: string): Promise<void>
  removeNoteFolder(noteId: string, folderId: string): Promise<void>
  listTemplates(): Promise<Template[]>
  createTemplate(name: string, phase: Template['phase'], sections: TemplateSection[]): Promise<Template>
  updateTemplate(id: string, name: string, phase: Template['phase'], sections: TemplateSection[]): Promise<void>
  deleteTemplate(id: string): Promise<void>
  exportNote(noteId: string, format: string, options?: ExportRequestOptions): Promise<NoteExportData>
  exportFolder(folderId: string, format: string, options?: ExportRequestOptions): Promise<NoteExportData>
  retryNote(id: string): Promise<void>
  processNextNote(id: string): Promise<void>
  resummarize(id: string): Promise<void>
  regenerateSummary(noteId: string, templateId: string): Promise<void>
  search(q: string, opts?: SearchOptions): Promise<SearchMatch[]>
  exportAllNotes(savePath: string): Promise<{ success: true; path: string } | { success: false; error: string }>
  getDefaultTranscriberStatus(): Promise<PluginStatus>
  checkAudioDedup(audio: ArrayBuffer): Promise<{ existingNoteId?: string; existingNoteTitle?: string }>
  listSpeakerAliases(noteId: string): Promise<SpeakerAlias[]>
  upsertSpeakerAlias(noteId: string, label: string, aliasName: string): Promise<SpeakerAlias>
  getDiarizationReview(noteId: string): Promise<DiarizationReview>
  postDiarizationReview(noteId: string, body: DiarizationReviewUpdate): Promise<DiarizationReview>
  listConversations(noteId?: string): Promise<Conversation[]>
  createConversation(req: CreateConversationRequest): Promise<CreateConversationResponse>
  getConversation(id: string): Promise<Conversation>
  deleteConversation(id: string): Promise<void>
  listMessages(conversationId: string): Promise<Message[]>
  sendMessage(conversationId: string, req: SendMessageRequest): Promise<SendMessageResponse>
  getCalendarEvents(from: string, to: string): Promise<CalendarEvent[]>
  getGoogleCalendarOAuthStatus(): Promise<GoogleOAuthStatus>
  openGoogleCalendarOAuthStart(): Promise<void>
  getMicrosoftCalendarOAuthStatus(): Promise<MicrosoftOAuthStatus>
  openMicrosoftCalendarOAuthStart(): Promise<void>
  getDigestConfig(): Promise<DigestConfig>
  updateDigestConfig(cadence: DigestConfig['cadence']): Promise<DigestConfig>
}

// Electron's ipcMain.handle rejection path only round-trips an Error's
// `message` (custom properties like ApiError.status are dropped), so we
// encode the HTTP status into the message as a `[NNN] ` prefix the renderer
// can parse — e.g. distinguishing a 409 send-in-progress from a 500 plugin
// failure (see chat/chatErrors.ts).
async function withApiError<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch (err) {
    if (err instanceof ApiError) throw new Error(`[${err.status}] ${err.message}`)
    throw err
  }
}

// insecureAllowedByEnv lets a developer bypass the HTTPS guardrail globally via
// MUESLI_ALLOW_INSECURE (the curl --insecure analog). Off unless explicitly set.
function insecureAllowedByEnv(): boolean {
  const v = (process.env.MUESLI_ALLOW_INSECURE ?? '').trim().toLowerCase()
  return v === '1' || v === 'true' || v === 'yes'
}

// createHandlers builds the IPC business logic with injected collaborators so it
// is unit-testable without Electron. main.ts adapts these to ipcMain.handle.
export function createHandlers(deps: HandlerDeps): Handlers {
  const { tokenStore, fetch: fetchImpl, onProgress, openExternal } = deps

  // Build an authenticated client from persisted config, or throw if absent.
  function authedClient(): MuesliClient {
    const cfg = tokenStore.load()
    if (!cfg) throw new Error('not connected: no saved server/token')
    return new MuesliClient({ baseUrl: cfg.serverUrl, token: cfg.token, fetch: fetchImpl })
  }

  return {
    async getConfig() {
      return tokenStore.load()
    },

    async connect(req) {
      // Refuse to send credentials/audio in the clear to a non-loopback server
      // unless the user explicitly opted in (checkbox) or a dev set the env flag.
      if (isInsecureRemote(req.serverUrl) && !req.allowInsecure && !insecureAllowedByEnv()) {
        throw new Error(INSECURE_CONNECTION_CODE)
      }
      const onboarding = new MuesliClient({ baseUrl: req.serverUrl, fetch: fetchImpl })
      if (req.isFirstRun) {
        await onboarding.setup(req.email, req.password)
      }
      const session = await onboarding.login(req.email, req.password)
      const appToken = await onboarding.createToken('muesli-desktop', session)
      tokenStore.save({ serverUrl: req.serverUrl, token: appToken })
      return { serverUrl: req.serverUrl }
    },

    async disconnect() {
      tokenStore.clear()
    },

    async listNotes(folderId) {
      return authedClient().listNotes(folderId)
    },

    async listPeople() {
      return authedClient().listPeople()
    },

    async listCompanies() {
      return authedClient().listCompanies()
    },

    async getInsights(from, to) {
      return authedClient().getInsights(from, to)
    },

    async listNoteActionItems(noteId) {
      return authedClient().listNoteActionItems(noteId)
    },

    async listNoteLinks(id) {
      return authedClient().listNoteLinks(id)
    },

    async listRelatedNotes(id) {
      return authedClient().listRelatedNotes(id)
    },

    async addNoteLink(id, toNoteId) {
      return withApiError(() => authedClient().addNoteLink(id, toNoteId))
    },

    async listActionItems(status) {
      return authedClient().listActionItems(status)
    },

    async getPerson(id) {
      return authedClient().getPerson(id)
    },

    async getPersonNotes(id) {
      return authedClient().getPersonNotes(id)
    },

    async updatePerson(id, req) {
      return withApiError(() => authedClient().updatePerson(id, req))
    },

    async updateActionItem(id, req) {
      return withApiError(() => authedClient().updateActionItem(id, req))
    },

    async mergePeople(fromId, intoId) {
      return withApiError(() => authedClient().mergePeople(fromId, intoId))
    },

    async deletePerson(id) {
      await withApiError(() => authedClient().deletePerson(id))
    },

    async getCompany(id) {
      return authedClient().getCompany(id)
    },

    async getCalendarEvents(from, to) {
      return authedClient().getCalendarEvents(from, to)
    },

    async getGoogleCalendarOAuthStatus() {
      return authedClient().getGoogleCalendarOAuthStatus()
    },

    async openGoogleCalendarOAuthStart() {
      const cfg = tokenStore.load()
      if (!cfg) throw new Error('not connected: no saved server/token')
      const url = new URL('/api/calendar/oauth/google/start', cfg.serverUrl)
      url.searchParams.set('token', cfg.token)
      if (!openExternal) throw new Error('openExternal unavailable')
      await openExternal(url.toString())
    },

    async getMicrosoftCalendarOAuthStatus() {
      return authedClient().getMicrosoftCalendarOAuthStatus()
    },

    async openMicrosoftCalendarOAuthStart() {
      const cfg = tokenStore.load()
      if (!cfg) throw new Error('not connected: no saved server/token')
      const url = new URL('/api/calendar/oauth/microsoft/start', cfg.serverUrl)
      url.searchParams.set('token', cfg.token)
      if (!openExternal) throw new Error('openExternal unavailable')
      await openExternal(url.toString())
    },

    async getDigestConfig() {
      return withApiError(() => authedClient().getDigestConfig())
    },

    async updateDigestConfig(cadence) {
      return withApiError(() => authedClient().updateDigestConfig(cadence))
    },

    async getFull(id) {
      return authedClient().getFull(id)
    },

    async createNote(title) {
      return authedClient().createNote(title)
    },

    async updateBody(id, content) {
      await authedClient().putBody(id, content)
    },

    async updateTitle(id, title) {
      await authedClient().updateTitle(id, title)
    },

    async deleteNote(id) {
      await authedClient().deleteNote(id)
    },

    async duplicateNote(id) {
      return authedClient().duplicateNote(id)
    },

    async pinNote(id) {
      await authedClient().pinNote(id)
    },

    async unpinNote(id) {
      await authedClient().unpinNote(id)
    },

    async linkNoteEvent(id, eventId) {
      await authedClient().linkNoteEvent(id, eventId)
    },

    async unlinkNoteEvent(id) {
      await authedClient().unlinkNoteEvent(id)
    },

    async listTrash() {
      return authedClient().listTrash()
    },
    async restoreNote(id) {
      await authedClient().restoreNote(id)
    },
    async retranscribeNote(id, options) {
      return authedClient().retranscribeNote(id, options)
    },
    async createShare(noteId, options) {
      return authedClient().createShare(noteId, options)
    },
    async listNoteShares(noteId) {
      return authedClient().listNoteShares(noteId)
    },
    async revokeShare(token) {
      await authedClient().revokeShare(token)
    },
    async permanentDeleteNote(id) {
      await authedClient().permanentDeleteNote(id)
    },

    async getNoteAudioUrl(noteId) {
      try {
        return await authedClient().getNoteAudioUrl(noteId)
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null
        if (err instanceof ApiError) throw new Error(`[${err.status}] ${err.message}`)
        throw err
      }
    },

    async uploadAudio(req) {
      const client = authedClient()
      const audio = req.audio ? new Uint8Array(req.audio) : null
      return uploadAudioToNote(
        client,
        { noteId: req.noteId, audio, audioMimeType: req.audioMimeType },
        onProgress,
      )
    },

    async addTag(noteId, name) {
      return authedClient().addTag(noteId, name)
    },

    async removeTag(noteId, name) {
      await authedClient().removeTag(noteId, name)
    },

    async renameTag(id, name) {
      return authedClient().renameTag(id, name)
    },

    async deleteTag(id) {
      await authedClient().deleteTag(id)
    },

    async listTags() {
      return authedClient().listTags()
    },

    async listSmartLists() {
      return authedClient().listSmartLists()
    },
    async createSmartList(name, rule) {
      return authedClient().createSmartList(name, rule)
    },
    async updateSmartList(id, name, rule) {
      await authedClient().updateSmartList(id, name, rule)
    },
    async deleteSmartList(id) {
      await authedClient().deleteSmartList(id)
    },
    async listTrashedSmartLists() {
      return authedClient().listTrashedSmartLists()
    },
    async restoreSmartList(id) {
      await authedClient().restoreSmartList(id)
    },
    async permanentDeleteSmartList(id) {
      await authedClient().permanentDeleteSmartList(id)
    },

    async listFolders() {
      return authedClient().listFolders()
    },
    async createFolder(name, parentId) {
      return authedClient().createFolder(name, parentId)
    },
    async updateFolder(id, name, parentId) {
      return authedClient().updateFolder(id, name, parentId)
    },
    async deleteFolder(id) {
      await authedClient().deleteFolder(id)
    },
    async reorderFolder(id, afterId) {
      await authedClient().reorderFolder(id, afterId)
    },
    async reorderNoteInFolder(folderId, noteId, afterId) {
      await authedClient().reorderNoteInFolder(folderId, noteId, afterId)
    },
    async listTrashedFolders() {
      return authedClient().listTrashedFolders()
    },
    async restoreFolder(id) {
      await authedClient().restoreFolder(id)
    },
    async permanentDeleteFolder(id) {
      await authedClient().permanentDeleteFolder(id)
    },
    async addNoteFolder(noteId, folderId) {
      await authedClient().addNoteFolder(noteId, folderId)
    },
    async removeNoteFolder(noteId, folderId) {
      await authedClient().removeNoteFolder(noteId, folderId)
    },

    async listTemplates() {
      return authedClient().listTemplates()
    },
    async createTemplate(name, phase, sections) {
      return authedClient().createTemplate(name, phase, sections)
    },
    async updateTemplate(id, name, phase, sections) {
      await authedClient().updateTemplate(id, name, phase, sections)
    },
    async deleteTemplate(id) {
      await authedClient().deleteTemplate(id)
    },

    async exportNote(noteId, format, options) {
      return authedClient().exportNote(noteId, format, options)
    },

    async exportFolder(folderId, format, options) {
      return authedClient().exportFolder(folderId, format, options)
    },

    async retryNote(id) {
      await authedClient().retryNote(id)
    },

    async processNextNote(id) {
      await authedClient().processNextNote(id)
    },

    async resummarize(id) {
      await authedClient().resummarize(id)
    },

    async regenerateSummary(noteId, templateId) {
      await authedClient().regenerateSummary(noteId, templateId)
    },

    async search(q, opts) {
      return authedClient().search(q, opts)
    },

    async getDefaultTranscriberStatus() {
      try {
        return await authedClient().getDefaultTranscriberStatus()
      } catch {
        return { status: 'unknown' as const }
      }
    },

    async checkAudioDedup(audio: ArrayBuffer) {
      try {
        const hash = createHash('sha256').update(Buffer.from(audio)).digest('hex')
        const result = await authedClient().checkAudioDedup(hash)
        if (result.matches.length > 0) {
          return {
            existingNoteId: result.matches[0].note_id,
            existingNoteTitle: result.matches[0].title,
          }
        }
        return {}
      } catch {
        // Network error or any other failure — fail open (never block the upload)
        return {}
      }
    },

    async listSpeakerAliases(noteId) {
      return authedClient().listSpeakerAliases(noteId)
    },

    async upsertSpeakerAlias(noteId, label, aliasName) {
      return authedClient().upsertSpeakerAlias(noteId, label, aliasName)
    },

    async getDiarizationReview(noteId) {
      return authedClient().getDiarizationReview(noteId)
    },

    async postDiarizationReview(noteId, body) {
      return authedClient().postDiarizationReview(noteId, {
        segment_id: body.segmentId,
        speaker: body.speaker,
        review_state: body.reviewState,
      })
    },

    async listConversations(noteId) {
      return withApiError(() => authedClient().listConversations(noteId))
    },

    async createConversation(req) {
      return withApiError(() => authedClient().createConversation(req))
    },

    async getConversation(id) {
      return withApiError(() => authedClient().getConversation(id))
    },

    async deleteConversation(id) {
      await withApiError(() => authedClient().deleteConversation(id))
    },

    async listMessages(conversationId) {
      return withApiError(() => authedClient().listMessages(conversationId))
    },

    async sendMessage(conversationId, req) {
      return withApiError(() => authedClient().sendMessage(conversationId, req))
    },

    async exportAllNotes(savePath) {
      try {
        const notes = await authedClient().listNotes()
        const zip = new JSZip()
        for (const note of notes) {
          const full = await authedClient().getFull(note.id)
          const md = fullNoteToMarkdown(full)
          const rawSlug = note.title
            .toLowerCase()
            .replace(/[^a-z0-9]+/g, '-')
            .replace(/^-|-$/g, '')
            .slice(0, 60)
          const slug = rawSlug || 'untitled'
          zip.file(`${note.id}-${slug}.md`, md)
        }
        const buf = await zip.generateAsync({ type: 'nodebuffer' })
        await writeFile(savePath, buf)
        return { success: true as const, path: savePath }
      } catch (err) {
        return { success: false as const, error: err instanceof Error ? err.message : String(err) }
      }
    },
  }
}
