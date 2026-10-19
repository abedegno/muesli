import type { CalendarReadiness } from '../shared/types'
import type { ServiceReadinessSnapshot } from '../shared/readiness'
import { recordingScope, type RecordingJournal } from './recordingJournal'
import type { RecordingRecoveryStore } from './recordingRecovery'
import type { RecordingRecoveryInfo } from '../shared/types'
import { createHash, randomUUID } from 'node:crypto'
import { writeFile } from 'node:fs/promises'
import JSZip from 'jszip'
import { fullNoteToMarkdown } from '../renderer/lib/noteMarkdown'
import type {
  ActionItem,
  AudioUrlGrant,
  CalendarEvent,
  CompanyWithCount,
  CompanyWithPeople,
  Conversation,
  CreateShareRequest,
  CreateShareResponse,
  DigestConfig,
  DiarizationReview,
  Folder,
  FullNote,
  GoogleOAuthStatus,
  InsightsResponse,
  Message,
  MicrosoftOAuthStatus,
  Note,
  NoteLink,
  NoteLinksResponse,
  PersonWithCompany,
  Plugin,
  PluginHealth,
  PluginStatus,
  RelatedNote,
  RetranscribeNoteRequest,
  RetranscribeNoteResponse,
  RuleGroup,
  SearchResult,
  ServerConfig,
  Share,
  SmartList,
  SpeakerAlias,
  Template,
  TemplateSection,
} from '../shared/types'
import type {
  ConnectRequest,
  CreateConversationRequest,
  CreateConversationResponse,
  DiarizationReviewUpdate,
  ExportRequestOptions,
  ListNoteActionItemsResponse,
  SearchOptions,
  SendMessageRequest,
  SendMessageResponse,
  UpdateActionItemRequest,
  UpdatePersonRequest,
  UploadAudioRequest,
} from '../shared/ipc'
import { INSECURE_CONNECTION_CODE, isInsecureRemote } from '../shared/url'
import type { AuthInvalidatedNotice } from '../shared/ipc'
import type { UploadProgress } from './uploadMachine'
import { ApiError, MuesliClient, type FetchLike, type NoteExportData } from './muesliClient'
import type { SecretStore } from './secretStore'
import { ensureLocalSession } from './localSession'
import { uploadAudioToNote } from './uploadMachine'
import { TokenStore } from './tokenStore'

interface HandlerDeps {
  tokenStore: TokenStore
  recordingJournal?: RecordingJournal
  recordingRecovery?: RecordingRecoveryStore
  fetch?: FetchLike
  onProgress: (p: UploadProgress) => void
  onAuthInvalidated?: (notice: AuthInvalidatedNotice) => void
  openExternal?: (url: string) => Promise<void>
  embedded?: boolean
  embeddedBaseUrl?: string
  secretStore?: Pick<
    SecretStore,
    | 'loadCreds'
    | 'saveCreds'
    | 'clearCreds'
    | 'getManualServer'
    | 'setManualServer'
    | 'getOnboarded'
    | 'setOnboarded'
    | 'getKeepRunningInBackground'
    | 'setKeepRunningInBackground'
  >
  makeClient?: (
    baseUrl: string
  ) => Pick<MuesliClient, 'setupNeeded' | 'setup' | 'login' | 'createToken'>
  generatePassword?: () => string
  log?: (msg: string, err?: unknown) => void
}

