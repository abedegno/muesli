import { useEffect, useRef, useState } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { Plus, Search, FileText, MessageSquare, Settings, Tag as TagIcon, Filter, Sparkles, Folder as FolderIcon, ChevronRight, ChevronDown, Trash2, PanelLeft, PanelLeftClose, MoreHorizontal, Calendar, Users, CheckSquare2 } from 'lucide-react'
import { cn } from '@/lib/cn'
import { descendantIds } from '@/lib/folders'
import { readStoredFolderCollapsed, writeStoredFolderCollapsed, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from '@/lib/sidebarPrefs'
import { describeRule } from '@/lib/smartList'
import { Button } from '@/components/ui/Button'
import { ContextMenu, ContextMenuTrigger, ContextMenuContent, ContextMenuItem, ContextMenuSeparator } from '@/components/ui/ContextMenu'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/DropdownMenu'
import type { SmartList, Folder } from '../../../shared/types'
import type { TagCount } from '@/lib/tagIndex'
import type { RecurringSuggestion } from '@/lib/recurring'
import type { ActiveView } from './AppLayout'

export function Sidebar({
  query, onQuery, dateFrom = '', dateTo = '', onDateFrom = () => {}, onDateTo = () => {}, tags, lists, listCount, suggestions,
  folders, folderCount, onNewFolder, onEditFolder, onDropNote, onReparentFolder = () => {}, onReorderFolder = () => {},
  activeView, onSelectView, onNewList, onEditList, onSaveSuggestion,
  onDeleteList = () => {}, onDeleteFolder = () => {}, onNewSubfolder = () => {}, onSaveTagAsList = () => {},
  onRenameTag = () => {},
  onSaveSearchAsList = () => {},
  collapsed = false, width = 256, onToggleCollapsed = () => {}, onResize = () => {},
}: {
  query: string
  onQuery: (q: string) => void
  dateFrom?: string
  dateTo?: string
  onDateFrom?: (v: string) => void
  onDateTo?: (v: string) => void
  tags: TagCount[]
  lists: SmartList[]
  listCount: (l: SmartList) => number
  suggestions: RecurringSuggestion[]
  folders: Folder[]
  folderCount: (f: Folder) => number
  onNewFolder: () => void
  onEditFolder: (f: Folder) => void
  onDropNote: (folderId: string, noteId: string) => void
  onReparentFolder?: (folderId: string, parentId: string | null) => void
  onReorderFolder?: (folderId: string, afterId: string | null) => void
  activeView: ActiveView
  onSelectView: (v: ActiveView) => void
  onNewList: () => void
  onEditList: (l: SmartList) => void
  onSaveSuggestion: (stem: string) => void
  onDeleteList?: (id: string) => void
  onDeleteFolder?: (id: string) => void
  onNewSubfolder?: (parentId: string) => void
  onSaveTagAsList?: (name: string) => void
  onRenameTag?: (tag: { id: string; name: string }) => void
  onSaveSearchAsList?: (query: string) => void
  collapsed?: boolean
  width?: number
  onToggleCollapsed?: () => void
  onResize?: (width: number) => void
}) {
  const navigate = useNavigate()
  const asideRef = useRef<HTMLElement>(null)
  // Holds the teardown for an in-flight resize drag so listeners are removed even
  // if the sidebar unmounts mid-drag (e.g. ⌘\ collapses it while dragging).
  const dragCleanupRef = useRef<() => void>(() => {})
  useEffect(() => () => dragCleanupRef.current(), [])
  // Seeded from localStorage (filtered to existing folders so stale ids drop off)
  // and persisted on change, so collapse state survives remount/navigation/reload.
  const [folderCollapsed, setFolderCollapsed] = useState<Set<string>>(() =>
    readStoredFolderCollapsed(folders.map((f) => f.id)),
  )
  useEffect(() => { writeStoredFolderCollapsed(folderCollapsed) }, [folderCollapsed])
  // Drop-target highlight while a folder is dragged (re-parent). null = none, '' = the
  // top-level "Folders" header target.
  const [dragOverFolder, setDragOverFolder] = useState<string | null>(null)
  // Highlight key for the reorder drop-gap currently under the pointer, e.g.
  // "root:first" or "A:B" (parent ?? 'root' : afterId ?? 'first'). null = none.
  const [dragOverGap, setDragOverGap] = useState<string | null>(null)

  // Hover state for the ⋯ more-actions button on smart-list and folder rows.
  const [hoveredListId, setHoveredListId] = useState<string | null>(null)
  const [openListDropdownId, setOpenListDropdownId] = useState<string | null>(null)
  const [hoveredFolderId, setHoveredFolderId] = useState<string | null>(null)
  const [openFolderDropdownId, setOpenFolderDropdownId] = useState<string | null>(null)
  // Keyboard-focus state: show ⋯ button when any element inside the row has focus.
  const [focusedListId, setFocusedListId] = useState<string | null>(null)
  const [focusedFolderId, setFocusedFolderId] = useState<string | null>(null)

  // Show the ⋯ button for a list/folder row when it is hovered, focused, OR its dropdown is open.
  const showListMore = (id: string) => hoveredListId === id || openListDropdownId === id || focusedListId === id
  const showFolderMore = (id: string) => hoveredFolderId === id || openFolderDropdownId === id || focusedFolderId === id

  const isFolderDrag = (e: React.DragEvent) => e.dataTransfer.types.includes('text/folder-id')
  const childrenOf = (pid: string | null) => folders.filter((f) => (f.parent_id ?? null) === pid)
  // Selecting a view always returns to the main list pane (so it shows even when a
  // note detail is open), then applies the filter.
  const selectView = (v: ActiveView) => { navigate('/'); onSelectView(v) }
  const rowBase = 'flex w-full items-center justify-between rounded-[var(--radius)] px-2 py-1.5 text-sm'
  const active = 'bg-primary/10 font-medium text-primary'
  const idle = 'text-foreground hover:bg-muted'
  const section = 'px-1 pb-1 pt-3 text-xs font-medium uppercase tracking-wide text-muted-foreground'

  const renderFolders = (parent: string | null, depth: number) => {
    // A thin (~6px) drop target between sibling rows. Dropping a same-parent
    // folder here reorders it to sit immediately after `afterId` (null = first).
    // Cross-parent drops are ignored so reparenting stays the drag-onto-row path.
    const gap = (afterId: string | null, label: string) => {
      const key = `${parent ?? 'root'}:${afterId ?? 'first'}`
      return (
        <li
          key={`gap:${key}`}
          aria-label={label}
          className={cn('h-1.5 w-full', dragOverGap === key && 'bg-primary')}
          style={{ paddingLeft: depth * 12 + 8 }}
          onDragOver={(e) => { if (isFolderDrag(e)) { e.preventDefault(); setDragOverGap(key) } }}
          onDragLeave={() => setDragOverGap((cur) => (cur === key ? null : cur))}
          onDrop={(e) => {
            setDragOverGap((cur) => (cur === key ? null : cur))
            const draggedId = e.dataTransfer.getData('text/folder-id')
            if (!draggedId) return
            const dragged = folders.find((x) => x.id === draggedId)
            if (!dragged) return
            if ((dragged.parent_id ?? null) === parent) {
              // Same-parent: reorder to sit immediately after `afterId`.
              if (draggedId !== afterId) onReorderFolder(draggedId, afterId)
            } else if (draggedId !== parent && !(parent != null && descendantIds(folders, draggedId).has(parent))) {
              // Cross-parent: re-parent into this group (lands at the end), cycle-guarded.
              onReparentFolder(draggedId, parent)
            }
          }} />
      )
    }
    const rows = childrenOf(parent).map((f) => {
      const kids = childrenOf(f.id)
      const isCollapsed = folderCollapsed.has(f.id)
      return (
        <li key={f.id}
          onMouseEnter={() => setHoveredFolderId(f.id)}
          onMouseLeave={() => setHoveredFolderId((cur) => (cur === f.id ? null : cur))}
          onFocus={() => setFocusedFolderId(f.id)}
          onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setFocusedFolderId(null) }}>
          <ContextMenu>
          <ContextMenuTrigger asChild>
          <div
            draggable
            onDragStart={(e) => { e.dataTransfer.setData('text/folder-id', f.id); e.dataTransfer.effectAllowed = 'move' }}
            onDragEnd={() => { setDragOverFolder(null); setDragOverGap(null) }}
            className={cn(rowBase,
              activeView.type === 'folder' && activeView.id === f.id ? active : idle,
              dragOverFolder === f.id && 'ring-1 ring-primary')}
            style={{ paddingLeft: depth * 12 + 8 }}
            onDragOver={(e) => { e.preventDefault(); setDragOverFolder(f.id) }}
            onDragLeave={() => setDragOverFolder((cur) => (cur === f.id ? null : cur))}
            onDrop={(e) => {
              setDragOverFolder(null)
              const folderId = e.dataTransfer.getData('text/folder-id')
              if (folderId) {
                if (folderId !== f.id && !descendantIds(folders, folderId).has(f.id)) onReparentFolder(folderId, f.id)
                return
              }
              const noteId = e.dataTransfer.getData('text/note-id')
              if (noteId) onDropNote(f.id, noteId)
            }}>
            {kids.length > 0 ? (
              <button
                aria-label={isCollapsed ? `Expand ${f.name}` : `Collapse ${f.name}`}
                onClick={(e) => { e.stopPropagation(); setFolderCollapsed((c) => { const n = new Set(c); if (n.has(f.id)) n.delete(f.id); else n.add(f.id); return n }) }}
                onKeyDown={(e) => {
                  if (e.key !== 'Enter' && e.key !== ' ') return
                  e.preventDefault()
                  e.stopPropagation()
                  setFolderCollapsed((c) => { const n = new Set(c); if (n.has(f.id)) n.delete(f.id); else n.add(f.id); return n })
                }}
                className="mr-1 shrink-0 text-muted-foreground hover:text-foreground">
                {isCollapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
              </button>
            ) : <span className="mr-1 inline-block w-3 shrink-0" />}
            <button
              onClick={() => selectView({ type: 'folder', id: f.id })}
              onDoubleClick={() => onEditFolder(f)}
              className="flex min-w-0 flex-1 items-center justify-between text-left">
              <span className="truncate">{f.name}</span>
              <span className="ml-2 shrink-0 text-xs text-muted-foreground">{folderCount(f)}</span>
            </button>
            {showFolderMore(f.id) && (
              <DropdownMenu onOpenChange={(open) => setOpenFolderDropdownId(open ? f.id : null)}>
                <DropdownMenuTrigger asChild>
                  <button
                    aria-label={`More actions for ${f.name}`}
                    onClick={(e) => e.stopPropagation()}
                    className="ml-1 shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground">
                    <MoreHorizontal size={14} />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onSelect={() => onNewSubfolder(f.id)}>New subfolder…</DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => onEditFolder(f)}>Rename…</DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem destructive onSelect={() => onDeleteFolder(f.id)}>Move to Trash</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
          </ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem onSelect={() => onNewSubfolder(f.id)}>New subfolder…</ContextMenuItem>
            <ContextMenuItem onSelect={() => onEditFolder(f)}>Rename…</ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem destructive onSelect={() => onDeleteFolder(f.id)}>Move to Trash</ContextMenuItem>
          </ContextMenuContent>
          </ContextMenu>
          {!isCollapsed && kids.length > 0 && <ul className="flex flex-col">{renderFolders(f.id, depth + 1)}</ul>}
        </li>
      )
    })
    const kids = childrenOf(parent)
    // Interleave drop-gaps: gap(first), row0, gap(after row0), row1, …
    return [
      gap(null, 'reorder gap first'),
      ...rows.flatMap((row, i) => [row, gap(kids[i].id, `reorder gap after ${kids[i].name}`)]),
    ]
  }

  // startResize tracks the pointer and reports a new width (pointer X relative to
  // the sidebar's left edge); window listeners are removed on mouseup.
  const startResize = (e: React.MouseEvent) => {
    e.preventDefault()
    const left = asideRef.current?.getBoundingClientRect().left ?? 0
    const onMove = (ev: MouseEvent) => onResize(ev.clientX - left)
    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      dragCleanupRef.current = () => {}
    }
    dragCleanupRef.current = onUp
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  if (collapsed) {
    return (
      <aside className="flex h-full w-12 shrink-0 flex-col items-center gap-2 border-r border-border bg-app-chrome py-3">
        <button aria-label="Expand sidebar" title="Expand sidebar (⌘\)" onClick={onToggleCollapsed}
          className="rounded-[var(--radius)] p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
          <PanelLeft size={18} />
        </button>
        <button aria-label="New meeting" title="New meeting" onClick={() => navigate('/new')}
          className="rounded-[var(--radius)] p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
          <Plus size={18} />
        </button>
        <button aria-label="Chat" title="Chat" onClick={() => navigate('/chat')}
          className="rounded-[var(--radius)] p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
          <MessageSquare size={18} />
        </button>
      </aside>
    )
  }

  return (
    <aside ref={asideRef} style={{ width }} className="relative flex h-full shrink-0 flex-col gap-3 border-r border-border bg-app-chrome p-3">
      <div className="flex items-center justify-between px-1 py-1">
        <span className="flex items-center gap-2 text-sm font-semibold">🥣 Muesli</span>
        <button aria-label="Collapse sidebar" title="Collapse sidebar (⌘\)" onClick={onToggleCollapsed}
          className="text-muted-foreground hover:text-foreground">
          <PanelLeftClose size={16} />
        </button>
      </div>
      <Button onClick={() => navigate('/new')} className="w-full"><Plus size={18} /> New meeting</Button>
      <label className="relative block">
        <Search size={16} className="absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
        <input aria-label="Search notes" value={query} onChange={(e) => onQuery(e.target.value)} placeholder="Search…"
          className="h-9 w-full rounded-[var(--radius)] border border-input bg-background pl-8 pr-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" />
      </label>
      {query.trim() && (
        <div className="flex items-center gap-1.5 px-1 text-xs text-muted-foreground">
          <label className="flex items-center gap-1">
            From
            <input
              type="date"
              aria-label="Search from date"
              value={dateFrom}
              onChange={(e) => onDateFrom(e.target.value)}
              className="h-7 rounded-[var(--radius)] border border-input bg-background px-1.5 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </label>
          <label className="flex items-center gap-1">
            To
            <input
              type="date"
              aria-label="Search to date"
              value={dateTo}
              onChange={(e) => onDateTo(e.target.value)}
              className="h-7 rounded-[var(--radius)] border border-input bg-background px-1.5 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </label>
        </div>
      )}
      {query.trim() && (
        <button onClick={() => onSaveSearchAsList(query.trim())}
          className="flex items-center gap-1 px-1 text-xs text-primary hover:underline">
          <Filter size={12} /> Save as smart list
        </button>
      )}

      <nav className="flex-1 overflow-y-auto">
        <NavLink to="/" end onClick={() => onSelectView({ type: 'all' })}
          className={cn(rowBase, activeView.type === 'all' ? active : idle)}>
          <span className="flex items-center gap-2"><FileText size={14} /> All notes</span>
        </NavLink>

        {folders.length > 0 ? (
          <>
            <div
              className={cn(section, 'flex items-center justify-between rounded-[var(--radius)]', dragOverFolder === '' && 'ring-1 ring-primary')}
              onDragOver={(e) => { if (isFolderDrag(e)) { e.preventDefault(); setDragOverFolder('') } }}
              onDragLeave={() => setDragOverFolder((cur) => (cur === '' ? null : cur))}
              onDrop={(e) => {
                setDragOverFolder(null)
                const folderId = e.dataTransfer.getData('text/folder-id')
                if (folderId) onReparentFolder(folderId, null)
              }}>
              <span><FolderIcon size={12} className="mr-1 inline" /> Folders</span>
              <button aria-label="New folder" onClick={onNewFolder} className="hover:text-primary"><Plus size={12} /></button>
            </div>
            <ul className="flex flex-col">{renderFolders(null, 0)}</ul>
          </>
        ) : (
          <button onClick={onNewFolder} className={cn(section, 'flex w-full items-center gap-1 hover:text-primary')}>
            <Plus size={12} /> New folder
          </button>
        )}

        {lists.length > 0 ? (
          <>
            <div className={cn(section, 'flex items-center justify-between')}>
              <span><Filter size={12} className="mr-1 inline" /> Smart lists</span>
              <button aria-label="New smart list" onClick={onNewList} className="hover:text-primary"><Plus size={12} /></button>
            </div>
            <ul className="flex flex-col">
              {lists.map((l) => {
                const desc = describeRule(l.rule)
                return (
                  <li key={l.id}
                    onMouseEnter={() => setHoveredListId(l.id)}
                    onMouseLeave={() => setHoveredListId((cur) => (cur === l.id ? null : cur))}
                    onFocus={() => setFocusedListId(l.id)}
                    onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setFocusedListId(null) }}>
                    <ContextMenu>
                    <ContextMenuTrigger asChild>
                    <div className={cn(rowBase, 'items-start', activeView.type === 'list' && activeView.id === l.id ? active : idle)}>
                      <button
                        onClick={() => selectView({ type: 'list', id: l.id })}
                        onDoubleClick={() => onEditList(l)}
                        className="flex min-w-0 flex-1 items-start text-left">
                        <span className="min-w-0 flex-1">
                          <span className="block truncate">{l.name}</span>
                          {desc && <span className="block truncate text-xs text-muted-foreground">{desc}</span>}
                        </span>
                        <span className="ml-2 shrink-0 text-xs text-muted-foreground">{listCount(l)}</span>
                      </button>
                      {showListMore(l.id) && (
                        <DropdownMenu onOpenChange={(open) => setOpenListDropdownId(open ? l.id : null)}>
                          <DropdownMenuTrigger asChild>
                            <button
                              aria-label={`More actions for ${l.name}`}
                              onClick={(e) => e.stopPropagation()}
                              className="ml-1 shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground">
                              <MoreHorizontal size={14} />
                            </button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onSelect={() => onEditList(l)}>Edit rule…</DropdownMenuItem>
                            <DropdownMenuItem destructive onSelect={() => onDeleteList(l.id)}>Move to Trash</DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      )}
                    </div>
                    </ContextMenuTrigger>
                    <ContextMenuContent>
                      <ContextMenuItem onSelect={() => onEditList(l)}>Edit rule…</ContextMenuItem>
                      <ContextMenuItem destructive onSelect={() => onDeleteList(l.id)}>Move to Trash</ContextMenuItem>
                    </ContextMenuContent>
                    </ContextMenu>
                  </li>
                )
              })}
            </ul>
          </>
        ) : (
          <button onClick={onNewList} className={cn(section, 'flex w-full items-center gap-1 hover:text-primary')}>
            <Plus size={12} /> New smart list
          </button>
        )}

        {tags.length > 0 && (
          <>
            <div className={section}><TagIcon size={12} className="mr-1 inline" /> Tags</div>
            <ul className="flex flex-col">
              {tags.map((t) => (
                <li key={t.name}>
                  <ContextMenu>
                  <ContextMenuTrigger asChild>
                  <button onClick={() => selectView(activeView.type === 'tag' && activeView.tag === t.name ? { type: 'all' } : { type: 'tag', tag: t.name })}
                    className={cn(rowBase, activeView.type === 'tag' && activeView.tag === t.name ? active : idle)}>
                    <span className="truncate">#{t.name}</span>
                    <span className="ml-2 shrink-0 text-xs text-muted-foreground">{t.count}</span>
                  </button>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem onSelect={() => onRenameTag({ id: t.id, name: t.name })}>Rename…</ContextMenuItem>
                    <ContextMenuItem onSelect={() => onSaveTagAsList(t.name)}>Save as smart list</ContextMenuItem>
                  </ContextMenuContent>
                  </ContextMenu>
                </li>
              ))}
            </ul>
          </>
        )}

        {suggestions.length > 0 && (
          <>
            <div className={section}><Sparkles size={12} className="mr-1 inline" /> Suggested</div>
            <ul className="flex flex-col">
              {suggestions.map((s) => (
                <li key={s.stem} className={cn(rowBase, 'text-muted-foreground')}>
                  <span className="truncate">{s.stem} <span className="text-xs">({s.count})</span></span>
                  <button aria-label={`Save ${s.stem} as a list`} onClick={() => onSaveSuggestion(s.stem)} className="ml-2 shrink-0 text-xs text-primary hover:underline">save</button>
                </li>
              ))}
            </ul>
          </>
        )}
      </nav>

      <NavLink to="/chat" className="flex items-center gap-2 rounded-[var(--radius)] px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted">
        <MessageSquare size={16} /> Chat
      </NavLink>
      <NavLink to="/coming-up" className="flex items-center gap-2 rounded-[var(--radius)] px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted">
        <Calendar size={16} /> Coming up
      </NavLink>
      <NavLink to="/action-items" className="flex items-center gap-2 rounded-[var(--radius)] px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted">
        <CheckSquare2 size={16} /> Action items
      </NavLink>
      <NavLink to="/people" className="flex items-center gap-2 rounded-[var(--radius)] px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted">
        <Users size={16} /> People
      </NavLink>
      <NavLink to="/trash" className="flex items-center gap-2 rounded-[var(--radius)] px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted">
        <Trash2 size={16} /> Trash
      </NavLink>
      <NavLink to="/settings" className="flex items-center gap-2 rounded-[var(--radius)] px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted">
        <Settings size={16} /> Settings
      </NavLink>

      {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/no-noninteractive-tabindex -- focusable separator widget (ARIA 1.2 §6.24) */}
      <div role="separator" tabIndex={0} aria-label="Resize sidebar" aria-orientation="vertical"
        onMouseDown={startResize}
        onKeyDown={(e) => {
          if (e.key === 'ArrowRight') { e.preventDefault(); onResize(Math.min(SIDEBAR_MAX_WIDTH, width + 16)) }
          else if (e.key === 'ArrowLeft') { e.preventDefault(); onResize(Math.max(SIDEBAR_MIN_WIDTH, width - 16)) }
        }}
        className="absolute right-0 top-0 h-full w-1 cursor-col-resize hover:bg-primary/30 focus:outline-none focus:bg-primary/30" />
    </aside>
  )
}
