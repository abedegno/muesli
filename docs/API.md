# Muesli HTTP API Reference

This document describes the HTTP API exposed by the Muesli Go server.

## Conventions

- Public endpoints are unauthenticated unless noted otherwise.
- Authenticated endpoints require either:
  - `Authorization: Bearer <token>`
  - `muesli_session` cookie
- Authentication failures return:
  - `401 {"error":"unauthorized"}`
- All JSON error responses use the shape:
  - `{"error":"<message>"}`

## JSON Models

### Note

```json
{
  "id": "<uuid>",
  "owner_id": "<uuid>",
  "title": "<string>",
  "status": "recording|uploaded|transcribing|summarizing|ready|failed",
  "started_at": "<RFC3339 or omitted>",
  "ended_at": "<RFC3339 or omitted>",
  "created_at": "<RFC3339>",
  "updated_at": "<RFC3339>",
  "deleted_at": "<RFC3339 or omitted>",
  "snippet": "<string, list responses only>",
  "tags": ["<string>"],
  "folder_ids": ["<uuid>"],
  "event_id": "<uuid, omitted if unlinked>"
}
```

Notes:

- `snippet` is only populated by list responses.
- `tags` and `folder_ids` are always serialized as arrays, never `null`.
- `event_id` is the linked calendar event's id; it is omitted when the note has no linked event.

### TagCount

Returned by `GET /api/tags`.

```json
{ "id": "<uuid>", "name": "<string>", "count": <int> }
```

### Tag

Returned by tag rename and note-tag add endpoints.

```json
{ "id": "<uuid>", "name": "<string>" }
```

### Folder

```json
{
  "id": "<uuid>",
  "name": "<string>",
  "parent_id": "<uuid or null>",
  "created_at": "<RFC3339>",
  "deleted_at": "<RFC3339 or omitted>",
  "note_count": <int>
}
```

Notes:

- Folder responses do not include `owner_id`.
- Folder list responses include `note_count`.

### CalendarSource

```json
{
  "id": "<uuid>",
  "owner_id": "<uuid>",
  "kind": "ics|caldav",
  "display_name": "<string>",
  "selected_calendars": {
    "<calendar-key>": true
  },
  "status": "<string>",
  "last_synced_at": "<RFC3339 or null>",
  "created_at": "<RFC3339>"
}
```

Notes:

- Calendar source responses do not include credentials.
- `selected_calendars` is a map of upstream calendar keys to sync-selection flags.
- `last_synced_at` is `null` until the source has synced at least once.

### CalendarEvent

```json
{
  "id": "<uuid>",
  "owner_id": "<uuid>",
  "source_id": "<uuid>",
  "external_id": "<string>",
  "title": "<string>",
  "starts_at": "<RFC3339>",
  "ends_at": "<RFC3339>",
  "description": "<string>",
  "location": "<string>",
  "conferencing_url": "<string>",
  "attendees": [
    {
      "email": "<string>",
      "name": "<string>",
      "response": "<string>"
    }
  ],
  "updated_at": "<RFC3339>"
}
```

Notes:

- `attendees` is always serialized as an array, never `null`.

### SmartList

```json
{
  "id": "<uuid>",
  "name": "<string>",
  "rule": { "opaque": "json boolean tree" },
  "created_at": "<RFC3339>",
  "deleted_at": "<RFC3339 or omitted>"
}
```

Notes:

- The `rule` field is validated on write, but the server treats its inner shape as opaque JSON.

### Template

```json
{
  "id": "<uuid>",
  "name": "<string>",
  "sections": [
    { "heading": "<string>", "instruction": "<string>" }
  ],
  "built_in": <bool>
}
```

Notes:

- Built-in templates are identified by `built_in: true`.
- Template responses do not include `owner_id`.

### Summary

```json
{
  "id": "<uuid>",
  "note_id": "<uuid>",
  "template_id": "<uuid>",
  "template_name": "<string>",
  "agent_plugin": "<string>",
  "model": "<string>",
  "status": "pending|ready|failed",
  "sections": [
    {
      "heading": "<string>",
      "content_markdown": "<string>",
      "refs": [<int>]
    }
  ]
}
```

### Transcript segment

```json
{
  "id": "<string, omitted when empty>",
  "start_ms": <int>,
  "end_ms": <int>,
  "text": "<string>",
  "source": "<string>",
  "speaker": "<string, omitted when empty>"
}
```

