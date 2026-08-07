// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ExportOptionsDialog } from './ExportOptionsDialog'

const formats = [
  { value: 'markdown', label: 'Markdown' },
  { value: 'pdf', label: 'PDF' },
]

function renderDialog(overrides: Partial<React.ComponentProps<typeof ExportOptionsDialog>> = {}) {
  const props: React.ComponentProps<typeof ExportOptionsDialog> = {
    open: true,
    title: 'Export note',
    formats,
    initialFormat: 'pdf',
    confirmLabel: 'Export',
    onCancel: vi.fn(),
    onConfirm: vi.fn(),
    ...overrides,
  }
  return { ...render(<ExportOptionsDialog {...props} />), props }
}

afterEach(cleanup)

describe('ExportOptionsDialog', () => {
  it('uses the requested format and default option selections on open', () => {
    renderDialog()
    expect(screen.getByLabelText('Export format')).toHaveValue('pdf')
    expect(screen.getByLabelText('Include transcript')).toBeChecked()
    expect(screen.getByLabelText('Redact speaker names')).not.toBeChecked()
  })

  it('falls back to the first format when initialFormat is unavailable', () => {
    renderDialog({ initialFormat: 'docx' })
    expect(screen.getByLabelText('Export format')).toHaveValue('markdown')
  })

  it('submits exactly the selected and toggled options', async () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    renderDialog({ onConfirm, onCancel })

    await userEvent.selectOptions(screen.getByLabelText('Export format'), 'markdown')
    await userEvent.click(screen.getByLabelText('Include transcript'))
    await userEvent.click(screen.getByLabelText('Redact speaker names'))
    await userEvent.click(screen.getByRole('button', { name: 'Export' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledExactlyOnceWith({
      format: 'markdown',
      includeTranscript: false,
      redactSpeakers: true,
    }))
    await waitFor(() => expect(onCancel).toHaveBeenCalledOnce())
  })

  it('cancels without submitting', async () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    renderDialog({ onConfirm, onCancel })
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalledOnce()
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('stays open and becomes usable again when confirmation rejects', async () => {
    const onConfirm = vi.fn().mockRejectedValue(new Error('export failed'))
    const onCancel = vi.fn()
    renderDialog({ onConfirm, onCancel })

    await userEvent.click(screen.getByRole('button', { name: 'Export' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledOnce())
    await waitFor(() => expect(screen.getByRole('button', { name: 'Export' })).toBeEnabled())
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(onCancel).not.toHaveBeenCalled()
  })
})
