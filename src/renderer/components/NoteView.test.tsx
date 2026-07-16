// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { NoteView, summaryToMarkdown } from './NoteView'
import type { FullNote, Template } from '../../shared/types'

const { writeClipboardTextMock } = vi.hoisted(() => ({
  writeClipboardTextMock: vi.fn().mockResolvedValue(undefined),
}))

// --- Hoisted bridge mocks (speaker aliases + chat) --------------------------
const {
  muesliMock,
  listSpeakerAliasesMock,
  upsertSpeakerAliasMock,
  listConversationsMock,
  listMessagesMock,
  getNoteAudioUrlMock,
} = vi.hoisted(() => {
  const muesliMock = {
    listSpeakerAliases: vi.fn().mockResolvedValue([]),
    upsertSpeakerAlias: vi.fn().mockResolvedValue({}),
    listConversations: vi.fn().mockResolvedValue([]),
    listMessages: vi.fn().mockResolvedValue([]),
    getNoteAudioUrl: vi.fn().mockResolvedValue({ url: 'http://example.test/audio', expires_at: new Date().toISOString() }),
  }
  return {
    muesliMock,
    listSpeakerAliasesMock: muesliMock.listSpeakerAliases,
    upsertSpeakerAliasMock: muesliMock.upsertSpeakerAlias,
    listConversationsMock: muesliMock.listConversations,
    listMessagesMock: muesliMock.listMessages,
    getNoteAudioUrlMock: muesliMock.getNoteAudioUrl,
  }
})

vi.mock('@/api', () => ({
  muesli: muesliMock,
}))

vi.mock('@/lib/clipboard', () => ({
  writeClipboardText: (text: string) => writeClipboardTextMock(text),
}))

beforeEach(() => {
  listSpeakerAliasesMock.mockReset().mockResolvedValue([])
  upsertSpeakerAliasMock.mockReset().mockResolvedValue({})
  listConversationsMock.mockReset().mockResolvedValue([])
  listMessagesMock.mockReset().mockResolvedValue([])
  getNoteAudioUrlMock.mockReset().mockResolvedValue({ url: 'http://example.test/audio', expires_at: new Date().toISOString() })
  muesliMock.getNoteAudioUrl = getNoteAudioUrlMock
  writeClipboardTextMock.mockReset().mockResolvedValue(undefined)
})

afterEach(cleanup)

// Two-summary fixture — picker should appear
const full: FullNote = {
  note: { id: '1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: 'my raw notes',
  transcript: { segments: [{ start_ms: 0, end_ms: 1000, text: 'hello world', source: 'mixed' }] },
  summaries: [
    { template_name: 'Action items', status: 'ready', sections: [{ heading: 'Action items', content_markdown: 'do X' }] },
    { template_name: 'General meeting', status: 'ready', sections: [
      { heading: 'Overview', content_markdown: 'Ship v2' },
      { heading: 'Decisions', content_markdown: 'postpone' },
    ] },
  ],
}

// Single-summary fixture — no picker
const fullSingle: FullNote = {
  note: { id: '2', title: 'Solo', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: 'raw notes',
  transcript: { segments: [{ start_ms: 0, end_ms: 500, text: 'hi there', source: 'mixed' }] },
  summaries: [
    { template_name: 'Default', status: 'ready', sections: [{ heading: 'Summary', content_markdown: 'Only content' }] },
  ],
}

const fullNoSummaries: FullNote = {
  note: { id: '3', title: 'Empty', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: 'raw notes',
  transcript: { segments: [{ start_ms: 0, end_ms: 500, text: 'hi there', source: 'mixed' }] },
  summaries: [],
}

describe('NoteView — template switcher', () => {
  it('defaults to the richest panel (General meeting, 2 sections)', () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    expect(screen.getByText('Ship v2')).toBeInTheDocument()
    expect(screen.getByText('postpone')).toBeInTheDocument()
    expect(screen.queryByText('do X')).not.toBeInTheDocument()
  })

  it('shows the picker button with active template name when multiple summaries', () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    const picker = screen.getByRole('button', { name: /switch template/i })
    expect(picker).toBeInTheDocument()
    expect(picker).toHaveTextContent('General meeting')
  })

  it('opens the picker dropdown and lists all templates', async () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /switch template/i }))
    // Both template names should appear as buttons inside the dropdown
    const options = screen.getAllByRole('option', { name: /Action items|General meeting/ })
    expect(options).toHaveLength(2)
  })

  it('switches to Action items panel when selected from picker', async () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /switch template/i }))
    await userEvent.click(screen.getByRole('option', { name: 'Action items' }))
    expect(screen.getByText('do X')).toBeInTheDocument()
    expect(screen.queryByText('Ship v2')).not.toBeInTheDocument()
    // Picker should close after selection
    expect(screen.queryByRole('option', { name: 'Action items' })).not.toBeInTheDocument()
  })

  it('does NOT show switch template button for a single-summary note', () => {
    render(<NoteView full={fullSingle} onSaveBody={async () => {}} />)
    expect(screen.queryByRole('button', { name: /switch template/i })).not.toBeInTheDocument()
    expect(screen.getByText('Only content')).toBeInTheDocument()
  })

  it('defaults to Enhanced with three tabs', () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Enhanced' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Transcript' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'My notes' })).toBeInTheDocument()
    expect(screen.queryByText('hello world')).not.toBeInTheDocument() // drawer closed
  })

  it('defaults to the Transcript tab when the note has no summary', () => {
    render(<NoteView full={fullNoSummaries} onSaveBody={async () => {}} />)
    const transcriptTab = screen.getByRole('radio', { name: 'Transcript' })
    expect(transcriptTab).toHaveAttribute('data-state', 'on')
    expect(screen.getByText('hi there')).toBeInTheDocument()
  })

  it("switching tabs hides the previous tab's content", async () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('radio', { name: 'Transcript' }))
    expect(screen.getByText('hello world')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('radio', { name: 'My notes' }))
    expect(screen.queryByText('hello world')).not.toBeInTheDocument()
  })

  it('renders no audio player when the server reports no audio URL', async () => {
    getNoteAudioUrlMock.mockResolvedValueOnce(null)
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('radio', { name: 'Transcript' }))
    await screen.findByText('hello world')
    expect(document.querySelector('audio')).toBeNull()
  })

  it('renders no audio player when the bridge method is missing', async () => {
    const saved = muesliMock.getNoteAudioUrl
    delete (muesliMock as { getNoteAudioUrl?: unknown }).getNoteAudioUrl
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      await userEvent.click(screen.getByRole('radio', { name: 'Transcript' }))
      await screen.findByText('hello world')
      expect(document.querySelector('audio')).toBeNull()
    } finally {
      muesliMock.getNoteAudioUrl = saved
    }
  })

  it('picker is hidden when tab is My notes', async () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('radio', { name: 'My notes' }))
    expect(screen.queryByRole('button', { name: /switch template/i })).not.toBeInTheDocument()
  })
})