### Plugin

```json
{
  "id": "<uuid>",
  "kind": "transcriber|agent",
  "name": "<string>",
  "endpoint_url": "<string>",
  "config": {},
  "config_schema": {},
  "enabled": <bool>,
  "is_default": <bool>
}
```

Notes:

- `token` is write-only and is not serialized in JSON responses.
- `config` and `config_schema` are raw JSON values.

### Job

```json
{
  "id": "<uuid>",
  "note_id": "<uuid>",
  "type": "transcribe|summarize|embed",
  "status": "pending|running|done|failed",
  "attempts": <int>,
  "last_error": "<string>"
}
```

Notes:

- `last_error` is omitted when empty.

### UploadGrant

```json
{
  "url": "<full presigned PUT URL>",
  "method": "PUT",
  "key": "<object key>",
  "expires_at": "<RFC3339>"
}
```

## Public Endpoints

### `GET /healthz`

Liveness probe.

- Auth: none
- Response `200`: `{"status":"ok"}`

### `GET /api/setup/status`

Returns whether first-run setup is required.

- Auth: none
- Response `200`: `{"needs_setup": true|false}`
- Errors:
  - `500`: database error

### `POST /api/setup`

Creates the initial user account. Rejected once any user already exists.

- Auth: none
- Request body:
  ```json
  { "email": "<string>", "password": "<string min 8 chars>" }
  ```
- Response `201`:
  ```json
  { "id": "<uuid>", "email": "<string>" }
  ```
- Errors:
  - `400`: missing email or password shorter than 8 characters
  - `409`: a user already exists
  - `500`: database or password hashing error

### `POST /api/login`

Authenticates a user and returns a session token.

- Auth: none
- Request body:
  ```json
  { "email": "<string>", "password": "<string>" }
  ```
- Response `200`:
  ```json
  { "token": "<raw-token>" }
  ```
- Errors:
  - `400`: invalid JSON body
  - `401`: invalid credentials
  - `500`: database or token generation error

## Blob Storage

The storage endpoint is HMAC-signed and does not use session tokens.

URL shape:

`/_storage/<key>?exp=<unix-seconds>&sig=<hex-hmac-sha256>`

The signature is computed over:

`<key>\n<exp>`

using the server signing key. The signature is host-independent.

### `GET /_storage/{key}`

Downloads a stored blob.

- Auth: HMAC signature in query string
- Query params:
  - `exp`: Unix timestamp
  - `sig`: hex HMAC-SHA256
- Response `200`: raw binary with `Content-Type: application/octet-stream`
- Errors:
  - `403`: expired or invalid signature
  - `404`: object not found

### `PUT /_storage/{key}`

Uploads a blob to a signed URL.

- Auth: HMAC signature in query string
- Query params:
  - `exp`: Unix timestamp
  - `sig`: hex HMAC-SHA256
- Request body: raw audio data
- Required header:
  - `Content-Type: audio/*`
- Limits:
  - 1 GiB maximum body size
- Response `200`: empty body
- Errors:
  - `403`: expired or invalid signature
  - `413`: payload exceeds 1 GiB
  - `415`: content type is not `audio/*`

## Authenticated Endpoints

All endpoints in this section require a valid session token or `muesli_session` cookie.

### Tokens

#### `POST /api/tokens`

Creates a named API token for the authenticated user.

- Auth: required
- Request body:
  ```json
  { "name": "<string>" }
  ```
- Response `201`:
  ```json
  { "token": "<raw-token>" }
  ```
- Errors:
  - `400`: missing name
  - `401`: not authenticated
  - `500`: token generation or database error

### Notes

#### `POST /api/notes`

Creates a new note.

- Auth: required
- Request body:
  ```json
  { "title": "<string>" }
  ```
- Response `201`: Note object
- Errors:
  - `400`: invalid JSON
  - `500`: database error

#### `GET /api/notes`

Lists non-deleted notes for the authenticated user.

- Auth: required
- Query params:
  - `tag=<name>`
  - `status=<status>`
  - `folder_id=<uuid>`
- Response `200`: array of Note objects, always an array
- Errors:
  - `400`: invalid `folder_id`
  - `500`: database error

#### `GET /api/notes/{id}`

Fetches a single note by ID.

- Auth: required
- Response `200`: Note object
- Notes:
  - `tags` and `folder_ids` are always returned as empty arrays on this endpoint
  - `snippet` is empty/omitted on this endpoint
