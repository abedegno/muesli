import { useEffect } from 'react'
import { cn } from '@/lib/cn'

// Detect mac so we can show ⌘ vs Ctrl in the overlay.
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPod|iPad/.test(navigator.platform)


const SECTIONS = [
  {
    title: 'Navigation',
    shortcuts: [
      { keys: [isMac ? '⌘K' : 'Ctrl+K'], label: 'Open command palette' },
      { keys: [isMac ? '⌘N' : 'Ctrl+N'], label: 'New meeting' },
      { keys: [isMac ? '⌘\\' : 'Ctrl+\\'], label: 'Toggle sidebar' },
    ],
  },
  {
    title: 'Help',
    shortcuts: [
      { keys: ['?'], label: 'Show keyboard shortcuts' },
    ],
  },
]

export function KeyboardShortcutsOverlay({ open, onClose }: { open: boolean; onClose: () => void }) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); onClose() }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  return (
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-[12vh]"
      onClick={onClose}
    >
      {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/click-events-have-key-events */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Keyboard shortcuts"
        className="w-[32rem] max-w-[90vw] overflow-hidden rounded-[var(--radius)] border border-border bg-popover shadow-md"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-base font-semibold">Keyboard shortcuts</h2>
        </div>
        <div className="p-4 space-y-5">
          {SECTIONS.map((section) => (
            <section key={section.title}>
              <h3 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {section.title}
              </h3>
              <ul className="space-y-1">
                {section.shortcuts.map((shortcut) => (
                  <li key={shortcut.label} className="flex items-center justify-between py-1">
                    <span className="text-sm">{shortcut.label}</span>
                    <span className="flex gap-1">
                      {shortcut.keys.map((k) => (
                        <kbd
                          key={k}
                          className={cn(
                            'inline-flex items-center rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground',
                          )}
                        >
                          {k}
                        </kbd>
                      ))}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      </div>
    </div>
  )
}