// D04 acceptance criteria — picker closes on outside-click and Escape.
describe('NoteView — template-picker dismissal', () => {
  // Note: use direct userEvent.click() (not userEvent.setup()) to avoid
  // userEvent v14's clipboard mock (Object.defineProperty non-configurable)
  // from breaking the "Copy as Markdown" tests that run after this describe.

  it('(i) closes the picker on outside mousedown', async () => {
    vi.useFakeTimers()
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      fireEvent.click(screen.getByRole('button', { name: /switch template/i }))
      await act(async () => {
        vi.runOnlyPendingTimers()
      })
      expect(screen.getByRole('option', { name: 'Action items' })).toBeInTheDocument()
      fireEvent.mouseDown(document.body)
      expect(screen.queryByRole('option', { name: 'Action items' })).not.toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('(ii) closes the picker on Escape keydown', async () => {
    vi.useFakeTimers()
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      fireEvent.click(screen.getByRole('button', { name: /switch template/i }))
      await act(async () => {
        vi.runOnlyPendingTimers()
      })
      expect(screen.getByRole('option', { name: 'Action items' })).toBeInTheDocument()
      fireEvent.keyDown(document, { key: 'Escape' })
      expect(screen.queryByRole('option', { name: 'Action items' })).not.toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('(iii) item pick closes the picker', async () => {
    vi.useFakeTimers()
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      fireEvent.click(screen.getByRole('button', { name: /switch template/i }))
      await act(async () => {
        vi.runOnlyPendingTimers()
      })
      expect(screen.getByRole('option', { name: 'Action items' })).toBeInTheDocument()
      fireEvent.click(screen.getByRole('option', { name: 'Action items' }))
      expect(screen.queryByRole('option', { name: 'Action items' })).not.toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('(iv) click inside the dropdown does NOT close it', async () => {
    vi.useFakeTimers()
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      fireEvent.click(screen.getByRole('button', { name: /switch template/i }))
      await act(async () => {
        vi.runOnlyPendingTimers()
      })
      expect(screen.getByRole('option', { name: 'Action items' })).toBeInTheDocument()
      fireEvent.mouseDown(screen.getByRole('option', { name: 'Action items' }))
      expect(screen.getByRole('option', { name: 'Action items' })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })
})

// USE06: Fix 3 — template picker focus management
describe('NoteView — template picker a11y (USE06)', () => {
  it('opener has aria-haspopup="listbox" and aria-expanded=false before opening', () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    const trigger = screen.getByRole('button', { name: /switch template/i })
    expect(trigger).toHaveAttribute('aria-haspopup', 'listbox')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
  })

  it('after opening, listbox role is in document and first option receives focus', async () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /switch template/i }))
    // Listbox container must be present
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    // First option should be focused (useEffect focuses it after render)
    await waitFor(() => {
      const firstOption = screen.getAllByRole('option')[0]
      expect(firstOption).toHaveFocus()
    })
  })

  it('pressing Escape closes the picker and returns focus to the trigger', async () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    const trigger = screen.getByRole('button', { name: /switch template/i })
    await userEvent.click(trigger)
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    // Picker should close
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    // Focus should return to trigger
    expect(trigger).toHaveFocus()
  })
})

// Fixture whose active (richest) panel has a section citing transcript segment index 3
const fullCited: FullNote = {
  note: { id: '3', title: 'Cited', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: 'raw',
  transcript: { segments: [
    { start_ms: 0,     end_ms: 1000,  text: 'seg zero',  source: 'mixed' },
    { start_ms: 1000,  end_ms: 2000,  text: 'seg one',   source: 'mixed' },
    { start_ms: 2000,  end_ms: 3000,  text: 'seg two',   source: 'mixed' },
    { start_ms: 3000,  end_ms: 4000,  text: 'seg three', source: 'mixed' },
  ] },
  summaries: [
    { template_name: 'Default', status: 'ready', sections: [
      { heading: 'Overview', content_markdown: 'with sources', refs: [3] },
      { heading: 'Plain', content_markdown: 'no sources' },
    ] },
  ],
}

describe('NoteView — summary citations', () => {
  it('renders a clickable Sources chip labelled 1-based for a section with refs', () => {
    render(<NoteView full={fullCited} onSaveBody={async () => {}} />)
    const chip = screen.getByRole('button', { name: /jump to transcript segment 4/i })
    expect(chip).toBeInTheDocument()
    expect(chip).toHaveTextContent('4')
  })

  it('renders no chips for a section without refs', () => {
    render(<NoteView full={fullCited} onSaveBody={async () => {}} />)
    // Only one citation chip total (for the refs:[3] section)
    expect(screen.getAllByRole('button', { name: /jump to transcript segment/i })).toHaveLength(1)
  })

  it('opens the transcript drawer when a citation chip is clicked', async () => {
    render(<NoteView full={fullCited} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /jump to transcript segment 4/i }))
    const transcriptTab = screen.getByRole('radio', { name: 'Transcript' })
    expect(transcriptTab).toHaveAttribute('data-state', 'on')
    expect(screen.getByText('seg three')).toBeInTheDocument()
    // The cited segment (index 3) gets the cited marker
    const cited = document.querySelector('[data-cited="true"]')
    expect(cited).not.toBeNull()
    expect(cited!.textContent).toContain('seg three')
  })
})