- Errors:
  - `404`: invalid UUID, not found, or owned by another user
  - `500`: database error

#### `PATCH /api/notes/{id}`

Updates a note title.

- Auth: required
- Request body:
  ```json
  { "title": "<string>" }
  ```
- Notes:
  - The title is trimmed before validation
  - Empty titles are rejected
- Response `200`: updated Note object
- Errors:
  - `400`: invalid JSON or empty title
  - `404`: invalid UUID, not found, or owned by another user
  - `500`: database error

#### `PUT /api/notes/{id}/body`

Replaces the note body.

- Auth: required
- Request body:
  ```json
  { "content": "<markdown-string>" }
  ```
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: invalid JSON
  - `404`: invalid UUID, not found, or owned by another user
  - `500`: database error

#### `POST /api/notes/{id}/event`

Links a note to a calendar event.

- Auth: required
- Request body:
  ```json
  { "event_id": "<uuid>" }
  ```
- Notes:
  - The note is verified to exist and belong to the caller before the event id is checked
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: invalid JSON body, invalid `event_id`, or event not found / not owned by caller
  - `404`: invalid UUID, not found, or owned by another user
  - `500`: database error

#### `DELETE /api/notes/{id}/event`

Unlinks a note's calendar event.

- Auth: required
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `404`: invalid UUID, not found, or owned by another user
  - `500`: database error

#### `DELETE /api/notes/{id}`

Soft-deletes a note and moves it to trash.

- Auth: required
- Response `200`:
  ```json
  { "status": "trashed" }
  ```
- Errors:
  - `404`: invalid UUID or not found
  - `500`: database error

#### `GET /api/notes/trash`

Lists trashed notes.

- Auth: required
- Response `200`: array of Note objects
- Errors:
  - `500`: database error

#### `POST /api/notes/{id}/restore`

Restores a trashed note.

- Auth: required
- Response `200`:
  ```json
  { "status": "restored" }
  ```
- Errors:
  - `404`: invalid UUID or not found in trash
  - `500`: database error

#### `DELETE /api/notes/{id}/permanent`

Permanently deletes a note.

- Auth: required
- Notes:
  - If the note had an audio object key, the blob is deleted synchronously before the response is sent; deletion errors are logged but not returned to the caller
- Response `200`:
  ```json
  { "status": "deleted" }
  ```
- Errors:
  - `404`: invalid UUID or not found
  - `500`: database or storage error

#### `GET /api/notes/{id}/full`

Returns the full note payload used by the desktop client.

- Auth: required
- Response `200`:
  ```json
  {
    "note": { "...": "note object with tags and folder_ids populated" },
    "body_markdown": "<string>",
    "transcript": {
      "segments": [{ "...": "segment" }]
    },
    "summaries": [{ "...": "summary" }]
  }
  ```
- Notes:
  - `transcript` is `null` if no transcript exists yet
  - `summaries` is always an array
- Errors:
  - `404`: invalid UUID or note not found
  - `500`: database error

#### `POST /api/notes/{id}/resummarize`

Deletes existing summaries and enqueues fresh summarization jobs.

- Auth: required
- Notes:
  - A transcript must already exist
- Response `202`:
  ```json
  { "status": "summarizing" }
  ```
- Errors:
  - `404`: invalid UUID or note not found
  - `409`: no transcript exists yet
  - `500`: database error

#### `POST /api/notes/{id}/retranscribe`

Re-runs transcription for a note that already has retained audio.

- Auth: required
- Request body:
  ```json
  {
    "model": "<optional string>",
    "language": "<optional string>"
  }
  ```
  - The body may be omitted or empty
- Response `202`:
  ```json
  { "status": "transcribing" }
  ```
- Notes:
  - The note must already be in `ready` or `failed` status and must still have retained audio
  - The handler does not transcribe synchronously; it enqueues a `transcribe` job, and the worker later claims that job to call the configured transcriber plugin
  - The worker's transcribe path replaces any existing transcript for the note; it only deletes the note's summaries and enqueues fresh summarize jobs when the newly saved transcript's `review_state` is `completed`
  - If the new transcript lands in `review_state=pending` because diarization produced speaker-labeled segments, the note's existing summaries are left as-is until that review is completed
