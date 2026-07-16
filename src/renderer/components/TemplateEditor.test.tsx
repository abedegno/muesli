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
    await userEvent.click(screen.getByLabelText('Auto-run on new notes'))
    await userEvent.type(screen.getByLabelText('Section 1 heading'), 'Action items')
    await userEvent.type(screen.getByLabelText('Section 1 instruction'), 'List them')
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }))
    expect(onSave).toHaveBeenCalledWith('Standup', 'pre', [
      { heading: 'Action items', instruction: 'List them' },
    ], false)
  })

  it('disables Save when the name exceeds 80 characters', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateEditor
        open
        title="New template"
        initial={{
          id: 'template-1',
          built_in: false,
          name: 'a'.repeat(81),
          phase: 'after',
          auto_run: true,
          sections: [{ heading: 'Heading', instruction: 'Instruction' }],
        }}
        onSave={onSave}
        onClose={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
  })

  it('disables Save when a section instruction exceeds 500 characters', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateEditor
        open
        title="New template"
        initial={{
          id: 'template-1',
          built_in: false,
          name: 'Standup',
          phase: 'after',
          auto_run: true,
          sections: [{ heading: 'Heading', instruction: 'a'.repeat(501) }],
        }}
        onSave={onSave}
        onClose={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
  })

  it('disables "+ Add section" at 12 sections', async () => {
    render(<TemplateEditor open title="New template" onSave={vi.fn()} onClose={() => {}} />)
    const add = screen.getByRole('button', { name: /add section/i })

    for (let i = 0; i < 11; i += 1) {
      await userEvent.click(add)
    }

    expect(add).toBeDisabled()
    expect(screen.getByText('Maximum 12 sections')).toBeInTheDocument()

    await userEvent.click(add)
    expect(screen.queryByLabelText('Section 13 heading')).toBeNull()
  })

  it('name input has maxLength 80', () => {
    render(<TemplateEditor open title="New template" onSave={vi.fn()} onClose={() => {}} />)
    expect((screen.getByLabelText('Template name') as HTMLInputElement).maxLength).toBe(80)
  })

  it('section heading input has maxLength 80', () => {
    render(<TemplateEditor open title="New template" onSave={vi.fn()} onClose={() => {}} />)
    expect((screen.getByLabelText('Section 1 heading') as HTMLInputElement).maxLength).toBe(80)
  })

  it('section instruction textarea has maxLength 500', () => {
    render(<TemplateEditor open title="New template" onSave={vi.fn()} onClose={() => {}} />)
    expect((screen.getByLabelText('Section 1 instruction') as HTMLTextAreaElement).maxLength).toBe(500)
  })
})
