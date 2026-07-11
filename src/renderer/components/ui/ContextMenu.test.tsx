// @vitest-environment jsdom
import { afterEach, describe, it, expect, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/react'
import {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
} from './ContextMenu'

afterEach(cleanup)

function renderMenu(onEdit = vi.fn(), onDelete = vi.fn()) {
  render(
    <ContextMenu>
      <ContextMenuTrigger>Right-click me</ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onSelect={onEdit}>Edit</ContextMenuItem>
        <ContextMenuItem destructive onSelect={onDelete}>Delete</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>,
  )
  return { onEdit, onDelete }
}

describe('ContextMenu', () => {
  it('renders its trigger', () => {
    renderMenu()
    expect(screen.getByText('Right-click me')).toBeInTheDocument()
  })

  it('opens on contextmenu, shows items, and fires onSelect when an item is chosen', async () => {
    const { onDelete } = renderMenu()
    fireEvent.contextMenu(screen.getByText('Right-click me'))
    const del = await screen.findByText('Delete')
    expect(await screen.findByText('Edit')).toBeInTheDocument()
    fireEvent.click(del)
    expect(onDelete).toHaveBeenCalled()
  })

  it('applies the destructive style to destructive items', async () => {
    renderMenu()
    fireEvent.contextMenu(screen.getByText('Right-click me'))
    const del = await screen.findByText('Delete')
    expect(del.className).toContain('text-destructive')
  })

  it('renders a submenu trigger inside the content', async () => {
    render(
      <ContextMenu>
        <ContextMenuTrigger>Open</ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuSub>
            <ContextMenuSubTrigger>More</ContextMenuSubTrigger>
            <ContextMenuSubContent>
              <ContextMenuItem>Nested</ContextMenuItem>
            </ContextMenuSubContent>
          </ContextMenuSub>
        </ContextMenuContent>
      </ContextMenu>,
    )
    fireEvent.contextMenu(screen.getByText('Open'))
    expect(await screen.findByText('More')).toBeInTheDocument()
  })
})