- Errors:
  - `400`: invalid body
  - `404`: invalid UUID or note not found
  - `409`: retranscribe already in progress, no stored audio to retranscribe, or note is not ready to retranscribe
  - `500`: database or job enqueue error

#### `GET /api/notes/{id}/export`

Returns a note export as a downloadable attachment.

- Auth: required
- Query params:
  - `format` (optional): `md`, `txt`, `docx`, or `pdf` case-insensitively; defaults to `md` when omitted or blank
- Response `200`:
  - `Content-Type` matches the selected format
  - `Content-Disposition` uses `attachment; filename="<slugified-title>.<ext>"`
  - Body is the raw rendered export bytes, not JSON
- Notes:
  - The export uses the same owner-scoped note lookup and `404` behavior as `GET /api/notes/{id}/full`
  - `md` returns Markdown, `txt` returns plain text, `docx` returns a DOCX document, and `pdf` returns a PDF document
  - Transcript segments and speaker aliases are included when a transcript exists
  - Summary sections are included only when the note is ready and the stored summaries are ready
- Errors:
  - `404`: invalid UUID or note not found
  - `400`: invalid `format` value
  - `500`: internal error while rendering the export

### Audio Upload Flow

#### Step 1: `POST /api/notes/{id}/audio-upload-url`

Issues a 15-minute presigned PUT grant for a note audio object.

- Auth: required
- Request body: none
- Response `200`: UploadGrant object
- Notes:
  - `key` is shaped like `notes/<noteID>/audio/<random-uuid>`
  - `method` is always `PUT`
  - `expires_at` is 15 minutes from issuance
- Errors:
  - `404`: invalid UUID or note not found
  - `500`: database or presign error

#### Step 2: `PUT /_storage/{key}`

Use the signed URL returned by step 1.

See the Blob Storage section for details.

#### Step 3: `POST /api/notes/{id}/audio-uploaded`

Notifies the server that the audio upload completed.

- Auth: required
- Request body:
  ```json
  { "key": "<object-key from grant>" }
  ```
- Response `200`:
  ```json
  { "status": "uploaded" }
  ```
- Notes:
  - The handler records the uploaded audio key on the note and changes the note status to `uploaded`
  - It then enqueues a `transcribe` job with the uploaded object key; the background worker pool is what later claims that job and performs the actual `/transcribe` plugin call
- Errors:
  - `400`: missing or invalid key, key does not belong to this note, or object not found in storage
  - `404`: invalid UUID or note not found
  - `500`: database or storage verification error

### Tags

#### `GET /api/tags`

Lists tags for the authenticated user with live-note counts.

- Auth: required
- Response `200`: array of TagCount objects
- Errors:
  - `500`: database error

#### `PUT /api/tags/{id}`

Renames a tag.

- Auth: required
- Request body:
  ```json
  { "name": "<string>" }
  ```
- Response `200`: Tag object
- Errors:
  - `400`: empty name or invalid tag name
  - `404`: tag not found
  - `409`: name already exists
  - `500`: database error

#### `DELETE /api/tags/{id}`

Deletes a tag and removes it from all notes.

- Auth: required
- Response `204`: no body
- Errors:
  - `404`: tag not found
  - `500`: database error

#### `POST /api/notes/{id}/tags`

Adds a tag to a note. Creates the tag if needed.

- Auth: required
- Request body:
  ```json
  { "name": "<string>" }
  ```
- Response `200`: Tag object
- Errors:
  - `400`: empty or invalid tag name
  - `404`: invalid UUID or note not found
  - `500`: database error

#### `DELETE /api/notes/{id}/tags`

Removes a tag from a note by name.

- Auth: required
- Query param:
  - `name=<tag-name>` required
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: missing `name`
  - `404`: invalid UUID, note not found, or tag not on note
  - `500`: database error

### Folders

#### `GET /api/folders`

Lists non-deleted folders.

- Auth: required
- Response `200`: array of Folder objects
- Errors:
  - `500`: database error

#### `GET /api/folders/trash`

Lists trashed folders.

- Auth: required
- Response `200`: array of Folder objects with `deleted_at` set
- Errors:
  - `500`: database error

#### `POST /api/folders`

Creates a folder.

- Auth: required
- Request body:
  ```json
  { "name": "<string>", "parent_id": "<uuid or null>" }
  ```
- Notes:
  - Folders can be nested
  - Maximum nesting depth is 5
- Response `201`: Folder object
- Errors:
  - `400`: invalid body, invalid parent, depth limit exceeded, or database error (body contains the error message)
  - `409`: folder name already exists

