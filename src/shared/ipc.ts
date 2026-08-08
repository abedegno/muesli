import type { ActionItem, ActionItemStatus, AudioUrlGrant, CalendarEvent, ChatSource, CompanyWithCount, CompanyWithPeople, Conversation, CreateShareRequest, CreateShareResponse, Decision, DigestConfig, DiarizationReview, EmbeddedStartupStatus, Folder, FullNote, GoogleOAuthStatus, InsightsResponse, Message, MicrosoftOAuthStatus, Note, NoteLink, NoteLinksResponse, PersonWithCompany, Plugin, PluginHealth, PluginStatus, RelatedNote, RetranscribeNoteRequest, RetranscribeNoteResponse, RuleGroup, SearchMatch, ServerConfig, Share, SmartList, SpeakerAlias, Template, TemplateSection } from './types'
import type { UploadProgress } from '../main/uploadMachine'
import type { MicStatus } from '../main/micPermission'
import type { SystemAudioFormat } from '../main/systemAudioHelper'
import type { SysAudioStatus } from '../main/systemAudioPermission'
import type { ExportOptions } from './export'

/**
 * Canonical channel names shared by preload and main. Renderer calls are awaited
 * `ipcRenderer.invoke` requests registered in `src/main/main.ts`; server-facing
 * business handlers are the same-named methods returned by `createHandlers` in
 * `src/main/ipcHandlers.ts`. Event-only channels are fire-and-forget main-to-renderer
 * pushes and therefore have no request handler in `ipcHandlers.ts`.
 */
