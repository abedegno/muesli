export interface NoteExportRequest {
  path: string
  method: 'GET'
}

export function buildNoteExportRequest(noteId: string, format: string): NoteExportRequest {
  return {
    method: 'GET',
    path: `/api/notes/${encodeURIComponent(noteId)}/export?format=${encodeURIComponent(format)}`,
  }
}

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
