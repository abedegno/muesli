import type { ActionItem, ActionItemStatus, AudioUrlGrant, CalendarEvent, ChatSource, CompanyWithCount, CompanyWithPeople, Conversation, Decision, DiarizationReview, Folder, FullNote, GoogleOAuthStatus, Message, MicrosoftOAuthStatus, Note, NoteLink, NoteLinksResponse, PersonWithCompany, PluginStatus, RelatedNote, RetranscribeNoteRequest, RetranscribeNoteResponse, SearchMatch, ServerConfig, SmartList, RuleGroup, SpeakerAlias, Template, TemplateSection } from './types'
import type { UploadProgress } from '../main/uploadMachine'

export const IPC = {
  getConfig: 'muesli:getConfig',
  connect: 'muesli:connect',
  disconnect: 'muesli:disconnect',
  listNotes: 'muesli:listNotes',
  listPeople: 'muesli:listPeople',
  listCompanies: 'muesli:listCompanies',
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
  permanentDeleteNote: 'muesli:permanentDeleteNote',
  getNoteAudioUrl: 'muesli:getNoteAudioUrl',
  uploadAudio: 'muesli:uploadAudio',
  uploadProgress: 'muesli:uploadProgress',
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
  exportAllNotes: 'muesli:exportAllNotes',
  resummarize: 'muesli:resummarize',
  regenerateSummary: 'muesli:regenerateSummary',
  retryNote: 'muesli:retryNote',
  processNextNote: 'muesli:processNextNote',
  search: 'muesli:search',
  getDefaultTranscriberStatus: 'muesli:getDefaultTranscriberStatus',
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
} as const

export interface ConnectRequest {
  serverUrl: string
  email: string
  password: string
  isFirstRun: boolean
  // Override the HTTPS guardrail to allow a plain-HTTP connection to a non-loopback
  // server (the user explicitly opted in via the connect screen).
  allowInsecure?: boolean
}

export interface UploadAudioRequest {
  noteId: string
  audio: ArrayBuffer | null
  audioMimeType?: string
}

export interface NoteStreamSegmentEvent {
  noteId: string
  type: 'segment'
  text: string
  start_ms: number
  end_ms: number
  speaker: string | null
  provisional: true
}

export interface NoteStreamConnectionEvent {
  noteId: string
  type: 'connecting' | 'live' | 'unavailable' | 'dropped'
}

export type NoteStreamEvent = NoteStreamSegmentEvent | NoteStreamConnectionEvent

// Body accepted by MuesliBridge.postDiarizationReview / POST
// /api/notes/{id}/transcript/review. At least one of segmentId or
// reviewState is required (enforced server-side); segmentId+speaker together
// confirm/reassign one turn's speaker, reviewState alone transitions the
// review lifecycle.
export interface DiarizationReviewUpdate {
  segmentId?: string
  speaker?: string
  reviewState?: string
}

// Optional date-range narrowing for search (RFC3339 or YYYY-MM-DD); both ends
// are optional and independent, mirroring the server's from/to query params.
export interface SearchOptions {
  from?: string
  to?: string
}

// Body accepted by MuesliBridge.createConversation / POST /api/conversations.
// When `content` is present the server creates the conversation AND sends the
// first message in one call ("create-and-send") — the natural path for both
// "Ask about this note" and starting a new global chat with an initial
// question. Omit/blank `content` to just create an empty conversation.
export interface CreateConversationRequest {
  note_id?: string
  title: string
  model_override?: string
  content?: string
}

// Response shape for POST /api/conversations: the Conversation fields are
// always present; `message`/`sources` are present only when `content` was
// sent (create-and-send).
export interface CreateConversationResponse extends Conversation {
  message?: Message
  sources?: ChatSource[]
}

// Body accepted by MuesliBridge.sendMessage / POST /api/conversations/{id}/messages.
export interface SendMessageRequest {
  content: string
  model_override?: string
}

export interface UpdatePersonRequest {
  displayName?: string
  companyId?: string | null
}

export interface UpdateActionItemRequest {
  text?: string
  status?: ActionItemStatus
  ownerPersonId?: string | null
}

export interface ListNoteActionItemsResponse {
  actionItems: ActionItem[]
  decisions: Decision[]
}

// Response for POST /api/conversations/{id}/messages: the ASSISTANT reply
// (the user's own message isn't echoed back — callers already have it).
export interface SendMessageResponse {
  message: Message
  sources: ChatSource[]
}

export interface MuesliBridge {
  getConfig(): Promise<ServerConfig | null>
  connect(req: ConnectRequest): Promise<{ serverUrl: string }>
  disconnect(): Promise<void>
  listNotes(folderId?: string): Promise<Note[]>
  listPeople(): Promise<PersonWithCompany[]>
  listCompanies(): Promise<CompanyWithCount[]>
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
  permanentDeleteNote(id: string): Promise<void>
  getNoteAudioUrl(noteId: string): Promise<AudioUrlGrant | null>
  uploadAudio(req: UploadAudioRequest): Promise<{ noteId: string }>
  startNoteStream(noteId: string): Promise<void>
  stopNoteStream(noteId: string): Promise<void>
  sendNoteStreamAudio(noteId: string, audio: ArrayBuffer): Promise<void>
  onNoteStreamEvent(cb: (event: NoteStreamEvent) => void): () => void
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
  createTemplate(name: string, sections: TemplateSection[]): Promise<Template>
  updateTemplate(id: string, name: string, sections: TemplateSection[]): Promise<void>
  deleteTemplate(id: string): Promise<void>
  exportFile(defaultName: string, content: string): Promise<string | null>
  exportNote(noteId: string, format: string): Promise<{ success: true; path: string } | { success: false; error: string }>
  exportAllNotes(): Promise<{ success: true; path: string } | { success: false; error: string }>
  resummarize(id: string): Promise<void>
  regenerateSummary(noteId: string, templateId: string): Promise<void>
  retryNote(id: string): Promise<void>
  processNextNote(id: string): Promise<void>
  search(q: string, opts?: SearchOptions): Promise<SearchMatch[]>
  onUploadProgress(cb: (p: UploadProgress) => void): () => void
  getDefaultTranscriberStatus(): Promise<PluginStatus>
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
}