export const IPC = {
  getConfig: 'muesli:getConfig',
  authInvalidated: 'muesli:authInvalidated',
  getManualServer: 'muesli:getManualServer',
  getOnboarded: 'muesli:getOnboarded',
  setOnboarded: 'muesli:setOnboarded',
  getKeepRunningInBackground: 'muesli:getKeepRunningInBackground',
  setKeepRunningInBackground: 'muesli:setKeepRunningInBackground',
  getReadyz: 'muesli:getReadyz',
  getEmbeddedStartupStatus: 'muesli:getEmbeddedStartupStatus',
  getServerHealth: 'muesli:getServerHealth',
  connect: 'muesli:connect',
  disconnect: 'muesli:disconnect',
  resetToBuiltIn: 'muesli:resetToBuiltIn',
  listNotes: 'muesli:listNotes',
  listPeople: 'muesli:listPeople',
  listCompanies: 'muesli:listCompanies',
  getInsights: 'muesli:getInsights',
  listNoteActionItems: 'muesli:listNoteActionItems',
  listNoteLinks: 'muesli:listNoteLinks',
  listRelatedNotes: 'muesli:listRelatedNotes',
  addNoteLink: 'muesli:addNoteLink',
  listActionItems: 'muesli:listActionItems',
  getPerson: 'muesli:getPerson',
  getPersonNotes: 'muesli:getPersonNotes',
  updatePerson: 'muesli:updatePerson',
  updateActionItem: 'muesli:updateActionItem',
  mergePeople: 'muesli:mergePeople',
  deletePerson: 'muesli:deletePerson',
  getCompany: 'muesli:getCompany',
  getFull: 'muesli:getFull',
  createNote: 'muesli:createNote',
  updateBody: 'muesli:updateBody',
  updateTitle: 'muesli:updateTitle',
  deleteNote: 'muesli:deleteNote',
  duplicateNote: 'muesli:duplicateNote',
  pinNote: 'muesli:pinNote',
  unpinNote: 'muesli:unpinNote',
  linkNoteEvent: 'muesli:linkNoteEvent',
  unlinkNoteEvent: 'muesli:unlinkNoteEvent',
  listTrash: 'muesli:listTrash',
  restoreNote: 'muesli:restoreNote',
  retranscribeNote: 'muesli:retranscribeNote',
  createShare: 'muesli:createShare',
  listNoteShares: 'muesli:listNoteShares',
  revokeShare: 'muesli:revokeShare',
  permanentDeleteNote: 'muesli:permanentDeleteNote',
  getNoteAudioUrl: 'muesli:getNoteAudioUrl',
  uploadAudio: 'muesli:uploadAudio',
  uploadProgress: 'muesli:uploadProgress',
  embeddedStartupStatus: 'muesli:embeddedStartupStatus',
  trayNavigate: 'muesli:trayNavigate',
  startNoteStream: 'muesli:startNoteStream',
  stopNoteStream: 'muesli:stopNoteStream',
  sendNoteStreamAudio: 'muesli:sendNoteStreamAudio',
  noteStreamEvent: 'muesli:noteStreamEvent',
  addTag: 'muesli:addTag',
  removeTag: 'muesli:removeTag',
  renameTag: 'muesli:renameTag',
  deleteTag: 'muesli:deleteTag',
  listTags: 'muesli:listTags',
  listSmartLists: 'muesli:listSmartLists',
  createSmartList: 'muesli:createSmartList',
  updateSmartList: 'muesli:updateSmartList',
  deleteSmartList: 'muesli:deleteSmartList',
  listTrashedSmartLists: 'muesli:listTrashedSmartLists',
  restoreSmartList: 'muesli:restoreSmartList',
  permanentDeleteSmartList: 'muesli:permanentDeleteSmartList',
  listFolders: 'muesli:listFolders',
  createFolder: 'muesli:createFolder',
  updateFolder: 'muesli:updateFolder',
  deleteFolder: 'muesli:deleteFolder',
  reorderFolder: 'muesli:reorderFolder',
  reorderNoteInFolder: 'muesli:reorderNoteInFolder',
  listTrashedFolders: 'muesli:listTrashedFolders',
  restoreFolder: 'muesli:restoreFolder',
  permanentDeleteFolder: 'muesli:permanentDeleteFolder',
  addNoteFolder: 'muesli:addNoteFolder',
  removeNoteFolder: 'muesli:removeNoteFolder',
  listTemplates: 'muesli:listTemplates',
  createTemplate: 'muesli:createTemplate',
  updateTemplate: 'muesli:updateTemplate',
  deleteTemplate: 'muesli:deleteTemplate',
  exportFile: 'muesli:exportFile',
  exportNote: 'muesli:exportNote',
  exportFolder: 'muesli:exportFolder',
  exportAllNotes: 'muesli:exportAllNotes',
  resummarize: 'muesli:resummarize',
  regenerateSummary: 'muesli:regenerateSummary',
  retryNote: 'muesli:retryNote',
  processNextNote: 'muesli:processNextNote',
  search: 'muesli:search',
  getDefaultTranscriberStatus: 'muesli:getDefaultTranscriberStatus',
  listPlugins: 'muesli:listPlugins',
  checkPluginHealth: 'muesli:checkPluginHealth',
  setStreamingTranscriber: 'muesli:setStreamingTranscriber',
  clearStreamingTranscriber: 'muesli:clearStreamingTranscriber',
  checkAudioDedup: 'muesli:checkAudioDedup',
  listSpeakerAliases: 'muesli:listSpeakerAliases',
  upsertSpeakerAlias: 'muesli:upsertSpeakerAlias',
  getDiarizationReview: 'muesli:getDiarizationReview',
  postDiarizationReview: 'muesli:postDiarizationReview',
  listConversations: 'muesli:listConversations',
  createConversation: 'muesli:createConversation',
  getConversation: 'muesli:getConversation',
  deleteConversation: 'muesli:deleteConversation',
  listMessages: 'muesli:listMessages',
  sendMessage: 'muesli:sendMessage',
  getCalendarEvents: 'muesli:getCalendarEvents',
  getGoogleCalendarOAuthStatus: 'muesli:getGoogleCalendarOAuthStatus',
  openGoogleCalendarOAuthStart: 'muesli:openGoogleCalendarOAuthStart',
  getMicrosoftCalendarOAuthStatus: 'muesli:getMicrosoftCalendarOAuthStatus',
  openMicrosoftCalendarOAuthStart: 'muesli:openMicrosoftCalendarOAuthStart',
  getCalendarPrefs: 'muesli:getCalendarPrefs',
  setCalendarPrefs: 'muesli:setCalendarPrefs',
  meetingDetectionRendererReady: 'muesli:meetingDetectionRendererReady',
  meetingDetectionPromptShow: 'muesli:meetingDetectionPromptShow',
  meetingDetectionPromptClear: 'muesli:meetingDetectionPromptClear',
  meetingDetectionAutoRecord: 'muesli:meetingDetectionAutoRecord',
  meetingDetectionPromptAccept: 'muesli:meetingDetectionPromptAccept',
  meetingDetectionPromptDismiss: 'muesli:meetingDetectionPromptDismiss',
  micStatus: 'muesli:micStatus',
  micRequest: 'muesli:micRequest',
  micOpenSettings: 'muesli:micOpenSettings',
  systemAudioStatus: 'muesli:systemAudioStatus',
  systemAudioRequest: 'muesli:systemAudioRequest',
  systemAudioOpenSettings: 'muesli:systemAudioOpenSettings',
  systemAudioAvailable: 'muesli:systemAudioAvailable',
  systemAudioStart: 'muesli:systemAudioStart',
  systemAudioPcm: 'muesli:systemAudioPcm',
  systemAudioStop: 'muesli:systemAudioStop',
  writeClipboardText: 'muesli:writeClipboardText',
  getDigestConfig: 'muesli:getDigestConfig',
  updateDigestConfig: 'muesli:updateDigestConfig',
} as const

