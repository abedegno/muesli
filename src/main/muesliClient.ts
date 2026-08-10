import type { ActionItem, AudioUrlGrant, CalendarEvent, CompanyWithCount, CompanyWithPeople, Conversation, CreateShareRequest, CreateShareResponse, Decision, DigestConfig, DiarizationReview, FullNote, Folder, GoogleOAuthStatus, InsightsResponse, Message, MicrosoftOAuthStatus, Note, NoteLink, NoteLinksResponse, PersonWithCompany, Plugin, PluginHealth, PluginStatus, RelatedNote, RetranscribeNoteRequest, RetranscribeNoteResponse, RuleGroup, SearchMatch, SearchResult, Share, SmartList, SpeakerAlias, Template, TemplateSection, UploadGrant } from '../shared/types'
import type { CreateConversationRequest, CreateConversationResponse, ListNoteActionItemsResponse, SearchOptions, SendMessageRequest, SendMessageResponse, UpdateActionItemRequest, UpdatePersonRequest } from '../shared/ipc'
import { buildNoteExportRequest, parseContentDispositionFilename, type ExportOptions } from '../shared/export'
import { buildCalendarEventsPath } from '../shared/calendar'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public body?: unknown,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export type FetchLike = (input: string | URL, init?: RequestInit) => Promise<Response>

export interface NoteExportData {
  bytes: Uint8Array
  filename: string | null
  contentType: string | null
}

interface MuesliClientOptions {
  baseUrl: string
  token?: string
  fetch?: FetchLike
}

// MuesliClient is a thin, fully-typed wrapper over the Muesli server API. It is
// Electron-free and fetch-injectable so it can be unit-tested in plain Node.
export class MuesliClient {
  private readonly baseUrl: string
  private token?: string
  private readonly fetchImpl: FetchLike

