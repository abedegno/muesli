import { describe, expect, it } from 'vitest'
import { NOTE_STATUSES, type NoteStatus } from '../../shared/types'
import { recordUnavailableReason } from './recordAvailability'

describe('recordUnavailableReason', () => {
  it('maps every note status to whether a new capture can start', () => {
    const expected: Record<NoteStatus, string | undefined> = {
      draft: undefined,
      recording: undefined,
      uploaded: 'This note already has a recording',
      transcribing: 'This note already has a recording',
      summarizing: 'This note already has a recording',
      ready: 'This note already has a recording',
      failed: 'This note already has a recording',
    }

    expect(NOTE_STATUSES).toHaveLength(Object.keys(expected).length)
    for (const status of NOTE_STATUSES) {
      expect(recordUnavailableReason(status), status).toBe(expected[status])
    }
  })
})
