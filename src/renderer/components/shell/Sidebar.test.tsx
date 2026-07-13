// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import type { SmartList } from '../../../shared/types'

const { exportFolderMock } = vi.hoisted(() => ({
  exportFolderMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    exportFolder: exportFolderMock,
  },
}))

afterEach(cleanup)
// Folder collapse state persists to localStorage; clear it so tests stay isolated.
beforeEach(() => {
  localStorage.clear()
  exportFolderMock.mockReset()
})

const lists: SmartList[] = [{ id: 'l1', name: 'Ready', created_at: '', rule: { op: 'and', children: [{ field: 'status', operator: 'is', value: 'ready' }] } }]

function renderSidebar(props: Partial<Parameters<typeof Sidebar>[0]> = {}) {
  return render(
    <MemoryRouter>
      <Sidebar query="" onQuery={() => {}} tags={[{ id: 't1', name: '1on1', count: 1 }]}
        lists={lists} listCount={() => 1} suggestions={[{ stem: 'standup', count: 3 }]}
        folders={[]} folderCount={() => 0} onNewFolder={() => {}} onEditFolder={() => {}} onDropNote={() => {}}
        activeView={{ type: 'all' }} onSelectView={() => {}} onNewList={() => {}} onEditList={() => {}} onSaveSuggestion={() => {}}
        {...props} />
    </MemoryRouter>,
  )
}