/** Main-to-renderer `authInvalidated` push emitted by auth handling in `src/main/ipcHandlers.ts`. */
export interface AuthInvalidatedNotice {
  message: string
}

/** Renderer-to-main `connect` payload handled by `connect` in `src/main/ipcHandlers.ts`. */
export interface ConnectRequest {
  serverUrl: string
  email: string
  password: string
  isFirstRun: boolean
  // Override the HTTPS guardrail to allow a plain-HTTP connection to a non-loopback
  // server (the user explicitly opted in via the connect screen).
  allowInsecure?: boolean
}

/**
 * Renderer-to-main `uploadAudio` payload handled by `uploadAudio` in
 * `src/main/ipcHandlers.ts`; null audio asks main to upload an already-staged recording.
 */
export interface UploadAudioRequest {
  noteId: string
  audio: ArrayBuffer | null
  audioMimeType?: string
}

/**
 * Main-to-renderer `noteStreamEvent` snapshot emitted after stream handling; offsets
 * are milliseconds and null speaker means diarization has not assigned one.
 * Stream request handlers live in `src/main/main.ts`, not `ipcHandlers.ts`.
 */
export interface NoteStreamSegmentEvent {
  noteId: string
  type: 'segment'
  text: string
  start_ms: number
  end_ms: number
  speaker: string | null
  provisional: true
  final: boolean
}

/**
 * Main-to-renderer `noteStreamEvent` connection snapshot; stream request handlers
 * live in `src/main/main.ts`, with no corresponding handler in `ipcHandlers.ts`.
 */
export interface NoteStreamConnectionEvent {
  noteId: string
  type: 'connecting' | 'live' | 'unavailable' | 'dropped'
}

/**
 * Fire-and-forget main-to-renderer payload for `noteStreamEvent`; stream lifecycle
 * requests are handled in `src/main/main.ts`, not `ipcHandlers.ts`.
 */
export type NoteStreamEvent = NoteStreamSegmentEvent | NoteStreamConnectionEvent

/**
 * Renderer-to-main `postDiarizationReview` payload handled by
 * `postDiarizationReview` in `src/main/ipcHandlers.ts`. At least one of
 * `segmentId` or `reviewState` is required server-side.
 */
export interface DiarizationReviewUpdate {
  segmentId?: string
  speaker?: string
  reviewState?: string
}

/**
 * Renderer-to-main `search` filters handled by `search` in `src/main/ipcHandlers.ts`;
 * date bounds accept RFC 3339 or `YYYY-MM-DD`, and omissions mean no narrowing.
 */
export interface SearchOptions {
  from?: string
  to?: string
  personId?: string
  folderId?: string
  tag?: string
}

/**
 * Renderer-to-main `createConversation` payload handled by `createConversation`
 * in `src/main/ipcHandlers.ts`; content creates and sends the first message atomically,
 * while omitted/blank content creates an empty conversation.
 */
export interface CreateConversationRequest {
  note_id?: string
  title: string
  model_override?: string
  content?: string
}

/**
 * Awaited `createConversation` result from its handler in `src/main/ipcHandlers.ts`;
 * message and sources are absent when no initial content was sent.
 */