#### `PUT /api/folders/{id}`

Updates a folder name and/or parent.

- Auth: required
- Request body:
  ```json
  { "name": "<string>", "parent_id": "<uuid or null>" }
  ```
- Response `200`: updated Folder object
- Errors:
  - `400`: invalid body, invalid parent, or database error (body contains the error message)
  - `404`: folder not found
  - `409`: duplicate name

#### `PUT /api/folders/{id}/reorder`

Updates a folder's display order.

- Auth: required
- Request body:
  ```json
  { "after_id": "<uuid or null>" }
  ```
- Notes:
  - `after_id: null` moves the folder to the beginning
  - `after_id` must be a valid sibling folder ID
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: invalid body or invalid sibling folder
  - `404`: folder not found
  - `500`: database error

#### `DELETE /api/folders/{id}`

Soft-deletes a folder.

- Auth: required
- Response `200`:
  ```json
  { "status": "trashed" }
  ```
- Errors:
  - `404`: folder not found
  - `500`: database error

#### `POST /api/folders/{id}/restore`

Restores a trashed folder.

- Auth: required
- Response `200`:
  ```json
  { "status": "restored" }
  ```
- Errors:
  - `404`: folder not found in trash
  - `500`: database error

#### `DELETE /api/folders/{id}/permanent`

Permanently deletes a folder.

- Auth: required
- Response `200`:
  ```json
  { "status": "deleted" }
  ```
- Errors:
  - `404`: folder not found
  - `500`: database error

#### `POST /api/notes/{id}/folders`

Adds a note to a folder.

- Auth: required
- Request body:
  ```json
  { "folder_id": "<uuid>" }
  ```
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: missing or invalid `folder_id`
  - `404`: note or folder not found
  - `500`: database error

#### `DELETE /api/notes/{id}/folders/{folderID}`

Removes a note from a specific folder.

- Auth: required
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `404`: invalid UUID
  - `500`: database error

### Calendar

All endpoints in this section are owner-scoped.

#### `GET /api/calendar/events`

Lists the caller's own calendar events in the requested time window.

- Auth: required
- Query params:
  - `from=<RFC3339>` optional, defaults to now
  - `to=<RFC3339>` optional, defaults to `from + 7 days`
- Response `200`: array of CalendarEvent objects
- Errors:
  - `400`: invalid `from` or `to`
  - `500`: database error

#### `GET /api/calendar/oauth/google/status`

Returns whether Google Calendar OAuth is configured on this server.

- Auth: required
- Response `200`:
  ```json
  { "configured": true|false }
  ```

#### `GET /api/calendar/oauth/microsoft/status`

Returns whether Microsoft Calendar OAuth is configured on this server.

- Auth: required
- Response `200`:
  ```json
  { "configured": true|false }
  ```

#### `GET /api/calendar/oauth/google/start`

Starts the Google Calendar OAuth connect flow for the authenticated user.

- Auth: required
- Behavior:
  - validates the caller's auth token
  - issues a short-lived state value for the callback
  - uses the state plus cookie to prevent CSRF and tie the callback to the initiating user
  - sets an HttpOnly, SameSite=Lax cookie scoped to `/api/calendar/oauth/google`
  - redirects the browser to Google with a `302 Found`
- On success:
  - `302`: redirect to Google OAuth authorization
- On error:
  - `401`: missing or invalid authentication
  - `404`: Google OAuth not configured
  - `500`: internal state issue

#### `GET /api/calendar/oauth/google/callback`

Completes the Google Calendar OAuth connect flow after the provider redirects back.

- Auth: none directly - authenticated via the one-time `state` value + cookie pair established during `/start`
- Query params:
  - `state=<string>` required
  - `code=<string>` required once `state` validates
- Behavior:
  - validates that the callback `state` matches the short-lived cookie
  - consumes the matching server-side state record so it can be used once only
  - exchanges the authorization code for provider tokens
  - seals/encrypts the refresh token before storing it
  - clears the OAuth state cookie after a successful connection
  - returns a small HTML success page when the connection completes
- Success:
  - `200`: HTML page indicating Google Calendar is connected
- Errors:
  - `400`: missing, expired, or invalid `state`; or missing `code`
  - `404`: Google OAuth not configured
  - `502`: provider authorization failed or no refresh token was returned
  - `500`: internal storage or sealing error

