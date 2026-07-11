// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { KeyboardShortcutsOverlay } from './KeyboardShortcutsOverlay'

afterEach(cleanup)

describe('KeyboardShortcutsOverlay', () => {
  it('renders nothing when closed', () => {
    render(<KeyboardShortcutsOverlay open={false} onClose={() => {}} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows all documented shortcuts when open', () => {
    render(<KeyboardShortcutsOverlay open={true} onClose={() => {}} />)
    // Command palette shortcut (⌘K or Ctrl+K)
    const hasCmdK = screen.queryByText('⌘K') !== null || screen.queryByText('Ctrl+K') !== null
    expect(hasCmdK).toBe(true)
    // New meeting shortcut (⌘N or Ctrl+N)
    const hasCmdN = screen.queryByText('⌘N') !== null || screen.queryByText('Ctrl+N') !== null
    expect(hasCmdN).toBe(true)
    // Toggle sidebar shortcut (⌘\ or Ctrl+\)
    const hasCmdBackslash = screen.queryByText('⌘\\') !== null || screen.queryByText('Ctrl+\\') !== null
    expect(hasCmdBackslash).toBe(true)
    // Keyboard shortcuts toggle
    expect(screen.getByText('?')).toBeInTheDocument()
  })

  it('shows a dialog with the correct role and label when open', () => {
    render(<KeyboardShortcutsOverlay open={true} onClose={() => {}} />)
    const dialog = screen.getByRole('dialog', { name: /keyboard shortcuts/i })
    expect(dialog).toBeInTheDocument()
    expect(dialog).toHaveAttribute('aria-modal', 'true')
  })

  it('calls onClose when Escape is pressed', async () => {
    const onClose = vi.fn()
    render(<KeyboardShortcutsOverlay open={true} onClose={onClose} />)
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalled()
  })

  it('calls onClose when the backdrop is clicked', async () => {
    const onClose = vi.fn()
    const { container } = render(<KeyboardShortcutsOverlay open={true} onClose={onClose} />)
    // The backdrop is the outermost fixed div; click it directly
    const backdrop = container.firstChild as HTMLElement
    await userEvent.click(backdrop)
    expect(onClose).toHaveBeenCalled()
  })

  it('does NOT call onClose when clicking inside the panel', async () => {
    const onClose = vi.fn()
    render(<KeyboardShortcutsOverlay open={true} onClose={onClose} />)
    const dialog = screen.getByRole('dialog', { name: /keyboard shortcuts/i })
    await userEvent.click(dialog)
    expect(onClose).not.toHaveBeenCalled()
  })
})