describe('Sidebar', () => {
  it('renders CTA, search, All-notes nav, tags, lists, suggestions (no note list)', () => {
    renderSidebar()
    expect(screen.getByRole('button', { name: /new meeting/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /all notes/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /1on1/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Ready/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /save .*standup/i })).toBeInTheDocument()
    // The note list no longer lives in the sidebar — it's the main pane now.
    expect(screen.queryByRole('link', { name: 'Standup' })).not.toBeInTheDocument()
  })
  it('renders the "Smart lists" section header', () => {
    renderSidebar()
    expect(screen.getByText('Smart lists')).toBeInTheDocument()
  })
  it('renders a global Chat nav link (CHT05)', () => {
    renderSidebar()
    expect(screen.getByRole('link', { name: /chat/i })).toHaveAttribute('href', '/chat')
  })
  it('renders a global Action items nav link', () => {
    renderSidebar()
    expect(screen.getByRole('link', { name: /action items/i })).toHaveAttribute('href', '/action-items')
  })
  it('collapsed sidebar still exposes a Chat entry point', () => {
    renderSidebar({ collapsed: true })
    expect(screen.getByRole('button', { name: /chat/i })).toBeInTheDocument()
  })
  it('renders a smart-list row first-condition subtitle', () => {
    renderSidebar()
    expect(screen.getByText('status is ready')).toBeInTheDocument()
  })
  it('dragging a note over a folder row highlights it (drop glow)', () => {
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }] })
    const row = screen.getByRole('button', { name: /Clients/ }).closest('div')!
    fireEvent.dragOver(row, { dataTransfer: { types: ['text/note-id'] } })
    expect(row.className).toContain('ring-primary')
  })
  it('selecting a list reports a list view', async () => {
    const onSelectView = vi.fn()
    renderSidebar({ onSelectView })
    await userEvent.click(screen.getByRole('button', { name: /Ready/i }))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'list', id: 'l1' })
  })
  it('selecting a folder reports a folder view', async () => {
    const onSelectView = vi.fn()
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }], folderCount: () => 2, onSelectView })
    await userEvent.click(screen.getByRole('button', { name: /Clients/ }))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'folder', id: 'f1' })
  })
  it('dropping a note on a folder calls onDropNote', () => {
    const onDropNote = vi.fn()
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }], onDropNote })
    // The drop handler lives on the row container (the name is a nested button).
    const row = screen.getByRole('button', { name: /Clients/ }).closest('div')!
    // text/folder-id is empty (note drag), text/note-id carries the id.
    fireEvent.drop(row, { dataTransfer: { getData: (t: string) => (t === 'text/note-id' ? 'note-9' : '') } })
    expect(onDropNote).toHaveBeenCalledWith('f1', 'note-9')
  })

  it('exports a folder with the selected format and export options', async () => {
    const user = userEvent.setup()
    exportFolderMock.mockResolvedValue({ success: true, path: '/tmp/clients.zip' })
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }] })

    const row = screen.getByRole('button', { name: /Clients/ }).closest('div')!
    fireEvent.mouseEnter(row)
    await user.click(screen.getByRole('button', { name: /more actions for clients/i }))
    await user.click(await screen.findByRole('menuitem', { name: /export folder/i }))
    expect(screen.getByRole('dialog')).toHaveTextContent('Export folder: Clients')

    await user.selectOptions(screen.getByLabelText('Export format'), 'pdf')
    await user.click(screen.getByLabelText(/include transcript/i))
    await user.click(screen.getByLabelText(/redact speaker names/i))
    await user.click(screen.getByRole('button', { name: /^export folder$/i }))

    await waitFor(() => expect(exportFolderMock).toHaveBeenCalledWith('f1', 'pdf', {
      includeTranscript: false,
      redactSpeakers: true,
    }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('dropping folder B onto folder A re-parents B under A', () => {
    const onReparentFolder = vi.fn()
    renderSidebar({
      folders: [
        { id: 'A', name: 'Alpha', created_at: '' },
        { id: 'B', name: 'Beta', created_at: '' },
      ],
      onReparentFolder,
    })
    const rowA = screen.getByRole('button', { name: /Alpha/ }).closest('div')!
    fireEvent.drop(rowA, { dataTransfer: { getData: (t: string) => (t === 'text/folder-id' ? 'B' : '') } })
    expect(onReparentFolder).toHaveBeenCalledWith('B', 'A')
  })

  it('dropping a folder on the Folders header moves it to top level (null parent)', () => {
    const onReparentFolder = vi.fn()
    renderSidebar({
      folders: [
        { id: 'A', name: 'Alpha', created_at: '' },
        { id: 'B', name: 'Beta', parent_id: 'A', created_at: '' },
      ],
      onReparentFolder,
    })
    const header = screen.getByText('Folders').closest('div')!
    fireEvent.drop(header, { dataTransfer: { getData: (t: string) => (t === 'text/folder-id' ? 'B' : '') } })
    expect(onReparentFolder).toHaveBeenCalledWith('B', null)
  })

  it('dropping a folder into the gap after a sibling reorders it after that sibling', () => {
    const onReorderFolder = vi.fn()
    renderSidebar({
      folders: [
        { id: 'A', name: 'Alpha', created_at: '' },
        { id: 'B', name: 'Beta', created_at: '' },
      ],
      onReorderFolder,
    })
    const gap = screen.getByLabelText('reorder gap after Alpha')
    fireEvent.drop(gap, { dataTransfer: { getData: (t: string) => (t === 'text/folder-id' ? 'B' : '') } })
    expect(onReorderFolder).toHaveBeenCalledWith('B', 'A')
  })

  it('dropping a folder into the first gap reorders it to the front (afterId null)', () => {
    const onReorderFolder = vi.fn()
    renderSidebar({
      folders: [
        { id: 'A', name: 'Alpha', created_at: '' },
        { id: 'B', name: 'Beta', created_at: '' },
      ],
      onReorderFolder,
    })
    const gap = screen.getByLabelText('reorder gap first')
    fireEvent.drop(gap, { dataTransfer: { getData: (t: string) => (t === 'text/folder-id' ? 'B' : '') } })
    expect(onReorderFolder).toHaveBeenCalledWith('B', null)
  })

  it('cross-parent folder dropped into a gap re-parents (not reorder)', () => {
    const onReorderFolder = vi.fn()
    const onReparentFolder = vi.fn()
    renderSidebar({
      folders: [
        { id: 'A', name: 'Alpha', created_at: '' },
        { id: 'B', name: 'Beta', created_at: '' },
        { id: 'C', name: 'Gamma', parent_id: 'A', created_at: '' },
      ],
      onReorderFolder,
      onReparentFolder,
    })
    // C lives under A; dropping it into a TOP-LEVEL gap re-parents it to top level (parent=null).
    const gap = screen.getByLabelText('reorder gap after Alpha')
    fireEvent.drop(gap, { dataTransfer: { getData: (t: string) => (t === 'text/folder-id' ? 'C' : '') } })
    expect(onReorderFolder).not.toHaveBeenCalled()
    expect(onReparentFolder).toHaveBeenCalledWith('C', null)
  })

  it('does NOT re-parent a folder onto its own descendant (cycle guard)', () => {
    const onReparentFolder = vi.fn()
    renderSidebar({
      folders: [
        { id: 'A', name: 'Alpha', created_at: '' },
        { id: 'B', name: 'Beta', parent_id: 'A', created_at: '' },
      ],
      onReparentFolder,
    })
    // Drag A (parent) onto B (its child) → cycle → ignored.
    const rowB = screen.getByRole('button', { name: /^Beta/ }).closest('div')!
    fireEvent.drop(rowB, { dataTransfer: { getData: (t: string) => (t === 'text/folder-id' ? 'A' : '') } })
    expect(onReparentFolder).not.toHaveBeenCalled()
  })

  it('a folder row is draggable and sets text/folder-id on dragStart', () => {
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }] })
    const row = screen.getByRole('button', { name: /Clients/ }).closest('div')!
    expect(row).toHaveAttribute('draggable', 'true')
    const setData = vi.fn()
    fireEvent.dragStart(row, { dataTransfer: { setData, effectAllowed: '' } })
    expect(setData).toHaveBeenCalledWith('text/folder-id', 'f1')
  })

  it('renders nested folders and collapses children', async () => {
    renderSidebar({
      folders: [
        { id: 'f1', name: 'Clients', created_at: '' },
        { id: 'f2', name: 'Acme', parent_id: 'f1', created_at: '' },
      ],
      folderCount: () => 0,
    })
    expect(screen.getByRole('button', { name: /^Clients/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Acme/ })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Collapse Clients/ }))
    expect(screen.queryByRole('button', { name: /^Acme/ })).not.toBeInTheDocument()
  })

  it('starts a folder collapsed when its id is persisted in localStorage', () => {
    localStorage.setItem('muesli.sidebar.folderCollapsed', JSON.stringify(['f1']))
    renderSidebar({
      folders: [
        { id: 'f1', name: 'Clients', created_at: '' },
        { id: 'f2', name: 'Acme', parent_id: 'f1', created_at: '' },
      ],
      folderCount: () => 0,
    })
    // Parent is shown but, being collapsed, its child is not rendered.
    expect(screen.getByRole('button', { name: /^Clients/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Acme/ })).not.toBeInTheDocument()
  })

  it('toggling a folder collapse persists the id to localStorage and removes it on toggle back', async () => {
    renderSidebar({
      folders: [
        { id: 'f1', name: 'Clients', created_at: '' },
        { id: 'f2', name: 'Acme', parent_id: 'f1', created_at: '' },
      ],
      folderCount: () => 0,
    })
    await userEvent.click(screen.getByRole('button', { name: /Collapse Clients/ }))
    expect(JSON.parse(localStorage.getItem('muesli.sidebar.folderCollapsed')!)).toContain('f1')
    await userEvent.click(screen.getByRole('button', { name: /Expand Clients/ }))
    expect(JSON.parse(localStorage.getItem('muesli.sidebar.folderCollapsed')!)).not.toContain('f1')
  })

  it('filters out stale folder ids that no longer exist in the folders prop', () => {
    localStorage.setItem('muesli.sidebar.folderCollapsed', JSON.stringify(['ghost', 'f1']))
    renderSidebar({
      folders: [
        { id: 'f1', name: 'Clients', created_at: '' },
        { id: 'f2', name: 'Acme', parent_id: 'f1', created_at: '' },
      ],
      folderCount: () => 0,
    })
    // After render the persisting effect rewrites the filtered set: ghost is gone, f1 stays.
    const stored = JSON.parse(localStorage.getItem('muesli.sidebar.folderCollapsed')!)
    expect(stored).not.toContain('ghost')
    expect(stored).toContain('f1')
  })
  it('selecting a tag reports a tag view', async () => {
    const onSelectView = vi.fn()
    renderSidebar({ onSelectView })
    await userEvent.click(screen.getByRole('button', { name: /1on1/i }))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'tag', tag: '1on1' })
  })
  it('shows "Save as smart list" only when searching and fires onSaveSearchAsList', async () => {
    const onSaveSearchAsList = vi.fn()
    const { rerender } = renderSidebar({ query: '', onSaveSearchAsList })
    expect(screen.queryByRole('button', { name: /save as smart list/i })).not.toBeInTheDocument()
    cleanup()
    renderSidebar({ query: 'roadmap', onSaveSearchAsList })
    void rerender
    await userEvent.click(screen.getByRole('button', { name: /save as smart list/i }))
    expect(onSaveSearchAsList).toHaveBeenCalledWith('roadmap')
  })

  it('saving a suggestion fires onSaveSuggestion with the stem', async () => {
    const onSaveSuggestion = vi.fn()
    renderSidebar({ onSaveSuggestion })
    await userEvent.click(screen.getByRole('button', { name: /save .*standup/i }))
    expect(onSaveSuggestion).toHaveBeenCalledWith('standup')
  })

  it('right-clicking a smart-list row shows Edit rule…/Move to Trash and Move to Trash calls onDeleteList', async () => {
    const onDeleteList = vi.fn()
    renderSidebar({ onDeleteList })
    fireEvent.contextMenu(screen.getByRole('button', { name: /Ready/i }))
    expect(await screen.findByText('Edit rule…')).toBeInTheDocument()
    const del = await screen.findByText('Move to Trash')
    await userEvent.click(del)
    expect(onDeleteList).toHaveBeenCalledWith('l1')
  })

  it('right-clicking a folder row shows New subfolder…/Rename…/Move to Trash and wires the handlers', async () => {
    const onNewSubfolder = vi.fn()
    const onDeleteFolder = vi.fn()
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }], onNewSubfolder, onDeleteFolder })
    const row = screen.getByRole('button', { name: /Clients/ }).closest('div')!
    fireEvent.contextMenu(row)
    expect(await screen.findByText('New subfolder…')).toBeInTheDocument()
    expect(await screen.findByText('Rename…')).toBeInTheDocument()
    expect(await screen.findByText('Move to Trash')).toBeInTheDocument()
    await userEvent.click(screen.getByText('New subfolder…'))
    expect(onNewSubfolder).toHaveBeenCalledWith('f1')
  })

  it('right-clicking a folder row → Move to Trash calls onDeleteFolder', async () => {
    const onDeleteFolder = vi.fn()
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }], onDeleteFolder })
    const row = screen.getByRole('button', { name: /Clients/ }).closest('div')!
    fireEvent.contextMenu(row)
    await userEvent.click(await screen.findByText('Move to Trash'))
    expect(onDeleteFolder).toHaveBeenCalledWith('f1')
  })

  it('right-clicking a tag row shows Save as smart list and calls onSaveTagAsList', async () => {
    const onSaveTagAsList = vi.fn()
    renderSidebar({ onSaveTagAsList })
    fireEvent.contextMenu(screen.getByRole('button', { name: /1on1/i }))
    await userEvent.click(await screen.findByText('Save as smart list'))
    expect(onSaveTagAsList).toHaveBeenCalledWith('1on1')
  })

  it('right-clicking a tag row shows Rename and calls onRenameTag with {id,name}', async () => {
    const onRenameTag = vi.fn()
    renderSidebar({ onRenameTag })
    fireEvent.contextMenu(screen.getByRole('button', { name: /1on1/i }))
    await userEvent.click(await screen.findByText('Rename…'))
    expect(onRenameTag).toHaveBeenCalledWith({ id: 't1', name: '1on1' })
  })

  it('expanded: shows a collapse toggle and a resize handle', () => {
    const onToggleCollapsed = vi.fn()
    renderSidebar({ onToggleCollapsed })
    expect(screen.getByRole('button', { name: /collapse sidebar/i })).toBeInTheDocument()
    expect(screen.getByRole('separator', { name: /resize sidebar/i })).toBeInTheDocument()
    // full content present
    expect(screen.getByRole('link', { name: /all notes/i })).toBeInTheDocument()
  })

  it('clicking the collapse toggle calls onToggleCollapsed', async () => {
    const onToggleCollapsed = vi.fn()
    renderSidebar({ onToggleCollapsed })
    await userEvent.click(screen.getByRole('button', { name: /collapse sidebar/i }))
    expect(onToggleCollapsed).toHaveBeenCalled()
  })

  it('collapsed: renders only the rail (expand + new meeting), no nav/search/handle', () => {
    renderSidebar({ collapsed: true })
    expect(screen.getByRole('button', { name: /expand sidebar/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /new meeting/i })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /all notes/i })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/search notes/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('separator', { name: /resize sidebar/i })).not.toBeInTheDocument()
  })

  it('dragging the resize handle reports new widths and stops on mouseup', () => {
    const onResize = vi.fn()
    renderSidebar({ onResize })
    const handle = screen.getByRole('separator', { name: /resize sidebar/i })
    fireEvent.mouseDown(handle, { clientX: 256 })
    fireEvent(window, new MouseEvent('mousemove', { clientX: 300 }))
    expect(onResize).toHaveBeenCalled()
    fireEvent(window, new MouseEvent('mouseup'))
    onResize.mockClear()
    fireEvent(window, new MouseEvent('mousemove', { clientX: 400 }))
    expect(onResize).not.toHaveBeenCalled()
  })

  it('unmounting mid-drag removes the window listeners (no dangling onResize)', () => {
    const onResize = vi.fn()
    const { unmount } = renderSidebar({ onResize })
    fireEvent.mouseDown(screen.getByRole('separator', { name: /resize sidebar/i }), { clientX: 256 })
    unmount()
    onResize.mockClear()
    fireEvent(window, new MouseEvent('mousemove', { clientX: 380 }))
    expect(onResize).not.toHaveBeenCalled()
  })

  describe('chevron button keyboard accessibility', () => {
    function renderWithNestedFolders(props: Partial<Parameters<typeof Sidebar>[0]> = {}) {
      return renderSidebar({
        folders: [
          { id: 'f1', name: 'Clients', created_at: '' },
          { id: 'f2', name: 'Acme', parent_id: 'f1', created_at: '' },
        ],
        folderCount: () => 0,
        ...props,
      })
    }

    it('Enter key on chevron collapses an expanded folder', () => {
      renderWithNestedFolders()
      // Initially expanded — child is visible
      expect(screen.getByRole('button', { name: /^Acme/ })).toBeInTheDocument()
      const chevron = screen.getByRole('button', { name: /Collapse Clients/ })
      fireEvent.keyDown(chevron, { key: 'Enter' })
      // Now collapsed — child hidden
      expect(screen.queryByRole('button', { name: /^Acme/ })).not.toBeInTheDocument()
    })

    it('Space key on chevron expands a collapsed folder', () => {
      renderWithNestedFolders()
      const chevron = screen.getByRole('button', { name: /Collapse Clients/ })
      // Collapse first with Enter
      fireEvent.keyDown(chevron, { key: 'Enter' })
      expect(screen.queryByRole('button', { name: /^Acme/ })).not.toBeInTheDocument()
      // Now Space to expand
      const chevronExpand = screen.getByRole('button', { name: /Expand Clients/ })
      fireEvent.keyDown(chevronExpand, { key: ' ' })
      expect(screen.getByRole('button', { name: /^Acme/ })).toBeInTheDocument()
    })

    it('click on chevron still toggles collapse (onClick remains)', async () => {
      renderWithNestedFolders()
      const chevron = screen.getByRole('button', { name: /Collapse Clients/ })
      fireEvent.click(chevron)
      expect(screen.queryByRole('button', { name: /^Acme/ })).not.toBeInTheDocument()
    })

    it('other keys on chevron do nothing', () => {
      renderWithNestedFolders()
      expect(screen.getByRole('button', { name: /^Acme/ })).toBeInTheDocument()
      const chevron = screen.getByRole('button', { name: /Collapse Clients/ })
      fireEvent.keyDown(chevron, { key: 'Tab' })
      fireEvent.keyDown(chevron, { key: 'ArrowDown' })
      // Still expanded
      expect(screen.getByRole('button', { name: /^Acme/ })).toBeInTheDocument()
    })

    it('Space keyDown calls preventDefault to avoid page scroll', async () => {
      const { act } = await import('react')
      renderWithNestedFolders()
      const chevron = screen.getByRole('button', { name: /Collapse Clients/ })
      const event = new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true })
      const preventDefaultSpy = vi.spyOn(event, 'preventDefault')
      await act(() => { chevron.dispatchEvent(event) })
      expect(preventDefaultSpy).toHaveBeenCalled()
    })

    it('Enter keyDown calls preventDefault to prevent synthetic click double-toggle', async () => {
      const { act } = await import('react')
      renderWithNestedFolders()
      const chevron = screen.getByRole('button', { name: /Collapse Clients/ })
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
      const preventDefaultSpy = vi.spyOn(event, 'preventDefault')
      await act(() => { chevron.dispatchEvent(event) })
      expect(preventDefaultSpy).toHaveBeenCalled()
    })

    it('chevron keyDown stops propagation (event does not bubble to ancestor)', () => {
      const ancestorKeyDown = vi.fn()
      render(
        <MemoryRouter>
          {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions -- test helper div, not real UI */}
          <div onKeyDown={ancestorKeyDown}>
            <Sidebar query="" onQuery={() => {}} tags={[{ id: 't1', name: '1on1', count: 1 }]}
              lists={[]} listCount={() => 0} suggestions={[]}
              folders={[
                { id: 'f1', name: 'Clients', created_at: '' },
                { id: 'f2', name: 'Acme', parent_id: 'f1', created_at: '' },
              ]}
              folderCount={() => 0} onNewFolder={() => {}} onEditFolder={() => {}} onDropNote={() => {}}
              activeView={{ type: 'all' }} onSelectView={() => {}} onNewList={() => {}} onEditList={() => {}} onSaveSuggestion={() => {}} />
          </div>
        </MemoryRouter>,
      )
      const chevron = screen.getByRole('button', { name: /Collapse Clients/ })
      fireEvent.keyDown(chevron, { key: 'Enter' })
      expect(ancestorKeyDown).not.toHaveBeenCalled()
    })
  })

  // ── G08: hover edit affordance ─────────────────────────────────────────────

  it('hover a smart-list row → ⋯ button becomes accessible', () => {
    renderSidebar()
    // No ⋯ button before hover
    expect(screen.queryByRole('button', { name: /more actions/i })).not.toBeInTheDocument()
    const li = screen.getByRole('button', { name: /Ready/i }).closest('li')!
    fireEvent.mouseEnter(li)
    expect(screen.getByRole('button', { name: /more actions/i })).toBeInTheDocument()
  })

  it('⋯ on a smart-list row → click "Edit rule…" → onEditList called with the list', async () => {
    const onEditList = vi.fn()
    renderSidebar({ onEditList })
    const li = screen.getByRole('button', { name: /Ready/i }).closest('li')!
    fireEvent.mouseEnter(li)
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }))
    await userEvent.click(await screen.findByRole('menuitem', { name: /edit rule/i }))
    expect(onEditList).toHaveBeenCalledWith(lists[0])
  })

  it('⋯ on a smart-list row → click "Move to Trash" → onDeleteList called with id', async () => {
    const onDeleteList = vi.fn()
    renderSidebar({ onDeleteList })
    const li = screen.getByRole('button', { name: /Ready/i }).closest('li')!
    fireEvent.mouseEnter(li)
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }))
    await userEvent.click(await screen.findByRole('menuitem', { name: /move to trash/i }))
    expect(onDeleteList).toHaveBeenCalledWith('l1')
  })

  it('hover a folder row → ⋯ button accessible; click "Rename…" → onEditFolder called', async () => {
    const onEditFolder = vi.fn()
    const folder = { id: 'f1', name: 'Clients', created_at: '' }
    renderSidebar({ folders: [folder], onEditFolder })
    const li = screen.getByRole('button', { name: /Clients/ }).closest('li')!
    fireEvent.mouseEnter(li)
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }))
    await userEvent.click(await screen.findByRole('menuitem', { name: /rename/i }))
    expect(onEditFolder).toHaveBeenCalledWith(folder)
  })

  it('hover a folder row → click "Move to Trash" → onDeleteFolder called with id', async () => {
    const onDeleteFolder = vi.fn()
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }], onDeleteFolder })
    const li = screen.getByRole('button', { name: /Clients/ }).closest('li')!
    fireEvent.mouseEnter(li)
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }))
    await userEvent.click(await screen.findByRole('menuitem', { name: /move to trash/i }))
    expect(onDeleteFolder).toHaveBeenCalledWith('f1')
  })

  it('clicking the row body (not ⋯) on a smart-list still calls onSelectView', async () => {
    const onSelectView = vi.fn()
    renderSidebar({ onSelectView })
    await userEvent.click(screen.getByRole('button', { name: /Ready/i }))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'list', id: 'l1' })
  })
  it('hover a folder row → ⋯ button appears; absent before hover', () => {
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }] })
    // Before hover: no ⋯ button
    expect(screen.queryByRole('button', { name: /more actions/i })).not.toBeInTheDocument()
    const li = screen.getByRole('button', { name: /Clients/ }).closest('li')!
    fireEvent.mouseEnter(li)
    // After hover: ⋯ button is present
    expect(screen.getByRole('button', { name: /more actions/i })).toBeInTheDocument()
  })

  it('⋯ click on smart-list row does not call onSelectView', async () => {
    const onSelectView = vi.fn()
    renderSidebar({ onSelectView })
    const li = screen.getByRole('button', { name: /Ready/i }).closest('li')!
    fireEvent.mouseEnter(li)
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }))
    expect(onSelectView).not.toHaveBeenCalled()
  })

  it('⋯ click on folder row does not call onSelectView', async () => {
    const onSelectView = vi.fn()
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }], onSelectView })
    const li = screen.getByRole('button', { name: /Clients/ }).closest('li')!
    fireEvent.mouseEnter(li)
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }))
    expect(onSelectView).not.toHaveBeenCalled()
  })

  it('clicking the folder row body calls onSelectView', async () => {
    const onSelectView = vi.fn()
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }], onSelectView })
    await userEvent.click(screen.getByRole('button', { name: /Clients/ }))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'folder', id: 'f1' })
  })

  // ── USE06 Fix 1: overflow button keyboard accessibility ──────────────────────

  it('USE06: focus inside a folder row reveals the More-actions button with item-specific aria-label', () => {
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }] })
    // No ⋯ button before focus
    expect(screen.queryByRole('button', { name: /more actions for clients/i })).not.toBeInTheDocument()
    // Fire a focus event on the folder name button (triggers React onFocus → setFocusedFolderId)
    fireEvent.focus(screen.getByRole('button', { name: /Clients/ }))
    // After focus, the ⋯ button should appear
    expect(screen.getByRole('button', { name: /more actions for clients/i })).toBeInTheDocument()
  })

  it('USE06: focus inside a smart-list row reveals the More-actions button with item-specific aria-label', () => {
    renderSidebar()
    // No ⋯ button before focus
    expect(screen.queryByRole('button', { name: /more actions for ready/i })).not.toBeInTheDocument()
    // Fire a focus event on the list name button (triggers React onFocus → setFocusedListId)
    fireEvent.focus(screen.getByRole('button', { name: /Ready/i }))
    // After focus, the ⋯ button should appear
    expect(screen.getByRole('button', { name: /more actions for ready/i })).toBeInTheDocument()
  })

  it('USE06: keyboard Tab navigates into the folder row and can reach the More-actions button', () => {
    renderSidebar({ folders: [{ id: 'f1', name: 'Clients', created_at: '' }] })
    // Fire focus on the folder name button so ⋯ button appears
    fireEvent.focus(screen.getByRole('button', { name: /Clients/ }))
    // ⋯ button is now in the DOM (shown on focus-within)
    const moreBtn = screen.getByRole('button', { name: /more actions for clients/i })
    expect(moreBtn).toBeInTheDocument()
    // The ⋯ button must be keyboard-reachable (tabIndex >= 0, not excluded from tab order)
    expect(moreBtn).not.toHaveAttribute('tabindex', '-1')
    // Correct aria-label identifies the specific folder
    expect(moreBtn).toHaveAttribute('aria-label', 'More actions for Clients')
  })

  // ── USE06 Fix 2: resize separator keyboard support ───────────────────────────

  it('USE06: resize separator has tabIndex=0 and is focusable', () => {
    renderSidebar()
    const handle = screen.getByRole('separator', { name: /resize sidebar/i })
    expect(handle).toHaveAttribute('tabindex', '0')
  })

  it('USE06: ArrowRight on resize separator increases sidebar width', () => {
    const onResize = vi.fn()
    renderSidebar({ onResize, width: 256 })
    const handle = screen.getByRole('separator', { name: /resize sidebar/i })
    handle.focus()
    fireEvent.keyDown(handle, { key: 'ArrowRight' })
    expect(onResize).toHaveBeenCalledWith(272) // 256 + 16
  })

  it('USE06: ArrowLeft on resize separator decreases sidebar width', () => {
    const onResize = vi.fn()
    renderSidebar({ onResize, width: 256 })
    const handle = screen.getByRole('separator', { name: /resize sidebar/i })
    handle.focus()
    fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    expect(onResize).toHaveBeenCalledWith(240) // 256 - 16
  })

  it('USE06: ArrowLeft on resize separator clamps to SIDEBAR_MIN_WIDTH (200)', () => {
    const onResize = vi.fn()
    renderSidebar({ onResize, width: 204 }) // 204 - 16 = 188 < 200
    const handle = screen.getByRole('separator', { name: /resize sidebar/i })
    handle.focus()
    fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    expect(onResize).toHaveBeenCalledWith(200) // clamped to min
  })

  it('USE06: ArrowRight on resize separator clamps to SIDEBAR_MAX_WIDTH (420)', () => {
    const onResize = vi.fn()
    renderSidebar({ onResize, width: 412 }) // 412 + 16 = 428 > 420
    const handle = screen.getByRole('separator', { name: /resize sidebar/i })
    handle.focus()
    fireEvent.keyDown(handle, { key: 'ArrowRight' })
    expect(onResize).toHaveBeenCalledWith(420) // clamped to max
  })
})
