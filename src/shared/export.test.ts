import { describe, expect, it } from 'vitest'
import { buildNoteExportRequest, parseContentDispositionFilename } from './export'

describe('note export helpers', () => {
  it('builds the export request path with encoded note id and format', () => {
    expect(buildNoteExportRequest('note/123', 'md')).toEqual({
      method: 'GET',
      path: '/api/notes/note%2F123/export?format=md',
    })
  })

  it('parses a quoted Content-Disposition filename', () => {
    expect(parseContentDispositionFilename('attachment; filename="Team Notes.md"')).toBe('Team Notes.md')
  })

  it('parses an RFC 5987 filename* value', () => {
    expect(parseContentDispositionFilename("attachment; filename*=UTF-8''Team%20Notes.md")).toBe('Team Notes.md')
  })

  it('returns null when no filename is present', () => {
    expect(parseContentDispositionFilename('attachment')).toBeNull()
  })
})