describe('summaryToMarkdown', () => {
  it('builds a heading + sections markdown document', () => {
    const md = summaryToMarkdown('Ship v2', [
      { heading: 'Standup', content_markdown: 'do X' },
      { heading: 'Decisions', content_markdown: 'postpone' },
    ])
    expect(md).toContain('# Ship v2')
    expect(md).toContain('## Standup')
    expect(md).toContain('do X')
    expect(md).toContain('## Decisions')
    expect(md).toContain('postpone')
  })

  it('returns just the title when there are no sections', () => {
    expect(summaryToMarkdown('Ship v2', [])).toBe('# Ship v2')
  })
})

describe('NoteView — Copy as Markdown', () => {
  it('copies the active enhanced panel as markdown to the clipboard', async () => {
    vi.useFakeTimers()
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      fireEvent.click(screen.getByRole('button', { name: /copy/i }))
      expect(writeClipboardTextMock).toHaveBeenCalledTimes(1)
      const md = writeClipboardTextMock.mock.calls[0][0]
      expect(typeof md).toBe('string')
      expect(md).toContain('# Standup')
      expect(md).toContain('## Overview')
      expect(md).toContain('Ship v2')
      await act(async () => {
        vi.runOnlyPendingTimers()
      })
    } finally {
      vi.useRealTimers()
    }
  })

  it('copies My notes body as markdown', async () => {
    vi.useFakeTimers()
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      fireEvent.click(screen.getByRole('radio', { name: 'My notes' }))
      fireEvent.click(screen.getByRole('button', { name: /copy/i }))
      const md = writeClipboardTextMock.mock.calls[0][0]
      expect(md).toContain('# Standup')
      expect(md).toContain('my raw notes')
      await act(async () => {
        vi.runOnlyPendingTimers()
      })
    } finally {
      vi.useRealTimers()
    }
  })

  it('copies the transcript as markdown when on the Transcript tab', async () => {
    vi.useFakeTimers()
    try {
      render(<NoteView full={full} onSaveBody={async () => {}} />)
      fireEvent.click(screen.getByRole('radio', { name: 'Transcript' }))
      fireEvent.click(screen.getByRole('button', { name: /copy/i }))
      const md = writeClipboardTextMock.mock.calls[0][0]
      expect(md).toContain('# Standup')
      expect(md).toContain('hello world')
      await act(async () => {
        vi.runOnlyPendingTimers()
      })
    } finally {
      vi.useRealTimers()
    }
  })
})

