// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ToastProvider } from '@/components/ui/Toast'
import type { Template } from '../../shared/types'

const builtIn: Template = {
  id: 'b1',
  name: 'General',
  sections: [{ heading: 'Summary', instruction: 'Summarize' }],
  built_in: true,
}
const custom: Template = {
  id: 'c1',
  name: 'My Standup',
  sections: [{ heading: 'Updates', instruction: 'List updates' }],
  built_in: false,
}

const listTemplates = vi.fn().mockResolvedValue([builtIn, custom])
const deleteTemplate = vi.fn().mockResolvedValue(undefined)

vi.mock('@/api', () => ({
  muesli: {
    listTemplates: () => listTemplates(),
    deleteTemplate: (id: string) => deleteTemplate(id),
    createTemplate: vi.fn().mockResolvedValue(undefined),
    updateTemplate: vi.fn().mockResolvedValue(undefined),
  },
}))

import { TemplatesScreen } from './TemplatesScreen'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
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
    expect(screen.queryByRole('button', { name: 'Delete General' })).toBeNull()
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
})