  constructor(opts: MuesliClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, '')
    this.token = opts.token
    // Fall back to the global fetch (Electron/Node 18+) when not injected.
    this.fetchImpl = opts.fetch ?? ((globalThis as { fetch: FetchLike }).fetch)
  }

  setToken(token: string | undefined) {
    this.token = token
  }

  // --- Auth / onboarding ---
  async setupNeeded(): Promise<boolean> {
    const res = await this.fetchImpl(new URL('/api/setup/status', this.baseUrl).toString())
    if (!res.ok) throw new ApiError(res.status, `setup status: ${res.status}`)
    const body = (await res.json()) as { needs_setup: boolean }
    return body.needs_setup
  }

  async setup(email: string, password: string): Promise<{ id: string; email: string }> {
    return this.json('POST', '/api/setup', { email, password })
  }

  async login(email: string, password: string): Promise<string> {
    const out = await this.json<{ token: string }>('POST', '/api/login', { email, password })
    return out.token
  }

  // sessionToken authorises the mint; the minted app token is what we persist.
  async createToken(name: string, sessionToken: string): Promise<string> {
    const out = await this.json<{ token: string }>('POST', '/api/tokens', { name }, sessionToken)
    return out.token
  }

  // --- Notes ---
  async createNote(title: string): Promise<Note> {
    return this.json<Note>('POST', '/api/notes', { title })
  }

  async listNotes(folderId?: string): Promise<Note[]> {
    const path = folderId ? `/api/notes?folder_id=${encodeURIComponent(folderId)}` : '/api/notes'
    return this.json<Note[]>('GET', path)
  }

  async listPeople(): Promise<PersonWithCompany[]> {
    return this.json<PersonWithCompany[]>('GET', '/api/people')
  }

  async listCompanies(): Promise<CompanyWithCount[]> {
    return this.json<CompanyWithCount[]>('GET', '/api/companies')
  }

  async getInsights(from?: string, to?: string): Promise<InsightsResponse> {
    const params = new URLSearchParams()
    if (from) params.set('from', from)
    if (to) params.set('to', to)
    const path = params.toString().length > 0 ? `/api/insights?${params.toString()}` : '/api/insights'
    return this.json<InsightsResponse>('GET', path)
  }

  async listNoteActionItems(id: string): Promise<ListNoteActionItemsResponse> {
    const res = await this.json<{ action_items: ActionItem[]; decisions: Decision[] }>('GET', `/api/notes/${id}/action-items`)
    return { actionItems: res.action_items, decisions: res.decisions }
  }

  async listNoteLinks(id: string): Promise<NoteLinksResponse> {
    return this.json<NoteLinksResponse>('GET', `/api/notes/${id}/links`)
  }

  async listRelatedNotes(id: string): Promise<RelatedNote[]> {
    return this.json<RelatedNote[]>('GET', `/api/notes/${id}/related`)
  }

  async addNoteLink(id: string, toNoteId: string): Promise<NoteLink> {
    return this.json<NoteLink>('POST', `/api/notes/${id}/links`, { to_note_id: toNoteId })
  }

  async listActionItems(status?: string): Promise<ActionItem[]> {
    const path = status ? `/api/action-items?status=${encodeURIComponent(status)}` : '/api/action-items'
    return this.json<ActionItem[]>('GET', path)
  }

  async getPerson(id: string): Promise<PersonWithCompany> {
    return this.json<PersonWithCompany>('GET', `/api/people/${id}`)
  }

  async getPersonNotes(id: string): Promise<Note[]> {
    return this.json<Note[]>('GET', `/api/people/${id}/notes`)
  }

  async updatePerson(id: string, req: UpdatePersonRequest): Promise<PersonWithCompany> {
    const body: Record<string, unknown> = {}
    if (req.displayName !== undefined) body.display_name = req.displayName
    if (req.companyId !== undefined) body.company_id = req.companyId
    return this.json<PersonWithCompany>('PATCH', `/api/people/${id}`, body)
  }

  async updateActionItem(id: string, req: UpdateActionItemRequest): Promise<ActionItem> {
    const body: Record<string, unknown> = {}
    if (req.text !== undefined) body.text = req.text
    if (req.status !== undefined) body.status = req.status
    if (req.ownerPersonId !== undefined) body.owner_person_id = req.ownerPersonId
    return this.json<ActionItem>('PATCH', `/api/action-items/${id}`, body)
  }

  async mergePeople(fromId: string, intoId: string): Promise<PersonWithCompany> {
    return this.json<PersonWithCompany>('POST', `/api/people/${fromId}/merge`, { into: intoId })
  }

  async deletePerson(id: string): Promise<void> {
    await this.json('DELETE', `/api/people/${id}`)
  }

  async getCompany(id: string): Promise<CompanyWithPeople> {
    return this.json<CompanyWithPeople>('GET', `/api/companies/${id}`)
  }

  // --- Calendar (CALUI01) ---
  async getCalendarEvents(from: string, to: string): Promise<CalendarEvent[]> {
    return this.json<CalendarEvent[]>('GET', buildCalendarEventsPath(from, to))
  }

  async getGoogleCalendarOAuthStatus(): Promise<GoogleOAuthStatus> {
    return this.json<GoogleOAuthStatus>('GET', '/api/calendar/oauth/google/status')
  }

  async getMicrosoftCalendarOAuthStatus(): Promise<MicrosoftOAuthStatus> {
    return this.json<MicrosoftOAuthStatus>('GET', '/api/calendar/oauth/microsoft/status')
  }

  async getDigestConfig(): Promise<DigestConfig> {
    return this.json<DigestConfig>('GET', '/api/digest/config')
  }

  async updateDigestConfig(cadence: DigestConfig['cadence']): Promise<DigestConfig> {
    return this.json<DigestConfig>('PUT', '/api/digest/config', { cadence })
  }

  async getNote(id: string): Promise<Note> {
    return this.json<Note>('GET', `/api/notes/${id}`)
  }

  async getNoteAudioUrl(id: string): Promise<AudioUrlGrant> {
    return this.json<AudioUrlGrant>('GET', `/api/notes/${id}/audio-url`)
  }

  async createShare(id: string, options?: CreateShareRequest): Promise<CreateShareResponse> {
    const expiresAt = options?.expires_at?.trim()
    return this.json<CreateShareResponse>('POST', `/api/notes/${id}/share`, expiresAt ? { expires_at: expiresAt } : undefined)
  }

  async listNoteShares(id: string): Promise<Share[]> {
    return this.json<Share[]>('GET', `/api/notes/${id}/shares`)
  }

  async revokeShare(token: string): Promise<void> {
    await this.json('DELETE', `/api/shares/${encodeURIComponent(token)}`)
  }

  async exportNote(id: string, format: string, options?: ExportOptions): Promise<NoteExportData> {
    const { path, method } = buildNoteExportRequest(id, format, options)
    return this.downloadAttachment(path, method)
  }

  async exportFolder(folderId: string, format: string, options?: ExportOptions): Promise<NoteExportData> {
    const body: Record<string, unknown> = {
      folder_id: folderId,
      format,
    }
    if (options?.includeTranscript !== undefined) body.include_transcript = options.includeTranscript
    if (options?.redactSpeakers !== undefined) body.redact_speakers = options.redactSpeakers
    return this.downloadAttachment('/api/export/batch', 'POST', body)
  }

  // Hybrid semantic + lexical search. Returns typed match objects (possibly
  // several per note, e.g. a title hit AND a transcript hit); the renderer
  // dedupes onto its already-loaded notes (no re-fetch). Degrades to
  // lexical-only server-side when embeddings are disabled. Optional filters are
  // omitted from the querystring when absent.
  async search(q: string, opts?: SearchOptions): Promise<SearchResult> {
    const params = new URLSearchParams()
    params.set('q', q)
    if (opts?.from) params.set('from', opts.from)
    if (opts?.to) params.set('to', opts.to)
    if (opts?.personId) params.set('person_id', opts.personId)
    if (opts?.folderId) params.set('folder_id', opts.folderId)
    if (opts?.tag) params.set('tag', opts.tag)
    const path = `/api/search?${params.toString()}`
    const headers: Record<string, string> = {}
    if (this.token) headers.Authorization = `Bearer ${this.token}`
    const res = await this.fetchImpl(`${this.baseUrl}${path}`, { method: 'GET', headers })
    const text = await res.text()
    const parsed = text ? safeParse(text) : undefined
    if (!res.ok) {
      const message = parsed && typeof parsed === 'object' && 'error' in parsed
        ? String((parsed as { error: unknown }).error)
        : `request failed: ${res.status}`
      throw new ApiError(res.status, message, parsed)
    }
    return {
      matches: parsed as SearchMatch[],
      semanticSearchAvailable: res.headers.get('x-muesli-semantic-search') === 'available',
    }
  }

  async getFull(id: string): Promise<FullNote> {
    return this.json<FullNote>('GET', `/api/notes/${id}/full`)
  }

  async updateTitle(id: string, title: string): Promise<void> {
    await this.json('PATCH', `/api/notes/${id}`, { title })
  }

  async deleteNote(id: string): Promise<void> {
    await this.json('DELETE', `/api/notes/${id}`)
  }

  async duplicateNote(id: string): Promise<Note> {
    return this.json<Note>('POST', `/api/notes/${id}/duplicate`)
  }

  async pinNote(id: string): Promise<void> {
    await this.json('POST', `/api/notes/${id}/pin`)
  }

  async unpinNote(id: string): Promise<void> {
    await this.json('DELETE', `/api/notes/${id}/pin`)
  }

  // --- Calendar link (CALLNK02) ---
  async linkNoteEvent(id: string, eventId: string): Promise<void> {
    await this.json('POST', `/api/notes/${id}/event`, { event_id: eventId })
  }

  async unlinkNoteEvent(id: string): Promise<void> {
    await this.json('DELETE', `/api/notes/${id}/event`)
  }

  async listTrash(): Promise<Note[]> {
    return this.json<Note[]>('GET', '/api/notes/trash')
  }

  async restoreNote(id: string): Promise<void> {
    await this.json('POST', `/api/notes/${id}/restore`)
  }

  async retranscribeNote(id: string, options?: RetranscribeNoteRequest): Promise<RetranscribeNoteResponse> {
    const body = options && (options.model !== undefined || options.language !== undefined)
      ? {
          ...(options.model !== undefined ? { model: options.model } : {}),
          ...(options.language !== undefined ? { language: options.language } : {}),
        }
      : undefined
    return this.json<RetranscribeNoteResponse>('POST', `/api/notes/${id}/retranscribe`, body)
  }

  async permanentDeleteNote(id: string): Promise<void> {
    await this.json('DELETE', `/api/notes/${id}/permanent`)
  }

  async addTag(id: string, name: string): Promise<{ id: string; name: string }> {
    return this.json<{ id: string; name: string }>('POST', `/api/notes/${id}/tags`, { name })
  }

  async removeTag(id: string, name: string): Promise<void> {
    await this.json('DELETE', `/api/notes/${id}/tags?name=${encodeURIComponent(name)}`)
  }

  // Renames an owner-scoped tag by id; cascades to every note carrying it.
  async renameTag(id: string, name: string): Promise<{ id: string; name: string }> {
    return this.json<{ id: string; name: string }>('PUT', `/api/tags/${id}`, { name })
  }

  async deleteTag(id: string): Promise<void> {
    await this.json('DELETE', `/api/tags/${id}`)
  }

  // Server-side tag list with live-note counts (prep for scale). Always [].
  async listTags(): Promise<{ id: string; name: string; count: number }[]> {
    return this.json<{ id: string; name: string; count: number }[]>('GET', '/api/tags')
  }

  // --- Smart Lists ---
  async listSmartLists(): Promise<SmartList[]> {
    return this.json<SmartList[]>('GET', '/api/smart-lists')
  }
  async createSmartList(name: string, rule: RuleGroup): Promise<SmartList> {
    return this.json<SmartList>('POST', '/api/smart-lists', { name, rule })
  }
  async updateSmartList(id: string, name: string, rule: RuleGroup): Promise<void> {
    await this.json('PUT', `/api/smart-lists/${id}`, { name, rule })
  }
  async deleteSmartList(id: string): Promise<void> {
    await this.json('DELETE', `/api/smart-lists/${id}`)
  }
  async listTrashedSmartLists(): Promise<SmartList[]> {
    return this.json<SmartList[]>('GET', '/api/smart-lists/trash')
  }
  async restoreSmartList(id: string): Promise<void> {
    await this.json('POST', `/api/smart-lists/${id}/restore`)
  }
  async permanentDeleteSmartList(id: string): Promise<void> {
    await this.json('DELETE', `/api/smart-lists/${id}/permanent`)
  }

  // --- Folders ---
  async listFolders(): Promise<Folder[]> {
    return this.json<Folder[]>('GET', '/api/folders')
  }
  async createFolder(name: string, parentId?: string | null): Promise<Folder> {
    return this.json<Folder>('POST', '/api/folders', { name, parent_id: parentId ?? null })
  }
  async updateFolder(id: string, name: string, parentId?: string | null): Promise<Folder> {
    return this.json<Folder>('PUT', `/api/folders/${id}`, { name, parent_id: parentId ?? null })
  }
  async deleteFolder(id: string): Promise<void> {
    await this.json('DELETE', `/api/folders/${id}`)
  }
  async reorderFolder(id: string, afterId: string | null): Promise<void> {
    await this.json('PUT', `/api/folders/${id}/reorder`, { after_id: afterId })
  }
  async reorderNoteInFolder(folderId: string, noteId: string, afterId: string | null): Promise<void> {
    await this.json('PUT', `/api/folders/${folderId}/notes/${noteId}/reorder`, { after_id: afterId })
  }
  async listTrashedFolders(): Promise<Folder[]> {
    return this.json<Folder[]>('GET', '/api/folders/trash')
  }
  async restoreFolder(id: string): Promise<void> {
    await this.json('POST', `/api/folders/${id}/restore`)
  }
  async permanentDeleteFolder(id: string): Promise<void> {
    await this.json('DELETE', `/api/folders/${id}/permanent`)
  }
  async addNoteFolder(noteId: string, folderId: string): Promise<void> {
    await this.json('POST', `/api/notes/${noteId}/folders`, { folder_id: folderId })
  }
  async removeNoteFolder(noteId: string, folderId: string): Promise<void> {
    await this.json('DELETE', `/api/notes/${noteId}/folders/${folderId}`)
  }

  // --- Templates ---
  async listTemplates(): Promise<Template[]> {
    return this.json<Template[]>('GET', '/api/templates')
  }
  async createTemplate(name: string, phase: Template['phase'], sections: TemplateSection[], autoRun: boolean): Promise<Template> {
    return this.json<Template>('POST', '/api/templates', { name, phase, sections, auto_run: autoRun })
  }
  async updateTemplate(id: string, name: string, phase: Template['phase'], sections: TemplateSection[], autoRun: boolean): Promise<void> {
    await this.json('PUT', `/api/templates/${id}`, { name, phase, sections, auto_run: autoRun })
  }
  async deleteTemplate(id: string): Promise<void> {
    await this.json('DELETE', `/api/templates/${id}`)
  }

  // --- Upload flow ---
  async getAudioUploadUrl(id: string): Promise<UploadGrant> {
    return this.json<UploadGrant>('POST', `/api/notes/${id}/audio-upload-url`)
  }

  // PUT raw bytes to the signed URL. No bearer token — the URL itself is the grant.
  // contentType is the recorder's actual mimeType (e.g. 'audio/webm;codecs=opus').
  async putAudio(
    grant: UploadGrant,
    bytes: Uint8Array,
    contentType = 'audio/webm',
  ): Promise<void> {
    const res = await this.fetchImpl(grant.url, {
      method: 'PUT',
      headers: { 'Content-Type': contentType },
      // Uint8Array is a valid BufferSource body at runtime; the cast bridges the
      // TS 5.9 BodyInit generic that excludes Uint8Array<ArrayBufferLike>.
      body: bytes as BodyInit,
    })
    if (!res.ok) {
      throw new ApiError(res.status, `audio PUT failed: ${res.status}`)
    }
  }

  async markAudioUploaded(id: string, key: string): Promise<{ status: string }> {
    return this.json<{ status: string }>('POST', `/api/notes/${id}/audio-uploaded`, { key })
  }

  async retryNote(id: string): Promise<void> {
    await this.json('POST', `/api/notes/${id}/retry`)
  }

  async processNextNote(id: string): Promise<void> {
    await this.json('POST', `/api/notes/${id}/process-next`)
  }

  async resummarize(id: string): Promise<void> {
    await this.json('POST', `/api/notes/${id}/resummarize`)
  }

  async regenerateSummary(noteId: string, templateId: string): Promise<void> {
    await this.json('POST', `/api/notes/${noteId}/templates/${templateId}/summarize`)
  }

  // PUT /api/notes/{id}/body is defined in Plan 1a (Task 12). See plan header.
  async putBody(id: string, content: string): Promise<void> {
    await this.json('PUT', `/api/notes/${id}/body`, { content })
  }

  async checkAudioDedup(audioHash: string): Promise<{ matches: Array<{ note_id: string; title: string; status: string; created_at: string }> }> {
    return this.json<{ matches: Array<{ note_id: string; title: string; status: string; created_at: string }> }>('POST', '/api/audio/dedup-check', { audio_hash: audioHash })
  }

  // --- Speaker aliases (DZ03) ---
  async listSpeakerAliases(id: string): Promise<SpeakerAlias[]> {
    const out = await this.json<{ aliases: SpeakerAlias[] }>('GET', `/api/notes/${id}/speaker-aliases`)
    return out.aliases
  }

  // `label` is always the raw/original speaker label (e.g. 'SPEAKER_00'), never a
  // previously-assigned alias name — see DZ03a.
  async upsertSpeakerAlias(id: string, label: string, aliasName: string): Promise<SpeakerAlias> {
    return this.json<SpeakerAlias>('PUT', `/api/notes/${id}/speaker-aliases/${encodeURIComponent(label)}`, { alias_name: aliasName })
  }

  // --- Diarization review (DZ04b/DZ04d) ---
  async getDiarizationReview(id: string): Promise<DiarizationReview> {
    return this.json<DiarizationReview>('GET', `/api/notes/${id}/transcript/review`)
  }

  // body mirrors the server's reviewUpdateRequest exactly (snake_case JSON keys);
  // the camelCase -> snake_case mapping happens in ipcHandlers so this client
  // stays a thin, directly-testable wrapper over the wire format.
  async postDiarizationReview(
    id: string,
    body: { segment_id?: string; speaker?: string; review_state?: string },
  ): Promise<DiarizationReview> {
    return this.json<DiarizationReview>('POST', `/api/notes/${id}/transcript/review`, body)
  }

    // --- internals ---
  async getDefaultTranscriberStatus(): Promise<PluginStatus> {
    return this.json<PluginStatus>('GET', '/api/admin/plugins/default-transcriber/status')
  }

  async listPlugins(): Promise<Plugin[]> {
    return this.json<Plugin[]>('GET', '/api/admin/plugins')
  }

  async checkPluginHealth(id: string): Promise<PluginHealth> {
    return this.json<PluginHealth>('POST', `/api/admin/plugins/${encodeURIComponent(id)}/health`)
  }

  async setStreamingTranscriber(req: { url: string; token: string }): Promise<Plugin> {
    const existing = (await this.listPlugins()).find((plugin) => plugin.kind === 'streaming-transcriber')
    const body = {
      endpoint_url: req.url,
      ...(req.token ? { token: req.token } : {}),
      enabled: true,
      is_default: true,
    }
    if (existing) {
      return this.json<Plugin>('PATCH', `/api/admin/plugins/${encodeURIComponent(existing.id)}`, body)
    }
    return this.json<Plugin>('POST', '/api/admin/plugins', {
      kind: 'streaming-transcriber',
      name: 'Streaming transcriber',
      ...body,
    })
  }

  async clearStreamingTranscriber(): Promise<void> {
    const existing = (await this.listPlugins()).find((plugin) => plugin.kind === 'streaming-transcriber')
    if (existing) await this.json('DELETE', `/api/admin/plugins/${encodeURIComponent(existing.id)}`)
  }

  // --- Chat (CHT01-CHT05) ---
  async listConversations(noteId?: string): Promise<Conversation[]> {
    const path = noteId ? `/api/conversations?note_id=${encodeURIComponent(noteId)}` : '/api/conversations'
    return this.json<Conversation[]>('GET', path)
  }

  async createConversation(body: CreateConversationRequest): Promise<CreateConversationResponse> {
    return this.json<CreateConversationResponse>('POST', '/api/conversations', body)
  }

  async getConversation(id: string): Promise<Conversation> {
    return this.json<Conversation>('GET', `/api/conversations/${id}`)
  }

  async deleteConversation(id: string): Promise<void> {
    await this.json('DELETE', `/api/conversations/${id}`)
  }

  async listMessages(conversationId: string): Promise<Message[]> {
    return this.json<Message[]>('GET', `/api/conversations/${conversationId}/messages`)
  }

  // A 409 here means chatSendGuard rejected a concurrent send on the SAME
  // conversation ("message send already in progress") — callers should check
  // `err instanceof ApiError && err.status === 409` and surface that distinctly
  // from a generic failure.
  async sendMessage(conversationId: string, body: SendMessageRequest): Promise<SendMessageResponse> {
    return this.json<SendMessageResponse>('POST', `/api/conversations/${conversationId}/messages`, body)
  }

  private async downloadAttachment(path: string, method: 'GET' | 'POST', body?: unknown): Promise<NoteExportData> {
    const headers: Record<string, string> = {}
    if (this.token) headers.Authorization = `Bearer ${this.token}`
    if (body !== undefined) headers['Content-Type'] = 'application/json'

    const res = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })

    const bytes = new Uint8Array(await res.arrayBuffer())
    if (!res.ok) {
      const text = bytes.length ? new TextDecoder().decode(bytes) : ''
      const parsed = text ? safeParse(text) : undefined
      const msg =
        (parsed && typeof parsed === 'object' && 'error' in parsed
          ? String((parsed as { error: unknown }).error)
          : undefined) ?? `request failed: ${res.status}`
      throw new ApiError(res.status, msg, parsed)
    }

    return {
      bytes,
      filename: parseContentDispositionFilename(res.headers.get('content-disposition')),
      contentType: res.headers.get('content-type'),
    }
  }

  private async json<T>(
    method: string,
    path: string,
    body?: unknown,
    overrideToken?: string,
  ): Promise<T> {
    const headers: Record<string, string> = {}
    const token = overrideToken ?? this.token
    if (token) headers['Authorization'] = `Bearer ${token}`
    if (body !== undefined) headers['Content-Type'] = 'application/json'

    const res = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })

    const text = await res.text()
    const parsed = text ? safeParse(text) : undefined
    if (!res.ok) {
      const msg =
        (parsed && typeof parsed === 'object' && 'error' in parsed
          ? String((parsed as { error: unknown }).error)
          : undefined) ?? `request failed: ${res.status}`
      throw new ApiError(res.status, msg, parsed)
    }
    return parsed as T
  }
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}