// DZ03b — Speaker legend / rename UI
const fullNoSpeakers: FullNote = {
  note: { id: 'no-speakers', title: 'Plain', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: { segments: [{ start_ms: 0, end_ms: 1000, text: 'hello world', source: 'mixed' }] },
  summaries: [],
}

const fullWithSpeakers: FullNote = {
  note: { id: 'note-speakers', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: {
    segments: [
      { start_ms: 0,    end_ms: 1000, text: 'hi from one', source: 'mixed', speaker: 'SPEAKER_00' },
      { start_ms: 1000, end_ms: 2000, text: 'hi from two', source: 'mixed', speaker: 'SPEAKER_01' },
      { start_ms: 2000, end_ms: 3000, text: 'one again',   source: 'mixed', speaker: 'SPEAKER_00' },
    ],
  },
  summaries: [],
}

// SPEAKER_00 was already renamed to 'Alice' server-side, so the transcript's
// `speaker` field now shows 'Alice' rather than the raw label. The aliases
// list is the only place the raw label ('SPEAKER_00') is still recoverable.
const fullAlreadyAliased: FullNote = {
  note: { id: 'note-aliased', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: {
    segments: [
      { start_ms: 0,    end_ms: 1000, text: 'hi from alice', source: 'mixed', speaker: 'Alice' },
      { start_ms: 1000, end_ms: 2000, text: 'hi from two',   source: 'mixed', speaker: 'SPEAKER_01' },
    ],
  },
  summaries: [],
}

describe('NoteView — speaker legend (DZ03b)', () => {
  it('is absent when the transcript has no speakers', async () => {
    render(<NoteView full={fullNoSpeakers} onSaveBody={async () => {}} />)
    expect(screen.queryByText('Speakers')).not.toBeInTheDocument()
  })

  it('is absent when the transcript is null', async () => {
    const noTranscript: FullNote = { ...fullNoSpeakers, transcript: null }
    render(<NoteView full={noTranscript} onSaveBody={async () => {}} />)
    expect(screen.queryByText('Speakers')).not.toBeInTheDocument()
  })

  it('renders one row per distinct speaker in first-appearance order', async () => {
    render(<NoteView full={fullWithSpeakers} onSaveBody={async () => {}} />)
    expect(await screen.findByText('Speakers')).toBeInTheDocument()
    const inputs = screen.getAllByRole('textbox').filter((el) => el.getAttribute('aria-label')?.startsWith('Rename'))
    expect(inputs).toHaveLength(2)
    expect(inputs[0]).toHaveValue('SPEAKER_00')
    expect(inputs[1]).toHaveValue('SPEAKER_01')
  })

  it('renaming a never-aliased speaker calls the bridge with the raw label as the key', async () => {
    render(<NoteView full={fullWithSpeakers} onSaveBody={async () => {}} />)
    const input = await screen.findByLabelText('Rename SPEAKER_00')
    await userEvent.clear(input)
    await userEvent.type(input, 'Alice{Enter}')
    await waitFor(() => expect(upsertSpeakerAliasMock).toHaveBeenCalledWith('note-speakers', 'SPEAKER_00', 'Alice'))
  })

  it('re-renaming an already-aliased speaker uses the RAW label (not the displayed alias) as the PUT key', async () => {
    listSpeakerAliasesMock.mockResolvedValue([
      { note_id: 'note-aliased', speaker_label: 'SPEAKER_00', alias_name: 'Alice' },
    ])
    render(<NoteView full={fullAlreadyAliased} onSaveBody={async () => {}} />)
    const input = await screen.findByLabelText('Rename Alice')
    expect(input).toHaveValue('Alice')
    await userEvent.clear(input)
    await userEvent.type(input, 'Bob')
    fireEvent.blur(input)
    await waitFor(() => expect(upsertSpeakerAliasMock).toHaveBeenCalledWith('note-aliased', 'SPEAKER_00', 'Bob'))
  })

  it('after a successful rename, the transcript view shows the new name immediately without a refetch', async () => {
    render(<NoteView full={fullWithSpeakers} onSaveBody={async () => {}} />)
    expect(screen.getAllByText('SPEAKER_00').length).toBeGreaterThan(0)
    const input = await screen.findByLabelText('Rename SPEAKER_00')
    await userEvent.clear(input)
    await userEvent.type(input, 'Alice{Enter}')
    await waitFor(() => expect(upsertSpeakerAliasMock).toHaveBeenCalled())
    // getFull/listSpeakerAliases was not called again to pick this up.
    expect(listSpeakerAliasesMock).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(screen.getAllByText('Alice').length).toBeGreaterThan(0))
    expect(screen.queryByText('hi from one')).toBeInTheDocument()
  })

  it('does not call the bridge when blurring without changing the value', async () => {
    render(<NoteView full={fullWithSpeakers} onSaveBody={async () => {}} />)
    const input = await screen.findByLabelText('Rename SPEAKER_00')
    fireEvent.blur(input)
    await new Promise((r) => setTimeout(r, 0))
    expect(upsertSpeakerAliasMock).not.toHaveBeenCalled()
  })

  it('disables the rename inputs until the alias fetch resolves, then enables them', async () => {
    let resolveAliases: (v: Array<{ note_id: string; speaker_label: string; alias_name: string }>) => void = () => {}
    listSpeakerAliasesMock.mockReturnValue(new Promise((res) => { resolveAliases = res }))

    render(<NoteView full={fullWithSpeakers} onSaveBody={async () => {}} />)

    const input = await screen.findByLabelText('Rename SPEAKER_00')
    expect(input).toBeDisabled()
    expect(screen.getByText('Loading speakers…')).toBeInTheDocument()

    // Blurring/Enter while still loading must not fire a rename, even if some
    // path bypasses the disabled attribute (belt-and-braces `commit` guard).
    fireEvent.keyDown(input, { key: 'Enter' })
    fireEvent.blur(input)
    expect(upsertSpeakerAliasMock).not.toHaveBeenCalled()

    resolveAliases([])
    await waitFor(() => expect(input).toBeEnabled())
    expect(screen.queryByText('Loading speakers…')).not.toBeInTheDocument()
  })

  it('shows an inline error when the rename fails, without silently reverting', async () => {
    upsertSpeakerAliasMock.mockRejectedValue(new Error('server exploded'))
    render(<NoteView full={fullWithSpeakers} onSaveBody={async () => {}} />)
    const input = await screen.findByLabelText('Rename SPEAKER_00')
    await userEvent.clear(input)
    await userEvent.type(input, 'Alice{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('server exploded')
  })
})

describe('NoteView — truncation badge', () => {
  it('shows a "May be truncated" badge when the active summary is flagged truncated', () => {
    const truncatedFull: FullNote = {
      ...fullSingle,
      summaries: [
        { template_name: 'Default', status: 'ready', truncated: true, sections: [{ heading: 'Summary', content_markdown: 'Only content' }] },
      ],
    }
    render(<NoteView full={truncatedFull} onSaveBody={async () => {}} />)
    expect(screen.getByText('May be truncated')).toBeInTheDocument()
  })

  it('does NOT show the truncation badge when the summary is not flagged truncated', () => {
    render(<NoteView full={fullSingle} onSaveBody={async () => {}} />)
    expect(screen.queryByText('May be truncated')).not.toBeInTheDocument()
  })
})

// TPL01 — full template list (all owner-visible templates, not just ones with
// an existing summary) + per-template Regenerate.
const tplA: Template = { id: 'tpl-a', name: 'Action items', phase: 'after', sections: [], built_in: true, auto_run: true }
const tplB: Template = { id: 'tpl-b', name: 'General meeting', phase: 'after', sections: [], built_in: true, auto_run: true }
const tplC: Template = { id: 'tpl-c', name: 'Retro', phase: 'cross', sections: [], built_in: false, auto_run: true }

const fullWithTemplateIds: FullNote = {
  note: { id: '4', title: 'Planning', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: 'raw',
  transcript: { segments: [{ start_ms: 0, end_ms: 1000, text: 'hello', source: 'mixed' }] },
  summaries: [
    { id: 's-a', template_id: 'tpl-a', template_name: 'Action items', status: 'ready', sections: [{ heading: 'Actions', content_markdown: 'do X' }] },
    { id: 's-b', template_id: 'tpl-b', template_name: 'General meeting', status: 'ready', sections: [
      { heading: 'Overview', content_markdown: 'Ship v2' },
      { heading: 'Decisions', content_markdown: 'postpone' },
    ] },
  ],
}

describe('NoteView — full template list + regenerate (TPL01)', () => {
  it('lists a template with no existing summary as "Not generated" in the picker', async () => {
    render(<NoteView full={fullWithTemplateIds} templates={[tplA, tplB, tplC]} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /switch template/i }))
    const retroOption = screen.getByRole('option', { name: /Retro/ })
    expect(retroOption).toHaveTextContent('Not generated')
  })

  it('selecting a not-yet-generated template shows the "No summary yet." empty state', async () => {
    render(<NoteView full={fullWithTemplateIds} templates={[tplA, tplB, tplC]} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /switch template/i }))
    await userEvent.click(screen.getByRole('option', { name: /Retro/ }))
    expect(screen.getByText('No summary yet.')).toBeInTheDocument()
  })

  it('shows the no-agent empty state and opens transcript when no template has ever summarized', async () => {
    render(<NoteView full={fullNoSummaries} onSaveBody={async () => {}} />)

    await userEvent.click(screen.getByRole('radio', { name: 'Enhanced' }))
    expect(screen.getByText('No AI summary yet')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Install Ollama' })).toHaveAttribute('href', 'https://ollama.com/download')
    expect(screen.queryByText('No summary yet.')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'View transcript' }))
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')
  })

  it('keeps the plain empty state when processing is still in progress', () => {
    const inProgress: FullNote = {
      ...fullNoSummaries,
      note: { ...fullNoSummaries.note, status: 'summarizing' },
    }

    render(<NoteView full={inProgress} onSaveBody={async () => {}} />)

    fireEvent.click(screen.getByRole('radio', { name: 'Enhanced' }))
    expect(screen.getByText('No summary yet.')).toBeInTheDocument()
    expect(screen.queryByText(/No AI summary yet/)).not.toBeInTheDocument()
  })

  it('renders a Regenerate control for the currently-selected template', () => {
    render(<NoteView full={fullWithTemplateIds} templates={[tplA, tplB, tplC]} onRegenerateTemplate={vi.fn()} onSaveBody={async () => {}} />)
    expect(screen.getByRole('button', { name: /regenerate/i })).toBeInTheDocument()
  })

  it('clicking Regenerate invokes onRegenerateTemplate with the selected templateId', async () => {
    const onRegenerate = vi.fn()
    render(<NoteView full={fullWithTemplateIds} templates={[tplA, tplB, tplC]} onRegenerateTemplate={onRegenerate} onSaveBody={async () => {}} />)
    // Default selection is the richest panel (General meeting / tpl-b).
    await userEvent.click(screen.getByRole('button', { name: /regenerate/i }))
    expect(onRegenerate).toHaveBeenCalledWith('tpl-b')
  })

  it('disables the Regenerate control while its template is in flight, and re-enables once cleared', () => {
    const { rerender } = render(
      <NoteView full={fullWithTemplateIds} templates={[tplA, tplB, tplC]} onRegenerateTemplate={vi.fn()} regeneratingTemplateId="tpl-b" onSaveBody={async () => {}} />,
    )
    const btn = screen.getByRole('button', { name: /regenerate/i })
    expect(btn).toBeDisabled()
    expect(btn).toHaveAttribute('aria-busy', 'true')

    rerender(
      <NoteView full={fullWithTemplateIds} templates={[tplA, tplB, tplC]} onRegenerateTemplate={vi.fn()} regeneratingTemplateId={null} onSaveBody={async () => {}} />,
    )
    expect(screen.getByRole('button', { name: /regenerate/i })).not.toBeDisabled()
  })

  it('does not render Regenerate when no onRegenerateTemplate handler is given (back-compat)', () => {
    render(<NoteView full={full} onSaveBody={async () => {}} />)
    expect(screen.queryByRole('button', { name: /regenerate/i })).not.toBeInTheDocument()
  })
})

