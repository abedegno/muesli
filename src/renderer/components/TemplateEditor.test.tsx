// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TemplateEditor } from './TemplateEditor'

afterEach(cleanup)

describe('TemplateEditor', () => {
  it('disables save until name and section are filled, then enables it', async () => {
    render(<TemplateEditor open title="New template" onSave={vi.fn()} onClose={() => {}} />)
    const save = screen.getByRole('button', { name: /^save$/i })
    expect(save).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Template name'), 'Standup')
    await userEvent.type(screen.getByLabelText('Section 1 heading'), 'Action items')
    await userEvent.type(screen.getByLabelText('Section 1 instruction'), 'List the action items')
    expect(save).toBeEnabled()
  })

  it('adds and removes section rows', async () => {
    render(<TemplateEditor open title="New template" onSave={vi.fn()} onClose={() => {}} />)
    expect(screen.queryByLabelText('Section 2 heading')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /add section/i }))
    expect(screen.getByLabelText('Section 2 heading')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Remove section 2' }))
    expect(screen.queryByLabelText('Section 2 heading')).toBeNull()
  })

  it('reorders sections with move down', async () => {
    render(<TemplateEditor open title="New template" onSave={vi.fn()} onClose={() => {}} />)
    await userEvent.type(screen.getByLabelText('Section 1 heading'), 'First')
    await userEvent.type(screen.getByLabelText('Section 1 instruction'), 'a')
    await userEvent.click(screen.getByRole('button', { name: /add section/i }))
    await userEvent.type(screen.getByLabelText('Section 2 heading'), 'Second')
    await userEvent.type(screen.getByLabelText('Section 2 instruction'), 'b')
    await userEvent.click(screen.getAllByRole('button', { name: 'Move down' })[0])
    expect((screen.getByLabelText('Section 1 heading') as HTMLInputElement).value).toBe('Second')
    expect((screen.getByLabelText('Section 2 heading') as HTMLInputElement).value).toBe('First')
  })

  it('saves trimmed name and sections', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<TemplateEditor open title="New template" onSave={onSave} onClose={() => {}} />)
    await userEvent.type(screen.getByLabelText('Template name'), '  Standup  ')
    await userEvent.selectOptions(screen.getByLabelText('Phase'), 'pre')
    await userEvent.type(screen.getByLabelText('Section 1 heading'), 'Action items')
    await userEvent.type(screen.getByLabelText('Section 1 instruction'), 'List them')
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }))
    expect(onSave).toHaveBeenCalledWith('Standup', 'pre', [
      { heading: 'Action items', instruction: 'List them' },
    ])
  })
})