#### `GET /api/calendar/oauth/microsoft/start`

Starts the Microsoft Calendar OAuth connect flow for the authenticated user.

- Auth: required
- Behavior:
  - validates the caller's auth token
  - issues a short-lived state value for the callback
  - uses the state plus cookie to prevent CSRF and tie the callback to the initiating user
  - sets an HttpOnly, SameSite=Lax cookie scoped to `/api/calendar/oauth/microsoft`
  - redirects the browser to Microsoft with a `302 Found`
- On success:
  - `302`: redirect to Microsoft OAuth authorization
- On error:
  - `401`: missing or invalid authentication
  - `404`: Microsoft OAuth not configured
  - `500`: internal state issue

#### `GET /api/calendar/oauth/microsoft/callback`

Completes the Microsoft Calendar OAuth connect flow after the provider redirects back.

- Auth: none directly - authenticated via the one-time `state` value + cookie pair established during `/start`
- Query params:
  - `state=<string>` required
  - `code=<string>` required once `state` validates
- Behavior:
  - validates that the callback `state` matches the short-lived cookie
  - consumes the matching server-side state record so it can be used once only
  - exchanges the authorization code for provider tokens
  - seals/encrypts the refresh token before storing it
  - clears the OAuth state cookie after a successful connection
  - returns a small HTML success page when the connection completes
- Success:
  - `200`: HTML page indicating Microsoft Calendar is connected
- Errors:
  - `400`: missing, expired, or invalid `state`; or missing `code`
  - `404`: Microsoft OAuth not configured
  - `502`: provider authorization failed or no refresh token was returned
  - `500`: internal storage or sealing error

#### `GET /api/calendar/sources`

Lists the caller's own calendar sources.

- Auth: required
- Response `200`: array of CalendarSource objects
- Errors:
  - `500`: database error

#### `POST /api/calendar/sources`

Creates a new ICS or CalDAV calendar source and kicks an async sync.

- Auth: required
- Request body:
  ```json
  {
    "kind": "ics|caldav",
    "display_name": "<string>",
    "url": "<string>",
    "user": "<string, caldav only>",
    "pass": "<string, caldav only>"
  }
  ```
- Notes:
  - `kind` must be `ics` or `caldav`
  - `url` is required
  - Credentials are sealed server-side and never echoed back
- Response `201`: CalendarSource object
- Errors:
  - `400`: invalid body, invalid kind, or missing url
  - `500`: database or seal error

#### `DELETE /api/calendar/sources/{id}`

Deletes a calendar source and cascades its events.

- Auth: required
- Response `200`:
  ```json
  { "status": "deleted" }
  ```
- Errors:
  - `404`: invalid UUID, not found, or not owned by the caller
  - `500`: database error

#### `POST /api/calendar/sources/{id}/select`

Updates which of a source's upstream calendars are selected for sync.

- Auth: required
- Request body:
  ```json
  {
    "selected": {
      "<calendar-key>": true
    }
  }
  ```
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: invalid body
  - `404`: invalid UUID, not found, or not owned by the caller
  - `500`: database error

#### `POST /api/calendar/refresh`

Kicks an async re-sync of all of the caller's own calendar sources.

- Auth: required
- Response `202`:
  ```json
  { "status": "accepted" }
  ```
- Errors:
  - `500`: database error

### Smart Lists

#### `GET /api/smart-lists`

Lists non-deleted smart lists.

- Auth: required
- Response `200`: array of SmartList objects
- Errors:
  - `500`: database error

#### `GET /api/smart-lists/trash`

Lists trashed smart lists.

- Auth: required
- Response `200`: array of SmartList objects
- Errors:
  - `500`: database error

#### `POST /api/smart-lists`

Creates a smart list.

- Auth: required
- Request body:
  ```json
  { "name": "<string>", "rule": { "opaque": "json boolean tree" } }
  ```
- Response `201`: SmartList object
- Errors:
  - `400`: invalid body, rule validation failure, or database error (body contains the error message)

#### `PUT /api/smart-lists/{id}`

Updates a smart list.

- Auth: required
- Request body:
  ```json
  { "name": "<string>", "rule": { "opaque": "json boolean tree" } }
  ```
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: invalid body, rule validation failure, or database error (body contains the error message)
  - `404`: not found

#### `DELETE /api/smart-lists/{id}`

Soft-deletes a smart list.