// TPL01 — the reorder race this REDO exists to fix. NoteScreen fetches the
// owner's templates asynchronously via listTemplates() in a useEffect that
// resolves AFTER first paint (`templates` starts as `[]`/absent). Before it
// resolves, NoteView falls back to one entry per existing summary, ordered by
// GetSummaries's SQL (`ORDER BY t.name` — plain template-name order). Once
// listTemplates() resolves, entries are recomputed from ListTemplates's order
// (`(owner_id IS NULL) DESC, lower(name)` — built-ins first, then alphabetical
// by lowercased name). These orderings differ whenever a user template
// alphabetically precedes a built-in (as here: "Zeta retro" sorts after both
// built-ins by plain name, but the *user* template shows first in the
// summary-fallback GetSummaries order because... — the key point is the two
// orderings simply disagree, see fixture below). Selection must be tracked by
// template identity, not raw array index, so it isn't silently swapped out
// from under the user when the async list arrives.
describe('NoteView — selection survives the async-template-list reorder race (TPL01)', () => {
  // Summary-fallback order (GetSummaries: ORDER BY t.name, case-sensitive
  // plain name) sorts "Zebra notes" (user template, capital Z) before
  // "general update" (built-in, lowercase g) — capital letters sort before
  // lowercase in a plain byte/name comparison. But ListTemplates orders by
  // `(owner_id IS NULL) DESC, lower(name)`, which puts the built-in first
  // (built-ins always win the DESC tiebreak) and then "general update" sorts
  // before "zebra notes" alphabetically. So the two orderings disagree, and
  // the entry the user had selected (by position) moves.
  const tplUser: Template = { id: 'tpl-user', name: 'Zebra notes', phase: 'during', sections: [], built_in: false, auto_run: true }
  const tplBuiltin: Template = { id: 'tpl-builtin', name: 'general update', phase: 'after', sections: [], built_in: true, auto_run: true }

  const fullRace: FullNote = {
    note: { id: '5', title: 'Race', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    body_markdown: 'raw',
    transcript: { segments: [{ start_ms: 0, end_ms: 1000, text: 'hello', source: 'mixed' }] },
    summaries: [
      { id: 's-user', template_id: 'tpl-user', template_name: 'Zebra notes', status: 'ready', sections: [{ heading: 'A', content_markdown: 'user summary' }] },
      { id: 's-builtin', template_id: 'tpl-builtin', template_name: 'general update', status: 'ready', sections: [{ heading: 'B', content_markdown: 'builtin summary' }] },
    ],
  }

  it('keeps the SAME template selected (by identity) after templates arrive in a different order', async () => {
    // First paint: no `templates` prop yet (matches NoteScreen's pre-resolve
    // state), so entries fall back to summary order: [Zebra notes, general
    // update] (both single-section, so default picks the first — Zebra notes).
    const { rerender } = render(<NoteView full={fullRace} onSaveBody={async () => {}} />)
    expect(screen.getByText('user summary')).toBeInTheDocument()
    expect(screen.queryByText('builtin summary')).not.toBeInTheDocument()

    // User explicitly switches to "general update".
    await userEvent.click(screen.getByRole('button', { name: /switch template/i }))
    await userEvent.click(screen.getByRole('option', { name: /general update/ }))
    expect(screen.getByText('builtin summary')).toBeInTheDocument()
    expect(screen.queryByText('user summary')).not.toBeInTheDocument()

    // Now the async listTemplates() resolves, supplying `templates` in
    // ListTemplates order: built-in first, then alphabetical — i.e. REVERSED
    // relative to the summary-fallback order above.
    rerender(<NoteView full={fullRace} templates={[tplBuiltin, tplUser]} onSaveBody={async () => {}} />)

    // The BUG: `selected` was a raw index (1), which pointed at "general
    // update" before but now points at "Zebra notes" post-reorder. The FIX:
    // selection is tracked by templateId, so it must still show "general
    // update" — not silently flip back to "Zebra notes".
    expect(screen.getByText('builtin summary')).toBeInTheDocument()
    expect(screen.queryByText('user summary')).not.toBeInTheDocument()
    const picker = screen.getByRole('button', { name: /switch template/i })
    expect(picker).toHaveTextContent('general update')
  })

  it('Regenerate still targets the user-selected template after the reorder', async () => {
    const onRegenerate = vi.fn()
    const { rerender } = render(<NoteView full={fullRace} onRegenerateTemplate={onRegenerate} onSaveBody={async () => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /switch template/i }))
    await userEvent.click(screen.getByRole('option', { name: /general update/ }))

    rerender(<NoteView full={fullRace} templates={[tplBuiltin, tplUser]} onRegenerateTemplate={onRegenerate} onSaveBody={async () => {}} />)

    await userEvent.click(screen.getByRole('button', { name: /regenerate/i }))
    expect(onRegenerate).toHaveBeenCalledWith('tpl-builtin')
  })

  it('falls back to the richest-summary default when the previously-selected template is no longer present', () => {
    // Selection was never made (defaults to richest — here both fixtures tie
    // on section count, so the first entry, "Zebra notes"/tpl-user, wins).
    // If the new `templates` list no longer contains tpl-user at all, the
    // fallback should re-derive a sane default from the new entries rather
    // than pointing at nothing.
    const { rerender } = render(<NoteView full={fullRace} onSaveBody={async () => {}} />)
    expect(screen.getByText('user summary')).toBeInTheDocument()

    rerender(<NoteView full={fullRace} templates={[tplBuiltin]} onSaveBody={async () => {}} />)
    expect(screen.getByText('builtin summary')).toBeInTheDocument()
  })
})