interface Handlers {
  getConfig(): Promise<ServerConfig | null>
  getManualServer(): Promise<boolean>
  getOnboarded(): Promise<boolean>
  setOnboarded(onboarded: boolean): Promise<void>
  getKeepRunningInBackground(): Promise<boolean>
  setKeepRunningInBackground(keepRunningInBackground: boolean): Promise<void>
  getReadyz(): Promise<{ ollamaDetected: boolean } | null>
  getServerHealth(): Promise<{ reachable: boolean; authenticated: boolean; version?: string }>
  connect(req: ConnectRequest): Promise<{ serverUrl: string }>
  disconnect(): Promise<void>
  resetToBuiltIn(): Promise<void>
  listNotes(folderId?: string): Promise<Note[]>
  listPeople(): Promise<PersonWithCompany[]>
  listCompanies(): Promise<CompanyWithCount[]>
  getInsights(from?: string, to?: string): Promise<InsightsResponse>
  getServiceReadiness(): Promise<ServiceReadinessSnapshot>
  retryAIModels(): Promise<void>
  getCapabilities(): Promise<{ agentConfigured: boolean; isAdmin?: boolean; userId?: string }>
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
  startNoteCapture(id: string, captureId?: string): Promise<Note>
  stopNoteCapture(id: string, captureId?: string): Promise<Note>
  updateBody(id: string, content: string, connectionId?: string): Promise<void>
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
  beginRecordingJournal(noteId: string): Promise<string>
  appendRecordingJournal(captureId: string, frame: ArrayBuffer): Promise<void>
  endRecordingJournal(captureId: string): Promise<void>
  listRecordingRecoveries(): Promise<RecordingRecoveryInfo[]>
  recoveryAudio(noteId: string): Promise<{ bytes: Uint8Array; mimeType: string }>
  recoverNoteCapture(id: string): Promise<Note>
  saveRecordingRecovery(req: UploadAudioRequest): Promise<RecordingRecoveryInfo | null>
  getRecordingRecovery(noteId: string): Promise<RecordingRecoveryInfo | null>
  retryRecordingUpload(noteId: string): Promise<{ noteId: string }>
  discardRecordingRecovery(noteId: string): Promise<void>
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
  createTemplate(
    name: string,
    phase: Template['phase'],
    sections: TemplateSection[],
    autoRun: boolean
  ): Promise<Template>
  updateTemplate(
    id: string,
    name: string,
    phase: Template['phase'],
    sections: TemplateSection[],
    autoRun: boolean
  ): Promise<void>
  deleteTemplate(id: string): Promise<void>
  exportNote(
    noteId: string,
    format: string,
    options?: ExportRequestOptions
  ): Promise<NoteExportData>
  exportFolder(
    folderId: string,
    format: string,
    options?: ExportRequestOptions
  ): Promise<NoteExportData>
  retryNote(id: string): Promise<void>
  processNextNote(id: string): Promise<void>
  resummarize(id: string): Promise<void>
  regenerateSummary(noteId: string, templateId: string): Promise<void>
  search(q: string, opts?: SearchOptions): Promise<SearchResult>
  exportAllNotes(
    savePath: string
  ): Promise<{ success: true; path: string } | { success: false; error: string }>
  getDefaultTranscriberStatus(): Promise<PluginStatus>
  listPlugins(): Promise<Plugin[]>
  selectTranscriptionPlugin(req: {
    kind: 'transcriber' | 'streaming-transcriber'
    pluginId: string | null
  }): Promise<void>
  saveTranscriptionService(req: {
    kind: 'transcriber' | 'streaming-transcriber'
    url: string
    token: string
  }): Promise<Plugin>
  checkPluginHealth(id: string): Promise<PluginHealth>
  setStreamingTranscriber(req: { url: string; token: string }): Promise<Plugin>
  clearStreamingTranscriber(): Promise<void>
  checkAudioDedup(
    audio: ArrayBuffer
  ): Promise<{ existingNoteId?: string; existingNoteTitle?: string }>
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
  getCalendarReadiness(): Promise<CalendarReadiness>
  getCalendarEvents(from: string, to: string): Promise<CalendarEvent[]>
  getGoogleCalendarOAuthStatus(): Promise<GoogleOAuthStatus>
  openGoogleCalendarOAuthStart(): Promise<void>
  getMicrosoftCalendarOAuthStatus(): Promise<MicrosoftOAuthStatus>
  openMicrosoftCalendarOAuthStart(): Promise<void>
  getDigestConfig(): Promise<DigestConfig>
  updateDigestConfig(cadence: DigestConfig['cadence']): Promise<DigestConfig>
}

const AUTH_INVALIDATED_MESSAGE =
  'Your saved sign-in is no longer valid for this server. Sign in again to reconnect.'

class AuthInvalidatedError extends Error {
  readonly code = 'AUTH_INVALIDATED' as const

  constructor() {
    super(`[AUTH_INVALIDATED] ${AUTH_INVALIDATED_MESSAGE}`)
    this.name = 'AuthInvalidatedError'
  }
}

function makeStatusError(status: number, message: string): Error & { status: number } {
  const err = new Error(`[${status}] ${message}`) as Error & { status: number }
  err.status = status
  return err
}

function getErrorStatus(err: unknown): number | null {
  if (err instanceof ApiError) return err.status
  if (err instanceof Error && typeof (err as { status?: unknown }).status === 'number') {
    return (err as { status?: unknown }).status as number
  }
  return null
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
    if (err instanceof ApiError) throw makeStatusError(err.status, err.message)
    throw err
  }
}

