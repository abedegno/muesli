// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FilenameTemplateInput } from './FilenameTemplateInput'

afterEach(cleanup)

describe('FilenameTemplateInput', () => {
  it('renders a text input with the supplied value', () => {
    render(<FilenameTemplateInput value="{title} - {date}" onChange={() => {}} />)
    const input = screen.getByLabelText('Filename template') as HTMLInputElement
    expect(input.value).toBe('{title} - {date}')
  })

  it('renders a preview with dummy context values', () => {
    render(<FilenameTemplateInput value="{title} - {date}" onChange={() => {}} />)
    const preview = screen.getByLabelText('Preview')
    // Dummy context: date='2024-01-15', title='Sample Meeting'
    expect(preview.textContent).toContain('Sample Meeting - 2024-01-15')
  })

  it('does not show an error for a valid template', () => {
    render(<FilenameTemplateInput value="{title} - {date}" onChange={() => {}} />)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a validation error for an unknown placeholder', () => {
    render(<FilenameTemplateInput value="{title} - {foo}" onChange={() => {}} />)
    const alert = screen.getByRole('alert')
    expect(alert).toBeInTheDocument()
    expect(alert.textContent).toMatch(/\{foo\}/)
  })

  it('shows a validation error for an empty template', () => {
    render(<FilenameTemplateInput value="   " onChange={() => {}} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('calls onChange when the input value changes', async () => {
    const onChange = vi.fn()
    render(<FilenameTemplateInput value="" onChange={onChange} />)
    const input = screen.getByLabelText('Filename template')
    await userEvent.type(input, 'a')
    expect(onChange).toHaveBeenCalled()
    expect(onChange).toHaveBeenCalledWith(expect.stringContaining('a'))
  })

  it('updates the preview when value changes', () => {
    const { rerender } = render(
      <FilenameTemplateInput value="{title}" onChange={() => {}} />,
    )
    expect(screen.getByLabelText('Preview').textContent).toContain('Sample Meeting')

    rerender(<FilenameTemplateInput value="{date}" onChange={() => {}} />)
    expect(screen.getByLabelText('Preview').textContent).toContain('2024-01-15')
  })

  it('accepts an optional template prop without error', () => {
    // The template prop is part of the public API even if not used for preview
    expect(() =>
      render(
        <FilenameTemplateInput
          value="{title}"
          template="{title}"
          onChange={() => {}}
        />,
      ),
    ).not.toThrow()
  })
})