// SRC01b — jump-to-timestamp from a transcript search hit.
describe('NoteView — initialSegmentId (search hit jump-to-timestamp)', () => {
  const fullForJump: FullNote = {
    note: { id: 'note-jump', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    body_markdown: '',
    transcript: {
      segments: [
        { id: 'seg-a', start_ms: 0, end_ms: 1000, text: 'first segment', source: 'mixed' },
        { id: 'seg-b', start_ms: 1000, end_ms: 2000, text: 'second segment', source: 'mixed' },
        { id: 'seg-c', start_ms: 2000, end_ms: 3000, text: 'third segment', source: 'mixed' },
      ],
    },
    summaries: [],
  }

  it('opens the transcript panel and highlights the segment matching initialSegmentId', async () => {
    render(<NoteView full={fullForJump} initialSegmentId="seg-b" onSaveBody={async () => {}} />)

    // The transcript tab opens automatically (no manual click needed).
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')

    // The segment whose id matches is resolved to its 0-based array index (1)
    // and highlighted via TranscriptView's existing citation mechanism.
    const cited = document.querySelector('[data-cited="true"]')
    expect(cited).not.toBeNull()
    expect(cited!.textContent).toContain('second segment')
    expect(document.querySelectorAll('[data-cited="true"]')).toHaveLength(1)
  })

  it('does nothing when initialSegmentId does not match any segment', () => {
    render(<NoteView full={fullForJump} initialSegmentId="seg-does-not-exist" onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')
  })

  it('does nothing when initialSegmentId is absent', () => {
    render(<NoteView full={fullForJump} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')
  })

  it('only jumps once — closing the transcript panel afterwards does not reopen it on re-render', async () => {
    const { rerender } = render(<NoteView full={fullForJump} initialSegmentId="seg-a" onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')

    await userEvent.click(screen.getByRole('radio', { name: 'Enhanced' }))
    expect(screen.getByText('No AI summary yet')).toBeInTheDocument()

    // Re-rendering with the same props (e.g. a parent re-render) must not
    // reopen the panel — the jump already happened once.
    rerender(<NoteView full={fullForJump} initialSegmentId="seg-a" onSaveBody={async () => {}} />)
    expect(screen.getByText('No AI summary yet')).toBeInTheDocument()
  })
})

// CHT06 -- jump-to-segment from a chat citation chip. Unlike initialSegmentId
// (a segment id resolved by lookup), initialSegmentIndex is already a
// resolved 0-based transcript-segment array index (chat.Source.segment_index
// mirrors TranscriptRef.SegmentIndex directly).
describe('NoteView — initialSegmentIndex (chat citation jump-to-segment)', () => {
  const fullA: FullNote = {
    note: { id: 'note-a', title: 'Note A', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    body_markdown: '',
    transcript: {
      segments: [
        { id: 'a-seg-0', start_ms: 0, end_ms: 1000, text: 'note A segment zero', source: 'mixed' },
        { id: 'a-seg-1', start_ms: 1000, end_ms: 2000, text: 'note A segment one', source: 'mixed' },
      ],
    },
    summaries: [],
  }
  const fullB: FullNote = {
    note: { id: 'note-b', title: 'Note B', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    body_markdown: '',
    transcript: {
      segments: [
        { id: 'b-seg-0', start_ms: 0, end_ms: 1000, text: 'note B segment zero', source: 'mixed' },
        { id: 'b-seg-1', start_ms: 1000, end_ms: 2000, text: 'note B segment one', source: 'mixed' },
      ],
    },
    summaries: [],
  }

  it('opens the transcript panel and highlights the segment at initialSegmentIndex', async () => {
    render(<NoteView full={fullA} initialSegmentIndex={1} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')
    const cited = document.querySelector('[data-cited="true"]')
    expect(cited).not.toBeNull()
    expect(cited!.textContent).toContain('note A segment one')
  })

  it('does nothing when initialSegmentIndex is absent or negative', () => {
    const { rerender } = render(<NoteView full={fullA} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')

    rerender(<NoteView full={fullA} initialSegmentIndex={-1} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')
  })

  it('only jumps once -- re-rendering with the same note + index afterwards does not reopen a closed panel', async () => {
    const { rerender } = render(<NoteView full={fullA} initialSegmentIndex={0} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')

    await userEvent.click(screen.getByRole('radio', { name: 'Enhanced' }))
    expect(screen.getByText('No AI summary yet')).toBeInTheDocument()

    rerender(<NoteView full={fullA} initialSegmentIndex={0} onSaveBody={async () => {}} />)
    expect(screen.getByText('No AI summary yet')).toBeInTheDocument()
  })

  // Regression for the cross-note suppression bug: react-router does not
  // remount NoteScreen/NoteView on a `:id`-only route change, so this same
  // component instance (and its "already consumed" ref) persists across a
  // navigation from note A to a DIFFERENT note B triggered by a second
  // global-chat citation chip. A naive ref keyed on the index value alone
  // would treat note B's identical segment_index as already-consumed and
  // silently suppress the second jump -- this asserts the jump still fires
  // for note B.
  it('still jumps in a different note that reuses the same segment_index after a jump was already consumed in a prior note', async () => {
    const { rerender } = render(<NoteView full={fullA} initialSegmentIndex={0} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')
    let cited = document.querySelector('[data-cited="true"]')
    expect(cited!.textContent).toContain('note A segment zero')

    await userEvent.click(screen.getByRole('radio', { name: 'Enhanced' }))
    expect(screen.getByText('No AI summary yet')).toBeInTheDocument()

    // Simulate navigating to a DIFFERENT note (via ChatScreen's onCiteClick)
    // whose citation happens to reuse the SAME segment_index (0).
    rerender(<NoteView full={fullB} initialSegmentIndex={0} onSaveBody={async () => {}} />)
    expect(screen.getByRole('radio', { name: 'Transcript' })).toHaveAttribute('data-state', 'on')
    cited = document.querySelector('[data-cited="true"]')
    expect(cited).not.toBeNull()
    expect(cited!.textContent).toContain('note B segment zero')
  })
})

describe('NoteView — chat entry point (CHT05)', () => {
  it('opens the "Ask about this note" panel from the toolbar, scoped to this note', async () => {
    render(<NoteView full={fullSingle} onSaveBody={async () => {}} />)
    expect(screen.queryByText('Ask about this note')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /ask about this note/i }))
    expect(await screen.findByText('Ask about this note')).toBeInTheDocument()
    await waitFor(() => expect(listConversationsMock).toHaveBeenCalledWith(fullSingle.note.id))

    await userEvent.click(screen.getByRole('button', { name: /close chat/i }))
    expect(screen.queryByText('Ask about this note')).not.toBeInTheDocument()
  })
})