export interface CreateConversationResponse extends Conversation {
  message?: Message
  sources?: ChatSource[]
}

/** Renderer-to-main `sendMessage` payload handled by `sendMessage` in `src/main/ipcHandlers.ts`. */
export interface SendMessageRequest {
  content: string
  model_override?: string
}

/** Renderer-to-main `updatePerson` patch handled by `updatePerson` in `src/main/ipcHandlers.ts`. */
export interface UpdatePersonRequest {
  displayName?: string
  companyId?: string | null
}

/**
 * Renderer-to-main `updateActionItem` patch handled by `updateActionItem` in
 * `src/main/ipcHandlers.ts`; null owner explicitly clears assignment, omission preserves it.
 */
export interface UpdateActionItemRequest {
  text?: string
  status?: ActionItemStatus
  ownerPersonId?: string | null
}

/** Renderer export switches consumed by export handlers wired outside `src/main/ipcHandlers.ts`. */
export type ExportRequestOptions = ExportOptions

/** Awaited renderer snapshot from `listNoteActionItems` in `src/main/ipcHandlers.ts`. */
export interface ListNoteActionItemsResponse {
  actionItems: ActionItem[]
  decisions: Decision[]
}

/**
 * Awaited result from `sendMessage` in `src/main/ipcHandlers.ts`; it contains the
 * assistant reply and citations, not the renderer's already-known user message.
 */
export interface SendMessageResponse {
  message: Message
  sources: ChatSource[]
}

/**
 * Fire-and-forget main-to-renderer meeting prompt payload. Meeting detection is
 * wired in `src/main/main.ts` and has no handler in `src/main/ipcHandlers.ts`.
 */
export interface MeetingDetectionEventPayload {
  event: CalendarEvent
  occurrenceKey: string
}

/**
 * Fire-and-forget main-to-renderer auto-record notice. Meeting detection is wired
 * in `src/main/main.ts` and has no handler in `src/main/ipcHandlers.ts`.
 */
export interface MeetingDetectionAutoRecordPayload {
  noteId: string
}

/** Main-to-renderer tray push target; emitted in main with no `ipcHandlers.ts` handler. */
export type TrayNavigationTarget = '/new' | '/settings'

/**
 * Awaited renderer-facing API exposed by preload. Promise methods invoke their
 * matching `IPC` channel; most delegate to same-named handlers returned by
 * `createHandlers` in `src/main/ipcHandlers.ts`, while OS/audio/export/meeting
 * methods are handled directly in `src/main/main.ts`. `on*` methods subscribe to
 * fire-and-forget main pushes and return an unsubscribe function. Returned objects
 * are snapshots; callers must invoke again or subscribe to an event for fresh state.
 */
