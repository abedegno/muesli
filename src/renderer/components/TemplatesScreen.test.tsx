// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ToastProvider } from '@/components/ui/Toast'
import type { Template } from '../../shared/types'

const listTemplates = vi.hoisted(() => vi.fn())
const deleteTemplate = vi.hoisted(() => vi.fn())
const updateTemplate = vi.hoisted(() => vi.fn())
const createTemplate = vi.hoisted(() => vi.fn())

const builtIn: Template = {
  id: 'b1',
  name: 'General',
  phase: 'after',
  sections: [{ heading: 'Summary', instruction: 'Summarize' }],
  built_in: true,
  auto_run: true,
}
const custom: Template = {
  id: 'c1',
  name: 'My Standup',
  phase: 'pre',
  sections: [{ heading: 'Updates', instruction: 'List updates' }],
  built_in: false,
  auto_run: true,
}

vi.mock('@/api', () => ({
  muesli: {
    listTemplates: () => listTemplates(),
    deleteTemplate: (id: string) => deleteTemplate(id),
    createTemplate: (...args: Parameters<typeof createTemplate>) => createTemplate(...args),
    updateTemplate: (...args: Parameters<typeof updateTemplate>) => updateTemplate(...args),
  },
}))

import { TemplatesScreen } from './TemplatesScreen'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

beforeEach(() => {
  listTemplates.mockResolvedValue([builtIn, custom])
  deleteTemplate.mockResolvedValue(undefined)
  updateTemplate.mockResolvedValue(undefined)
  createTemplate.mockResolvedValue(undefined)
})

function renderScreen() {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <TemplatesScreen />
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('TemplatesScreen', () => {
  it('shows a Built-in badge and no delete for built-in templates', async () => {
    renderScreen()
    const name = await screen.findByText('General')
    // the badge sits next to the template name within the same row
    const row = name.closest('li')!
    expect(row).toHaveTextContent('Built-in')
    expect(row).toHaveTextContent('After')
    expect(screen.queryByRole('button', { name: 'Delete General' })).toBeNull()
  })

  it('shows the phase badge for custom templates', async () => {
    renderScreen()
    const name = await screen.findByText('My Standup')
    const row = name.closest('li')!
    expect(row).toHaveTextContent('Pre')
  })

  it('shows the auto-run state for custom templates', async () => {
    renderScreen()
    const name = await screen.findByText('My Standup')
    const row = name.closest('li')!
    expect(row).toHaveTextContent('Auto-run')
  })

  it('shows a Delete button for custom templates and deletes on click', async () => {
    renderScreen()
    // Step 1: click the Delete button to open the dialog
    const del = await screen.findByRole('button', { name: 'Delete My Standup' })
    await userEvent.click(del)
    // Step 2: click the destructive Delete button inside the dialog
    const confirmBtn = await screen.findByRole('button', { name: 'Delete' })
    await userEvent.click(confirmBtn)
    await waitFor(() => expect(deleteTemplate).toHaveBeenCalledWith('c1'))
  })

  it('Cancel aborts deletion', async () => {
    renderScreen()
    // Click the Delete button to open the dialog
    const del = await screen.findByRole('button', { name: 'Delete My Standup' })
    await userEvent.click(del)
    // Click Cancel inside the dialog
    const cancelBtn = await screen.findByRole('button', { name: 'Cancel' })
    await userEvent.click(cancelBtn)
    // deleteTemplate should NOT have been called
    expect(deleteTemplate).not.toHaveBeenCalled()
  })

  it('Confirm deletes the template', async () => {
    renderScreen()
    // Click the Delete button to open the dialog
    const del = await screen.findByRole('button', { name: 'Delete My Standup' })
    await userEvent.click(del)
    // Click the destructive Delete button inside the dialog
    const confirmBtn = await screen.findByRole('button', { name: 'Delete' })
    await userEvent.click(confirmBtn)
    await waitFor(() => expect(deleteTemplate).toHaveBeenCalledOnce())
    expect(deleteTemplate).toHaveBeenCalledWith('c1')
  })

  it('toggles auto-run in the editor and saves it through updateTemplate', async () => {
    renderScreen()
    const edit = await screen.findByRole('button', { name: 'Edit' })
    await userEvent.click(edit)
    const toggle = await screen.findByLabelText('Auto-run on new notes')
    await userEvent.click(toggle)
    const save = await screen.findByRole('button', { name: /^save$/i })
    await userEvent.click(save)
    await waitFor(() => expect(updateTemplate).toHaveBeenCalledWith('c1', 'My Standup', 'pre', [
      { heading: 'Updates', instruction: 'List updates' },
    ], false))
  })
})
