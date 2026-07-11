import { useEffect, useMemo, useRef, useState } from 'react'
import type React from 'react'
import { Search } from 'lucide-react'
import { cn } from '@/lib/cn'
import type { Note, Folder, SmartList } from '../../shared/types'
import type { ActiveView } from './shell/AppLayout'

type Item =
  | { kind: 'all'; label: string }
  | { kind: 'action'; label: string; run: () => void }
  | { kind: 'note'; label: string; id: string }
  | { kind: 'folder'; label: string; id: string }
  | { kind: 'list'; label: string; id: string }
  | { kind: 'tag'; label: string; tag: string }

export function CommandPalette({
  open, onClose, notes, folders, lists, tags, onSelectNote, onSelectView, actions = [],
}: {
  open: boolean
  onClose: () => void
  notes: Note[]
  folders: Folder[]
  lists: SmartList[]
  tags: { name: string }[]
  onSelectNote: (id: string) => void
  onSelectView: (v: ActiveView) => void
  actions?: { label: string; run: () => void }[]
}) {
  const [q, setQ] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const previousFocusRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!open) return
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setQ('')
    setActive(0)
    const id = window.setTimeout(() => inputRef.current?.focus(), 0)
    return () => {
      window.clearTimeout(id)
      const previousFocus = previousFocusRef.current
      previousFocusRef.current = null
      if (previousFocus && previousFocus.isConnected) previousFocus.focus()
    }
  }, [open])

  const items = useMemo<Item[]>(() => {
    const needle = q.trim().toLowerCase()
    const match = (s: string) => s.toLowerCase().includes(needle)
    const out: Item[] = []
    // "All notes" command (always when empty or matches)
    if (!needle || 'all notes'.includes(needle)) out.push({ kind: 'all', label: 'All notes' })
    // Action commands (always when empty or matches their label)
    actions.filter((a) => !needle || a.label.toLowerCase().includes(needle)).forEach((a) => out.push({ kind: 'action', label: a.label, run: a.run }))
    if (!needle) {
      // empty query → recent notes only
      notes.slice(0, 7).forEach((n) => out.push({ kind: 'note', label: n.title || 'Untitled', id: n.id }))
      return out
    }
    notes.filter((n) => match(n.title || 'Untitled')).slice(0, 6).forEach((n) => out.push({ kind: 'note', label: n.title || 'Untitled', id: n.id }))
    folders.filter((f) => match(f.name)).slice(0, 6).forEach((f) => out.push({ kind: 'folder', label: f.name, id: f.id }))
    lists.filter((l) => match(l.name)).slice(0, 6).forEach((l) => out.push({ kind: 'list', label: l.name, id: l.id }))
    tags.filter((t) => match(t.name)).slice(0, 6).forEach((t) => out.push({ kind: 'tag', label: '#' + t.name, tag: t.name }))
    return out
  }, [q, notes, folders, lists, tags, actions])

  useEffect(() => { if (active >= items.length) setActive(0) }, [items.length, active])

  if (!open) return null

  const choose = (it: Item) => {
    if (it.kind === 'action') { it.run(); onClose(); return }
    if (it.kind === 'note') onSelectNote(it.id)
    else if (it.kind === 'all') onSelectView({ type: 'all' })
    else if (it.kind === 'folder') onSelectView({ type: 'folder', id: it.id })
    else if (it.kind === 'list') onSelectView({ type: 'list', id: it.id })
    else if (it.kind === 'tag') onSelectView({ type: 'tag', tag: it.tag })
    onClose()
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); onClose() }
    else if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => (items.length ? (a + 1) % items.length : 0)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => (items.length ? (a - 1 + items.length) % items.length : 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); if (items[active]) choose(items[active]) }
  }

  // Basic focus-trap: keep Tab within the panel's focusable elements.
  const onPanelKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== 'Tab') return
    const panel = panelRef.current
    if (!panel) return
    const focusable = panel.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
    )
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const activeEl = document.activeElement as HTMLElement | null
    if (e.shiftKey) {
      if (activeEl === first || !panel.contains(activeEl)) { e.preventDefault(); last.focus() }
    } else if (activeEl === last) {
      e.preventDefault(); first.focus()
    }
  }

  const groupLabel = (k: Item['kind']) => ({ all: '', action: 'Commands', note: 'Notes', folder: 'Folders', list: 'Smart lists', tag: 'Tags' }[k])

  return (
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-[12vh]" onClick={onClose}>
      {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="w-[34rem] max-w-[92vw] overflow-hidden rounded-[var(--radius)] border border-border bg-popover shadow-md"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={onPanelKeyDown}
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Search size={16} className="text-muted-foreground" />
          <input ref={inputRef} aria-label="Search" value={q}
            onChange={(e) => { setQ(e.target.value); setActive(0) }} onKeyDown={onKeyDown}
            placeholder="Jump to a note, folder, list, or tag…"
            className="h-11 w-full bg-transparent text-sm focus:outline-none" />
        </div>
        <ul className="max-h-[50vh] overflow-y-auto p-1">
          {items.length === 0 && <li className="px-3 py-2 text-sm text-muted-foreground">No matches</li>}
          {items.map((it, i) => {
            const g = groupLabel(it.kind)
            const prevG = i > 0 ? groupLabel(items[i - 1].kind) : ''
            return (
              <li key={i}>
                {g && g !== prevG && <div className="px-2 pb-1 pt-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">{g}</div>}
                <button
                  onMouseEnter={() => setActive(i)}
                  onClick={() => choose(it)}
                  className={cn('flex items-center gap-2 block w-full rounded px-2 py-1.5 text-left text-sm', i === active ? 'bg-primary/10 text-primary' : 'hover:bg-muted')}
                >
                  <span className="min-w-0 truncate">{it.label}</span>
                  {it.kind === 'folder' && <span className="shrink-0 text-xs text-muted-foreground">→ folder</span>}
                </button>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}