export interface MuesliBridge {
  platform: NodeJS.Platform
  onAuthInvalidated?(listener: (notice: AuthInvalidatedNotice) => void): () => void
  getConfig(): Promise<ServerConfig | null>
  getManualServer(): Promise<boolean>
  getOnboarded(): Promise<boolean>
  setOnboarded(b: boolean): Promise<void>
  getKeepRunningInBackground(): Promise<boolean>
  setKeepRunningInBackground(b: boolean): Promise<void>
  getReadyz(): Promise<{ ollamaDetected: boolean } | null>
  getEmbeddedStartupStatus(): Promise<EmbeddedStartupStatus | null>
  getServerHealth(): Promise<{ reachable: boolean; authenticated: boolean; version?: string }>
  connect(req: ConnectRequest): Promise<{ serverUrl: string }>
  disconnect(): Promise<void>
  resetToBuiltIn(): Promise<void>
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
  startNoteStream(noteId: string): Promise<void>
  stopNoteStream(noteId: string): Promise<void>
  sendNoteStreamAudio(noteId: string, audio: ArrayBuffer): Promise<void>
  onNoteStreamEvent(cb: (event: NoteStreamEvent) => void): () => void
  onEmbeddedStartupStatus?(cb: (status: EmbeddedStartupStatus) => void): () => void
  onMeetingDetectionPromptShow?(cb: (payload: MeetingDetectionEventPayload) => void): () => void
  onMeetingDetectionPromptClear?(cb: (payload: { occurrenceKey: string }) => void): () => void
  onMeetingDetectionAutoRecord?(cb: (payload: MeetingDetectionAutoRecordPayload) => void): () => void
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
  createTemplate(name: string, phase: Template['phase'], sections: TemplateSection[], autoRun: boolean): Promise<Template>
  updateTemplate(id: string, name: string, phase: Template['phase'], sections: TemplateSection[], autoRun: boolean): Promise<void>
  deleteTemplate(id: string): Promise<void>
  exportFile(defaultName: string, content: string): Promise<string | null>
  exportNote(noteId: string, format: string, options?: ExportRequestOptions): Promise<{ success: true; path: string } | { success: false; error: string }>
  exportFolder(folderId: string, format: string, options?: ExportRequestOptions): Promise<{ success: true; path: string } | { success: false; error: string }>
  exportAllNotes(): Promise<{ success: true; path: string } | { success: false; error: string }>
  resummarize(id: string): Promise<void>
  regenerateSummary(noteId: string, templateId: string): Promise<void>
  retryNote(id: string): Promise<void>
  processNextNote(id: string): Promise<void>
  search(q: string, opts?: SearchOptions): Promise<SearchMatch[]>
  onUploadProgress(cb: (p: UploadProgress) => void): () => void
  onTrayNavigate?(cb: (target: TrayNavigationTarget) => void): () => void
  getDefaultTranscriberStatus(): Promise<PluginStatus>
  listPlugins(): Promise<Plugin[]>
  checkPluginHealth(id: string): Promise<PluginHealth>
  setStreamingTranscriber(req: { url: string; token: string }): Promise<Plugin>
  clearStreamingTranscriber(): Promise<void>
  checkAudioDedup(audio: ArrayBuffer): Promise<{ existingNoteId?: string; existingNoteTitle?: string }>
  listSpeakerAliases(noteId: string): Promise<SpeakerAlias[]>
  upsertSpeakerAlias(noteId: string, label: string, aliasName: string): Promise<SpeakerAlias>
  getDiarizationReview(noteId: string): Promise<DiarizationReview>
  postDiarizationReview(noteId: string, body: DiarizationReviewUpdate): Promise<DiarizationReview>
  // --- Chat (CHT05) ---
  listConversations(noteId?: string): Promise<Conversation[]>
  createConversation(req: CreateConversationRequest): Promise<CreateConversationResponse>
  getConversation(id: string): Promise<Conversation>
  deleteConversation(id: string): Promise<void>
  listMessages(conversationId: string): Promise<Message[]>
  sendMessage(conversationId: string, req: SendMessageRequest): Promise<SendMessageResponse>
  // --- Calendar (CALUI01) ---
  getCalendarEvents(from: string, to: string): Promise<CalendarEvent[]>
  getGoogleCalendarOAuthStatus(): Promise<GoogleOAuthStatus>
  openGoogleCalendarOAuthStart(): Promise<void>
  getMicrosoftCalendarOAuthStatus(): Promise<MicrosoftOAuthStatus>
  openMicrosoftCalendarOAuthStart(): Promise<void>
  getCalendarPrefs?(): Promise<{ autoRecordDetectedMeetings: boolean }>
  setCalendarPrefs?(prefs: { autoRecordDetectedMeetings: boolean }): Promise<{ autoRecordDetectedMeetings: boolean }>
  meetingDetectionRendererReady?(): Promise<void>
  meetingDetectionPromptAccept?(occurrenceKey: string): Promise<void>
  meetingDetectionPromptDismiss?(occurrenceKey: string): Promise<void>
  micStatus(): Promise<MicStatus>
  micRequest(): Promise<MicStatus>
  micOpenSettings(): Promise<void>
  systemAudioStatus(): Promise<SysAudioStatus>
  systemAudioRequest(): Promise<SysAudioStatus>
  systemAudioOpenSettings(): Promise<void>
  systemAudioAvailable(): Promise<boolean>
  systemAudioStart(): Promise<SystemAudioFormat | null>
  onSystemAudioPcm(cb: (chunk: Uint8Array) => void): () => void
  systemAudioStop(): Promise<void>
  writeClipboardText(text: string): Promise<void>
  getDigestConfig(): Promise<DigestConfig>
  updateDigestConfig(cadence: DigestConfig['cadence']): Promise<DigestConfig>
}
