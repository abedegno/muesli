/** Renderer-selected switches that main serializes into a note export request. */
export interface ExportOptions {
  includeTranscript?: boolean
  redactSpeakers?: boolean
}

/** Immutable HTTP request description produced in main for one note export. */
export interface NoteExportRequest {
  path: string
  method: 'GET'
}

/** Builds the encoded GET request that main sends for a renderer-initiated export. */
export function buildNoteExportRequest(noteId: string, format: string, options?: ExportOptions): NoteExportRequest {
  const params = new URLSearchParams({ format })
  if (options?.includeTranscript !== undefined) params.set('include_transcript', String(options.includeTranscript))
  if (options?.redactSpeakers !== undefined) params.set('redact_speakers', String(options.redactSpeakers))
  return {
    method: 'GET',
    path: `/api/notes/${encodeURIComponent(noteId)}/export?${params.toString()}`,
  }
}

/**
 * Extracts the server-suggested download name, preferring RFC 5987 `filename*`.
 * Returns `null` when the header is absent or contains no usable filename.
 */
export function parseContentDispositionFilename(value: string | null): string | null {
  if (!value) return null

  const parts = value.split(';').map((part) => part.trim())
  let fallback: string | null = null

  for (const part of parts.slice(1)) {
    const lower = part.toLowerCase()
    if (lower.startsWith('filename*=')) {
      const raw = part.slice('filename*='.length).trim()
      const parsed = parseDispositionValue(raw)
      if (!parsed) continue
      const encoded = parsed.split("'").slice(2).join("'")
      if (!encoded) continue
      try {
        return decodeURIComponent(encoded)
      } catch {
        continue
      }
    }

    if (fallback == null && lower.startsWith('filename=')) {
      fallback = parseDispositionValue(part.slice('filename='.length).trim())
    }
  }

  return fallback
}

function parseDispositionValue(raw: string): string | null {
  if (!raw) return null
  if (raw.startsWith('"') && raw.endsWith('"')) {
    return raw.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, '\\')
  }
  return raw
}