- Auth: required
- Response `200`:
  ```json
  { "status": "trashed" }
  ```
- Errors:
  - `404`: not found
  - `500`: database error

#### `POST /api/smart-lists/{id}/restore`

Restores a trashed smart list.

- Auth: required
- Response `200`:
  ```json
  { "status": "restored" }
  ```
- Errors:
  - `404`: not found in trash
  - `500`: database error

#### `DELETE /api/smart-lists/{id}/permanent`

Permanently deletes a smart list.

- Auth: required
- Response `200`:
  ```json
  { "status": "deleted" }
  ```
- Errors:
  - `404`: not found
  - `500`: database error

### Templates

#### `GET /api/templates`

Lists all templates visible to the user.

- Auth: required
- Response `200`: array of Template objects
- Notes:
  - Built-in templates are returned with `built_in: true`
- Errors:
  - `500`: database error

#### `POST /api/templates`

Creates a custom template.

- Auth: required
- Request body:
  ```json
  {
    "name": "<string>",
    "sections": [{ "heading": "<string>", "instruction": "<string>" }]
  }
  ```
- Response `201`: Template object
- Errors:
  - `400`: invalid body or database error (body contains the error message)
  - `409`: template name already exists

#### `PUT /api/templates/{id}`

Updates a template.

- Auth: required
- Request body:
  ```json
  {
    "name": "<string>",
    "sections": [{ "heading": "<string>", "instruction": "<string>" }]
  }
  ```
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `400`: invalid body or database error (body contains the error message)
  - `404`: not found
  - `409`: duplicate name

#### `DELETE /api/templates/{id}`

Deletes a template.

- Auth: required
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Errors:
  - `404`: not found
  - `500`: database error

### Search

#### `GET /api/search`

Hybrid lexical and semantic search over the authenticated user's notes.

- Auth: required
- Query param:
  - `q=<string>` required
- Notes:
  - Empty or whitespace-only queries return `[]`
  - Results are note IDs, ranked highest first
  - Results are capped at 30
  - Semantic search is skipped if no embedder is configured
- Response `200`: array of note ID strings
- Errors:
  - `500`: database or embedding error

### Admin - Plugins

These endpoints are in the authenticated group and do not have a separate admin-role guard.

#### `GET /api/admin/plugins`

Lists all registered plugins.

- Auth: required
- Response `200`: array of Plugin objects
- Notes:
  - `token` is not returned
  - redaction may apply to sensitive config values
- Errors:
  - `500`: database or crypto error

#### `POST /api/admin/plugins`

Registers a new plugin.

- Auth: required
- Request body:
  ```json
  {
    "kind": "transcriber|agent",
    "name": "<string>",
    "endpoint_url": "<string>",
    "token": "<string>",
    "config": {},
    "config_schema": {},
    "enabled": true,
    "is_default": false
  }
  ```
- Required fields:
  - `kind`
  - `name`
  - `endpoint_url`
- Defaults:
  - `enabled` defaults to `true`
  - `config` defaults to `{}` if omitted
- Response `201`:
  ```json
  { "id": "<uuid>" }
  ```
- Errors:
  - `400`: missing required fields or invalid kind
  - `500`: database or crypto error

#### `PATCH /api/admin/plugins/{id}`

Partially updates a plugin. Only supplied fields change.

- Auth: required
- Request body:
  - Same shape as create, with all fields optional
- Response `200`:
  ```json
  { "status": "ok" }
  ```
- Notes:
  - `is_default: true` can be sent by itself to make an existing plugin the default
- Errors:
  - `400`: invalid body
  - `404`: plugin not found
  - `500`: database or crypto error

#### `DELETE /api/admin/plugins/{id}`

Deletes a plugin.

- Auth: required
- Response `204`: no body
- Errors:
  - `404`: plugin not found
  - `500`: database error

### Admin - Jobs

#### `GET /api/admin/jobs`

Lists processing jobs.

- Auth: required
- Query param:
  - `status=<pending|running|done|failed>` optional
- Response `200`: array of Job objects
- Errors:
  - `500`: database error

#### `POST /api/admin/jobs/{id}/retry`

Re-enqueues a failed job.

- Auth: required
- Request body: none
- Response `202`:
  ```json
  { "status": "queued" }
  ```
- Errors:
  - `404`: job not found or associated note not found
  - `409`: job is not in `failed` state
  - `500`: database error
