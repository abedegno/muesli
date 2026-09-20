// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CrossAnalysisComposer } from './CrossAnalysisComposer'
import type { Note, Template } from '../../../shared/types'

afterEach(cleanup)

function note(overrides: Partial<Note>): Note {
  return {
    id: 'note-x',
    title: 'Untitled',
    status: 'ready',
    partial_transcript: false,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  }
}

function template(overrides: Partial<Template>): Template {
  return {
    id: 'tmpl-x',
    name: 'Untitled template',
    phase: 'after',
    sections: [],
    built_in: false,
    auto_run: false,
    ...overrides,
  }
}

const crossTemplate = template({ id: 'tmpl-cross', name: 'Cross recap', phase: 'cross' })
const afterTemplate = template({ id: 'tmpl-after', name: 'After summary', phase: 'after' })

const readyA = note({ id: 'note-a', title: 'Sprint planning' })
const readyB = note({ id: 'note-b', title: 'Retro' })
const notReady = note({ id: 'note-c', title: 'In progress', status: 'transcribing' })
const partial = note({ id: 'note-d', title: 'Cut short', partial_transcript: true })

describe('CrossAnalysisComposer', () => {
  it('shows only cross-phase templates, never other phases', () => {
    render(<CrossAnalysisComposer templates={[crossTemplate, afterTemplate]} notes={[readyA, readyB]} sending={false} onSend={vi.fn()} />)
    const select = screen.getByLabelText('Cross-analysis template') as HTMLSelectElement
    const optionLabels = Array.from(select.options).map((o) => o.textContent)
    expect(optionLabels).toContain('Cross recap')
    expect(optionLabels).not.toContain('After summary')
  })

  it('hides ineligible notes (not ready, or partial) from the picker entirely', () => {
    render(
      <CrossAnalysisComposer templates={[crossTemplate]} notes={[readyA, readyB, notReady, partial]} sending={false} onSend={vi.fn()} />,
    )
    const list = screen.getByRole('listbox', { name: /eligible meetings/i })
    expect(within(list).getByText('Sprint planning')).toBeInTheDocument()
    expect(within(list).getByText('Retro')).toBeInTheDocument()
    expect(within(list).queryByText('In progress')).not.toBeInTheDocument()
    expect(within(list).queryByText('Cut short')).not.toBeInTheDocument()
  })

  it('filters the note list by title as the user types', async () => {
    const user = userEvent.setup()
    render(<CrossAnalysisComposer templates={[crossTemplate]} notes={[readyA, readyB]} sending={false} onSend={vi.fn()} />)
    const list = screen.getByRole('listbox', { name: /eligible meetings/i })
    expect(within(list).getByText('Retro')).toBeInTheDocument()

    await user.type(screen.getByLabelText('Filter meetings'), 'sprint')
    expect(within(list).getByText('Sprint planning')).toBeInTheDocument()
    expect(within(list).queryByText('Retro')).not.toBeInTheDocument()
  })

  it('"Select all" resolves only the CURRENTLY DISPLAYED filtered notes, not every eligible note', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn().mockResolvedValue(true)
    render(<CrossAnalysisComposer templates={[crossTemplate]} notes={[readyA, readyB]} sending={false} onSend={onSend} />)

    await user.type(screen.getByLabelText('Filter meetings'), 'sprint')
    await user.click(screen.getByRole('button', { name: /select all/i }))
    await user.selectOptions(screen.getByLabelText('Cross-analysis template'), 'tmpl-cross')

    // Only one note matches the filter, so "Run analysis" stays disabled
    // (below the 2-note minimum) -- proving select-all did NOT pull in
    // Retro, which the filter had hidden.
    expect(screen.getByRole('button', { name: /run analysis/i })).toBeDisabled()
  })

  it('disables "Run analysis" until a template and at least two notes are selected, then submits the resolved id list plus trimmed focus', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn().mockResolvedValue(true)
    render(<CrossAnalysisComposer templates={[crossTemplate]} notes={[readyA, readyB]} sending={false} onSend={onSend} />)

    const runButton = screen.getByRole('button', { name: /run analysis/i })
    expect(runButton).toBeDisabled()

    await user.click(screen.getByRole('checkbox', { name: 'Sprint planning' }))
    expect(runButton).toBeDisabled() // only one note selected

    await user.click(screen.getByRole('checkbox', { name: 'Retro' }))
    expect(runButton).toBeDisabled() // no template yet

    await user.selectOptions(screen.getByLabelText('Cross-analysis template'), 'tmpl-cross')
    expect(runButton).toBeEnabled()

    await user.type(screen.getByLabelText('Cross-analysis focus'), '  Compare decisions  ')
    await user.click(runButton)

    expect(onSend).toHaveBeenCalledTimes(1)
    const [templateId, noteIds, focus] = onSend.mock.calls[0]
    expect(templateId).toBe('tmpl-cross')
    expect(new Set(noteIds)).toEqual(new Set(['note-a', 'note-b']))
    expect(focus).toBe('Compare decisions')
  })

  it('disables the submit button and every input while sending (existing send-lock state)', () => {
    render(<CrossAnalysisComposer templates={[crossTemplate]} notes={[readyA, readyB]} sending={true} onSend={vi.fn()} />)
    expect(screen.getByRole('button', { name: /running analysis/i })).toBeDisabled()
    expect(screen.getByLabelText('Cross-analysis template')).toBeDisabled()
    expect(screen.getByLabelText('Filter meetings')).toBeDisabled()
  })

  it('does not allow selecting beyond the shared maximum of 40 notes', async () => {
    const user = userEvent.setup()
    const manyNotes = Array.from({ length: 41 }, (_, i) => note({ id: `note-${i}`, title: `Meeting ${i}` }))
    render(<CrossAnalysisComposer templates={[crossTemplate]} notes={manyNotes} sending={false} onSend={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: /select all/i }))
    expect(screen.getByText('Meetings (40/40)')).toBeInTheDocument()
  })
})