function handleAuthedError(
  err: unknown,
  deps: Pick<HandlerDeps, 'tokenStore' | 'onAuthInvalidated'>
): never {
  if (err instanceof ApiError) {
    if (err.status === 401) {
      deps.tokenStore.clear()
      deps.onAuthInvalidated?.({ message: AUTH_INVALIDATED_MESSAGE })
      throw new AuthInvalidatedError()
    }
    throw makeStatusError(err.status, err.message)
  }
  throw err
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
  const { tokenStore, onProgress, openExternal } = deps
  const fetchImpl = deps.fetch ?? globalThis.fetch?.bind(globalThis)
  const secretStore =
    deps.secretStore ??
    ({
      loadCreds: () => null,
      saveCreds: () => {},
      clearCreds: () => {},
      getManualServer: () => false,
      setManualServer: () => {},
      getOnboarded: () => false,
      setOnboarded: () => {},
      getKeepRunningInBackground: () => false,
      setKeepRunningInBackground: () => {},
    } satisfies Pick<
      SecretStore,
      | 'loadCreds'
      | 'saveCreds'
      | 'clearCreds'
      | 'getManualServer'
      | 'setManualServer'
      | 'getOnboarded'
      | 'setOnboarded'
      | 'getKeepRunningInBackground'
      | 'setKeepRunningInBackground'
    >)
  const makeClient =
    deps.makeClient ?? ((baseUrl: string) => new MuesliClient({ baseUrl, fetch: fetchImpl }))
  const localSessionDeps = {
    embedded: deps.embedded ?? false,
    baseUrl: deps.embeddedBaseUrl ?? '',
    tokenStore: {
      load: () => tokenStore.load(),
      save: (config: { serverUrl: string; token: string }) => tokenStore.save(config),
    },
    secretStore: {
      loadCreds: () => secretStore.loadCreds(),
      saveCreds: (creds: { email: string; password: string }) => secretStore.saveCreds(creds),
      getManualServer: () => secretStore.getManualServer(),
      getOnboarded: () => secretStore.getOnboarded(),
    },
    makeClient,
    generatePassword: deps.generatePassword ?? (() => ''),
    log: deps.log,
  }

  // Build an authenticated client from persisted config, or throw if absent.
  function authedClient(): MuesliClient {
    const cfg = tokenStore.load()
    if (!cfg) throw new Error('not connected: no saved server/token')
    const client = new MuesliClient({ baseUrl: cfg.serverUrl, token: cfg.token, fetch: fetchImpl })
    return new Proxy(client, {
      get(target, prop, receiver) {
        const value = Reflect.get(target, prop, receiver)
        if (typeof value !== 'function') return value
        return (...args: unknown[]) => {
          try {
            const out = value.apply(target, args)
            return out && typeof (out as Promise<unknown>).then === 'function'
              ? (out as Promise<unknown>).catch((err) => handleAuthedError(err, deps))
              : out
          } catch (err) {
            return handleAuthedError(err, deps)
          }
        }
      },
    }) as MuesliClient
  }

  let connectionId = randomUUID()
  const captures = new Map<
    string,
    {
      config: ServerConfig
      client: MuesliClient
      noteId: string
      lastHeartbeat: number
      pending: boolean
    }
  >()
  function captureConfig(captureId?: string): ServerConfig | null {
    if (captureId) {
      const capture = captures.get(captureId)
      if (!capture)
        throw new Error(
          'Recording session is no longer available. Open the saved recording to retry.'
        )
      return capture.config
    }
    return recoveryConfig()
  }
  function recoveryConfig(): ServerConfig | null {
    const config = tokenStore.load()
    return config ? { ...config, manualServer: secretStore.getManualServer() } : null
  }

  return {
    async getConfig() {
      try {
        await ensureLocalSession(localSessionDeps)
      } catch (err) {
        deps.log?.('ensureLocalSession failed', err)
      }
      let cfg = tokenStore.load()
      if (!cfg) return null
      if (!cfg.userId) {
        try {
          const identity = await authedClient().getCapabilities()
          if (identity.userId) {
            await deps.recordingRecovery?.bindAccount(
              { ...cfg, manualServer: secretStore.getManualServer() },
              identity.userId
            )
            cfg = { ...cfg, userId: identity.userId }
            tokenStore.save(cfg)
          }
        } catch (error) {
          deps.log?.('Could not bind saved recordings to the account yet', error)
        }
      }
      return { ...cfg, manualServer: secretStore.getManualServer() }
    },

    async getManualServer() {
      return secretStore.getManualServer()
    },

    async getOnboarded() {
      return secretStore.getOnboarded()
    },

    async setOnboarded(onboarded) {
      secretStore.setOnboarded(onboarded)
    },

    async getKeepRunningInBackground() {
      return secretStore.getKeepRunningInBackground()
    },

    async setKeepRunningInBackground(keepRunningInBackground) {
      secretStore.setKeepRunningInBackground(keepRunningInBackground)
    },

    async getReadyz() {
      const manualServer = secretStore.getManualServer()
      const cfg = tokenStore.load()
      const baseUrl = manualServer ? cfg?.serverUrl?.trim() : deps.embeddedBaseUrl?.trim()
      if (!baseUrl || !fetchImpl) return null
      try {
        const headers = new Headers()
        if (cfg?.token) headers.set('Authorization', `Bearer ${cfg.token}`)
        const res = await fetchImpl(new URL('/readyz', baseUrl), { headers })
        if (!res.ok) return null
        const body = (await res.json()) as { embedded?: { ollamaDetected?: boolean } }
        return { ollamaDetected: body.embedded?.ollamaDetected ?? false }
      } catch {
        return null
      }
    },

    async getServerHealth() {
      const fetchFn = fetchImpl ?? globalThis.fetch?.bind(globalThis)
      const cfg = tokenStore.load()
      const serverUrl = cfg?.serverUrl?.trim()
      if (!serverUrl || !fetchFn) return { reachable: false, authenticated: false }
      try {
        const url = new URL('healthz', `${serverUrl.replace(/\/+$/, '')}/`)
        const res = await fetchFn(url)
        if (!res.ok) return { reachable: false, authenticated: false }
        const body = (await res.json()) as Record<string, unknown> | null
        if (!body || body.status !== 'ok') return { reachable: false, authenticated: false }
        const version = typeof body.version === 'string' ? body.version : undefined
        let authenticated = true
        try {
          await authedClient().getDigestConfig()
        } catch (err) {
          if (err instanceof AuthInvalidatedError) {
            authenticated = false
          }
        }
        return { reachable: true, authenticated, version }
      } catch {
        return { reachable: false, authenticated: false }
      }
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
      const identity = await new MuesliClient({
        baseUrl: req.serverUrl,
        token: appToken,
        fetch: fetchImpl,
      })
        .getCapabilities()
        .catch(() => null)
      const previous = tokenStore.load()
      const wasManual = secretStore.getManualServer()
      try {
        secretStore.setManualServer(true)
        tokenStore.save({
          serverUrl: req.serverUrl,
          token: appToken,
          ...(identity?.userId ? { userId: identity.userId } : {}),
        })
      } catch (error) {
        try {
          secretStore.setManualServer(wasManual)
          if (previous) tokenStore.save(previous)
          else tokenStore.clear()
        } catch (restoreError) {
          deps.log?.('Could not restore previous connection', restoreError)
        }
        throw error
      }
      connectionId = randomUUID()
      return { serverUrl: req.serverUrl }
    },

    async disconnect() {
      connectionId = randomUUID()
      tokenStore.clear()
      secretStore.setManualServer(true)
    },

    async resetToBuiltIn() {
      connectionId = randomUUID()
      tokenStore.clear()
      secretStore.setManualServer(false)
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

    async getServiceReadiness() {
      return authedClient().getServiceReadiness()
    },
    async retryAIModels() {
      await authedClient().retryAIModels()
    },
    async getCapabilities() {
      return authedClient().getCapabilities()
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

    async getCalendarReadiness() {
      return authedClient().getCalendarReadiness()
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
      const context = connectionId
      const full = await authedClient().getFull(id)
      return { ...full, connectionId: context }
    },

    async createNote(title) {
      return authedClient().createNote(title)
    },

    async stopNoteCapture(id, captureId) {
      return (
        captureId ? (captures.get(captureId)?.client ?? authedClient()) : authedClient()
      ).stopNoteCapture(id, captureId)
    },
    async startNoteCapture(id, captureId) {
      return (
        captureId ? (captures.get(captureId)?.client ?? authedClient()) : authedClient()
      ).startNoteCapture(id, captureId)
    },

    async updateBody(id, content, expectedConnection) {
      if (expectedConnection && expectedConnection !== connectionId)
        throw new Error(
          'The connection changed before these notes could be saved. Return to the original server to save them.'
        )
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
        const status = getErrorStatus(err)
        if (status === 404) return null
        if (status != null) {
          const message =
            err instanceof Error ? err.message.replace(/^\[\d+\]\s*/, '') : 'request failed'
          throw makeStatusError(status, message)
        }
        throw err
      }
    },

    async beginRecordingJournal(noteId) {
      const config = recoveryConfig()
      if (!config || !deps.recordingJournal) throw new Error('Recording recovery is unavailable.')
      if (captures.size >= 10)
        throw new Error('Too many pending recordings. Recover or discard them first.')
      const client = new MuesliClient({
        baseUrl: config.serverUrl,
        token: config.token,
        fetch: fetchImpl,
      })
      await client.getFull(noteId)
      if (
        (await deps.recordingRecovery?.info(config, noteId)) ||
        (await deps.recordingJournal.list(config)).some((item) => item.noteId === noteId)
      )
        throw new Error('Recover or discard the saved audio for this note before recording again.')
      const captureId = await deps.recordingJournal.begin(config, noteId)
      captures.set(captureId, { config, client, noteId, lastHeartbeat: Date.now(), pending: false })
      return captureId
    },
    async appendRecordingJournal(captureId, frame) {
      const capture = captures.get(captureId)
      if (!capture || !deps.recordingJournal)
        throw new Error('Recording session is no longer current.')
      await deps.recordingJournal.append(captureId, frame)
      if (
        capture.config.userId &&
        !capture.pending &&
        Date.now() - capture.lastHeartbeat >= 20_000
      ) {
        capture.pending = true
        capture.lastHeartbeat = Date.now()
        void capture.client
          .heartbeatCapture(capture.noteId, captureId)
          .catch((error) => deps.log?.('Recording heartbeat failed', error))
          .finally(() => {
            capture.pending = false
          })
      }
    },
    async endRecordingJournal(captureId) {
      if ((await deps.recordingJournal?.end(captureId)) === false) captures.delete(captureId)
    },
    async recoveryAudio(noteId) {
      const config = recoveryConfig()
      if (!config) throw new Error('Connect to the recording’s account first.')
      const full = await deps.recordingRecovery?.load(config, noteId)
      if (full?.audio) return { bytes: full.audio, mimeType: full.info.mimeType }
      const partial = await deps.recordingJournal?.recover(config, noteId)
      if (partial) return { bytes: partial.audio, mimeType: 'audio/wav' }
      throw new Error('No saved recording is available for this note.')
    },
    async listRecordingRecoveries() {
      const config = recoveryConfig()
      if (!config) return []
      const completed = (await deps.recordingRecovery?.list(config)) ?? []
      const partial = (await deps.recordingJournal?.list(config)) ?? []
      return [...completed, ...partial.filter((p) => !completed.some((c) => c.noteId === p.noteId))]
    },
    async recoverNoteCapture(id) {
      return authedClient().recoverNoteCapture(id)
    },
    async saveRecordingRecovery(req) {
      const config = captureConfig(req.captureId)
      if (!config || !deps.recordingRecovery || !req.audio) return null
      await deps.recordingRecovery.save(
        config,
        req.noteId,
        new Uint8Array(req.audio),
        req.audioMimeType ?? 'audio/webm',
        req.captureId
      )
      await deps.recordingJournal?.discard(config, req.noteId, req.captureId)
      return deps.recordingRecovery.info(config, req.noteId)
    },
    async getRecordingRecovery(noteId) {
      const config = recoveryConfig()
      if (!config) return null
      return (
        (await deps.recordingRecovery?.info(config, noteId)) ??
        (await deps.recordingJournal?.list(config))?.find((info) => info.noteId === noteId) ??
        null
      )
    },
    async discardRecordingRecovery(noteId) {
      const config = recoveryConfig()
      if (config) {
        await deps.recordingRecovery?.remove(config, noteId)
        await deps.recordingJournal?.discard(config, noteId)
        for (const [captureId, capture] of captures)
          if (
            capture.noteId === noteId &&
            recordingScope(capture.config) === recordingScope(config)
          )
            captures.delete(captureId)
      }
    },
    async retryRecordingUpload(noteId) {
      const config = recoveryConfig()
      if (!config || !deps.recordingRecovery)
        throw new Error('No saved recording is available for this connection.')
      let saved = await deps.recordingRecovery.load(config, noteId)
      const client = authedClient()
      if (!saved) {
        const partial = await deps.recordingJournal?.recover(config, noteId)
        if (partial) {
          const full = await client.getFull(noteId)
          if (full.note.status === 'recording')
            await client.stopNoteCapture(noteId, partial.captureId)
          await deps.recordingRecovery.save(
            config,
            noteId,
            partial.audio,
            'audio/wav',
            partial.captureId
          )
          await deps.recordingJournal?.discard(config, noteId, partial.captureId)
          saved = await deps.recordingRecovery.load(config, noteId)
        }
      }
      if (saved?.audio) {
        const full = await client.getFull(noteId)
        if (full.note.status === 'recording') {
          if (saved.info.captureId) await client.stopNoteCapture(noteId, saved.info.captureId)
          else await client.recoverNoteCapture(noteId)
        }
      }
      if (!saved?.audio) throw new Error('No saved recording is available for this note.')
      // If confirmation succeeded before its response was lost, do not enqueue
      // another transcription or overwrite the already-uploaded recording.
      try {
        const uploaded = await client.getNoteAudioUrl(noteId)
        if (!saved.info.uploadKey || uploaded.key !== saved.info.uploadKey)
          throw new Error(
            'This note already contains another recording. Your recovery audio has been kept; save an audio copy before choosing how to use it.'
          )
        await deps.recordingRecovery
          .remove(config, noteId, saved.info.revision)
          .catch((error) => deps.log?.('Could not remove uploaded recording backup', error))
        onProgress({ noteId, phase: 'done' })
        if (saved.info.captureId) captures.delete(saved.info.captureId)
        return { noteId }
      } catch (error) {
        if (getErrorStatus(error) !== 404) throw error
      }
      const result = await uploadAudioToNote(
        client,
        { noteId, audio: saved.audio, audioMimeType: saved.info.mimeType },
        onProgress,
        (key) => deps.recordingRecovery!.setUploadKey(config, noteId, saved!.info.revision, key)
      )
      if (saved.info.captureId) captures.delete(saved.info.captureId)
      await deps.recordingRecovery
        .remove(config, noteId, saved.info.revision)
        .catch((error) => deps.log?.('Could not remove uploaded recording backup', error))
      return result
    },
    async uploadAudio(req) {
      const config = captureConfig(req.captureId)
      const client =
        req.captureId && config
          ? new MuesliClient({ baseUrl: config.serverUrl, token: config.token, fetch: fetchImpl })
          : authedClient()
      const audio = req.audio ? new Uint8Array(req.audio) : null
      let revision: string | undefined
      if (audio?.length && config && deps.recordingRecovery) {
        revision = await deps.recordingRecovery.save(
          config,
          req.noteId,
          audio,
          req.audioMimeType ?? 'audio/webm',
          req.captureId
        )
        await deps.recordingJournal?.discard(config, req.noteId, req.captureId)
      }
      const result = await uploadAudioToNote(
        client,
        { noteId: req.noteId, audio, audioMimeType: req.audioMimeType },
        onProgress,
        async (key) => {
          if (config && revision)
            await deps.recordingRecovery?.setUploadKey(config, req.noteId, revision, key)
        }
      )
      if (req.captureId) captures.delete(req.captureId)
      if (config && revision)
        await deps.recordingRecovery
          ?.remove(config, req.noteId, revision)
          .catch((error) => deps.log?.('Could not remove uploaded recording backup', error))
      return result
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
    async createTemplate(name, phase, sections, autoRun) {
      return authedClient().createTemplate(name, phase, sections, autoRun)
    },
    async updateTemplate(id, name, phase, sections, autoRun) {
      await authedClient().updateTemplate(id, name, phase, sections, autoRun)
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

    async listPlugins() {
      return authedClient().listPlugins()
    },
    async selectTranscriptionPlugin(req) {
      await authedClient().selectTranscriptionPlugin(req)
    },
    async saveTranscriptionService(req) {
      return authedClient().saveTranscriptionService(req)
    },

    async checkPluginHealth(id) {
      return authedClient().checkPluginHealth(id)
    },

    async setStreamingTranscriber(req) {
      return authedClient().setStreamingTranscriber(req)
    },

    async clearStreamingTranscriber() {
      await authedClient().clearStreamingTranscriber()
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
