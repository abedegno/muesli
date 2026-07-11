import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import type { Note, SearchMatch, SmartList, Folder } from '../../../shared/types'
import { tagIndex, type TagCount } from '@/lib/tagIndex'
import { evaluateList, countList } from '@/lib/smartList'
import { countFolder, descendantIds } from '@/lib/folders'
import { suggestRecurring } from '@/lib/recurring'
import { useSidebarPrefs } from '@/lib/sidebarPrefs'
import { sortNotesPinnedFirst } from '@/lib/noteOrdering'
import type { RuleGroup } from '../../../shared/types'
import { RuleEditor } from '@/components/RuleEditor'
import { FolderDialog } from '@/components/FolderDialog'
import { TagRenameDialog } from '@/components/TagRenameDialog'
import { useToast } from '@/components/ui/Toast'
import { CommandPalette } from '@/components/CommandPalette'
import { KeyboardShortcutsOverlay } from '@/components/KeyboardShortcutsOverlay'
import { ActivityFeed } from '@/components/ActivityFeed'
import { Sidebar } from './Sidebar'
import { MeetingRecordPrompt } from './MeetingRecordPrompt'
import { useMeetingDetectionLoop } from '@/hooks/useMeetingDetectionLoop'

export type ActiveView = { type: 'all' } | { type: 'tag'; tag: string } | { type: 'list'; id: string } | { type: 'folder'; id: string }

function reorderById<T extends { id: string }>(items: T[], movedId: string, afterId: string | null): T[] {
  const moved = items.find((item) => item.id === movedId)
  if (!moved) return items
  const rest = items.filter((item) => item.id !== movedId)
  if (afterId == null) return [moved, ...rest]
  const afterIndex = rest.findIndex((item) => item.id === afterId)
  if (afterIndex < 0) return items
  const next = rest.slice()
  next.splice(afterIndex + 1, 0, moved)
  return next
}

export function AppLayout() {
  const [notes, setNotes] = useState<Note[]>([])
  const [folderNotes, setFolderNotes] = useState<Note[]>([])
  const [lists, setLists] = useState<SmartList[]>([])
  const [folders, setFolders] = useState<Folder[]>([])
  // Sidebar tag list+counts come from the server (prepped for scale); the tag
  // *filter* and autocomplete stay client-side (see `tags` / tagIndex below).
  const [sidebarTags, setSidebarTags] = useState<TagCount[]>([])
  const [query, setQuery] = useState('')
  const [view, setView] = useState<ActiveView>({ type: 'all' })
  const [editing, setEditing] = useState<{ list?: SmartList; seed?: { name: string; rule: RuleGroup } } | null>(null)
  const [editingFolder, setEditingFolder] = useState<{ folder?: Folder; parentId?: string | null } | null>(null)
  const [renamingTag, setRenamingTag] = useState<{ id: string; name: string } | null>(null)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [searching, setSearching] = useState(false)
  const [searchMatches, setSearchMatches] = useState<SearchMatch[] | null>(null)
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  // Tracks the (query, date-range) key of the most recently *issued* search so
  // a late-arriving response can tell whether it's still wanted — guards
  // against a stale response from query A applying after the user has already
  // moved on to query B (see the query-change race test in AppLayout.test.tsx).
  const latestSearchKeyRef = useRef('')
  const [folderNotesLoaded, setFolderNotesLoaded] = useState(true)
  const { width, collapsed, setWidth, toggleCollapsed } = useSidebarPrefs()
  const { notify } = useToast()
  const navigate = useNavigate()
  const selectView = (next: ActiveView) => {
    if (next.type === 'folder') setFolderNotesLoaded(false)
    setView(next)
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPaletteOpen((o) => !o) }
      else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'n') { e.preventDefault(); navigate('/new') }
      else if ((e.metaKey || e.ctrlKey) && e.key === '\\') { e.preventDefault(); toggleCollapsed() }
      else if (e.key === '?') {
        const t = e.target as HTMLElement
        const tag = t.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t.isContentEditable) return
        if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return
        e.preventDefault()
        setShowShortcuts((o) => !o)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate, toggleCollapsed])

  const refresh = useCallback(() => {
    void muesli.listNotes().then(setNotes).catch(() => {
      notify('Failed to load notes — check your connection', 'error')
    }).finally(() => setLoaded(true))
    if (view.type === 'folder') {
      setFolderNotesLoaded(false)
      void muesli.listNotes(view.id).then(setFolderNotes).catch(() => {
        notify('Failed to load notes — check your connection', 'error')
      }).finally(() => setFolderNotesLoaded(true))
    } else {
      setFolderNotes([])
      setFolderNotesLoaded(true)
    }
    void muesli.listSmartLists().then(setLists).catch(() => {})
    void muesli.listFolders().then(setFolders).catch(() => {})
    void muesli.listTags().then(setSidebarTags).catch(() => {})
  }, [notify, view])
  useEffect(() => { refresh() }, [refresh])

  const { promptEvent, acceptPrompt, dismissPrompt } = useMeetingDetectionLoop({
    notes,
    loaded,
    navigate,
    notify,
    refresh,
  })

  // Debounced semantic-search refinement. Purely additive: it only ever appends
  // meaning-matched notes to today's instant lexical filter; on empty query,
  // failure, or no hits it stays null so the UI is exactly the lexical result.
  //
  // Stale-result guard (MUST-FIX from PR #221): the previous query's matches
  // are cleared SYNCHRONOUSLY the moment the query/date-range changes (not
  // just once the new response lands), so they can never render under a new,
  // different query while the new request is debouncing or in flight. The
  // response handlers additionally re-check `latestSearchKeyRef` against the
  // key captured when THIS request was issued, so an in-flight response only
  // applies if it's still the one the user is currently looking at.
  useEffect(() => {
    const q = query.trim()
    const key = `${q}|${dateFrom}|${dateTo}`
    latestSearchKeyRef.current = key
    if (!q) { setSearchMatches(null); setSearching(false); return }
    setSearchMatches(null)
    const h = setTimeout(() => {
      setSearching(true)
      muesli.search(q, { from: dateFrom || undefined, to: dateTo || undefined })
        .then((matches) => { if (latestSearchKeyRef.current === key) setSearchMatches(matches) })
        .catch(() => { if (latestSearchKeyRef.current === key) setSearchMatches(null) })
        .finally(() => { if (latestSearchKeyRef.current === key) setSearching(false) })
    }, 250)
    return () => { clearTimeout(h) }
  }, [query, dateFrom, dateTo])

  const tags = tagIndex(notes)
  const suggestions = suggestRecurring(notes, lists)

  // Ordered-unique note ids derived from the typed matches (preserves ranked
  // server order; a note can have several matches — title + transcript, etc).
  const semanticIds = useMemo(() => {
    if (!searchMatches) return null
    const seen = new Set<string>()
    const ids: string[] = []
    for (const m of searchMatches) {
      if (!seen.has(m.note_id)) { seen.add(m.note_id); ids.push(m.note_id) }
    }
    return ids
  }, [searchMatches])

  // Raw matches grouped by note id, for rendering match-type badges + snippets
  // per hit (a note can appear with more than one match, e.g. a title AND a
  // transcript hit).
  const semanticMatches = useMemo(() => {
    const map: Record<string, SearchMatch[]> = {}
    for (const m of searchMatches ?? []) {
      (map[m.note_id] ??= []).push(m)
    }
    return map
  }, [searchMatches])

  let inViewNotes: Note[]
  if (view.type === 'list') {
    const list = lists.find((l) => l.id === view.id)
    inViewNotes = list ? evaluateList(notes, list) : notes
  } else if (view.type === 'folder') {
    inViewNotes = folderNotes
  } else {
    const inView = (n: Note): boolean => {
      if (view.type === 'all') return true
      if (view.type === 'tag') return (n.tags ?? []).some((t) => t.toLowerCase() === view.tag.toLowerCase())
      return false
    }
    inViewNotes = notes.filter(inView)
  }
  const orderedInViewNotes = sortNotesPinnedFirst(inViewNotes)
  let lexicalNotes: Note[]
  let semanticNotes: Note[]
  if (!query.trim()) {
    lexicalNotes = orderedInViewNotes
    semanticNotes = []
  } else {
    const lq = query.toLowerCase()
    lexicalNotes = orderedInViewNotes.filter((n) => `${n.title} ${n.snippet ?? ''}`.toLowerCase().includes(lq))
    if (semanticIds && semanticIds.length) {
      const lexicalIds = new Set(lexicalNotes.map((n) => n.id))
      const rank = new Map(semanticIds.map((id, i) => [id, i] as const))
      semanticNotes = sortNotesPinnedFirst(
        [...orderedInViewNotes.filter((n) => !lexicalIds.has(n.id) && rank.has(n.id))]
          .sort((a, b) => rank.get(a.id)! - rank.get(b.id)!),
      )
    } else {
      semanticNotes = []
    }
  }

  const heading =
    view.type === 'tag' ? `#${view.tag}` :
    view.type === 'list' ? (lists.find((l) => l.id === view.id)?.name ?? 'List') :
    view.type === 'folder' ? (folders.find((f) => f.id === view.id)?.name ?? 'Folder') :
    'All notes'
  const isFiltered = view.type !== 'all' || query !== ''
  const currentNotesLoaded = loaded && (view.type !== 'folder' || folderNotesLoaded)

  async function saveSuggestion(stem: string) {
    try {
      await muesli.createSmartList(stem, { op: 'and', children: [{ field: 'title', operator: 'contains', value: stem }] })
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not save list', 'error')
    }
  }

  // Folder delete is a soft/reversible recycle-bin move, so it acts immediately (no confirm).
  async function deleteFolder(id: string) {
    try {
      await muesli.deleteFolder(id)
      if (view.type === 'folder' && view.id === id) setView({ type: 'all' })
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not delete folder', 'error')
    }
  }
  // Smart-list delete is a soft/reversible recycle-bin move, so it acts immediately (no confirm).
  async function deleteList(id: string) {
    try {
      await muesli.deleteSmartList(id)
      if (view.type === 'list' && view.id === id) setView({ type: 'all' })
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not delete list', 'error')
    }
  }

  async function reorderNote(folderId: string, movedNoteId: string, afterId: string | null) {
    setFolderNotes((cur) => reorderById(cur, movedNoteId, afterId))
    try {
      await muesli.reorderNoteInFolder(folderId, movedNoteId, afterId)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not reorder note', 'error')
    } finally {
      refresh()
    }
  }

  return (
    <div className="app-layout flex h-full [container-type:inline-size]">
      <Sidebar
        collapsed={collapsed} width={width} onToggleCollapsed={toggleCollapsed} onResize={setWidth}
        query={query} onQuery={setQuery}
        dateFrom={dateFrom} dateTo={dateTo} onDateFrom={setDateFrom} onDateTo={setDateTo}
        tags={sidebarTags} lists={lists} listCount={(l) => countList(notes, l)} suggestions={suggestions}
        folders={folders} folderCount={(f) => countFolder(notes, folders, f)}
        activeView={view} onSelectView={selectView}
        onNewList={() => setEditing({})} onEditList={(l) => setEditing({ list: l })} onSaveSuggestion={saveSuggestion}
        onDeleteList={deleteList}
        onSaveTagAsList={(name) => setEditing({ seed: { name, rule: { op: 'and', children: [{ field: 'tag', operator: 'is', value: name }] } } })}
        onRenameTag={(tag) => setRenamingTag(tag)}
        onSaveSearchAsList={(q) => setEditing({ seed: { name: q, rule: { op: 'and', children: [{ field: 'title', operator: 'contains', value: q }] } } })}
        onNewFolder={() => setEditingFolder({})} onEditFolder={(f) => setEditingFolder({ folder: f })}
        onNewSubfolder={(parentId) => setEditingFolder({ parentId })}
        onDeleteFolder={deleteFolder}
        onDropNote={async (folderId, noteId) => {
          try { await muesli.addNoteFolder(noteId, folderId); refresh() }
          catch (err) { notify(err instanceof Error ? err.message : 'Could not file note', 'error') }
        }}
        onReparentFolder={async (folderId, parentId) => {
          const f = folders.find((x) => x.id === folderId)
          if (!f || (f.parent_id ?? null) === (parentId ?? null)) return
          try { await muesli.updateFolder(folderId, f.name, parentId); refresh() }
          catch (err) { notify(err instanceof Error ? err.message : 'Could not move folder', 'error') }
        }}
        onReorderFolder={async (id, afterId) => {
          try { await muesli.reorderFolder(id, afterId); refresh() }
          catch (err) { notify(err instanceof Error ? err.message : 'Could not reorder folder', 'error') }
        }}
      />
      <main className="flex-1 overflow-y-auto">
        {promptEvent && (
          <MeetingRecordPrompt
            event={promptEvent}
            onAccept={() => { void acceptPrompt() }}
            onDismiss={dismissPrompt}
          />
        )}
        <Outlet context={{ notes: lexicalNotes, semanticNotes, semanticMatches, allNotes: notes, refresh, heading, isFiltered, view, searchQuery: query.trim(), folders, loaded: currentNotesLoaded, searching, onReorderNote: reorderNote }} />
      </main>
      {editingFolder && (
        <FolderDialog
          open
          title={editingFolder.folder ? 'Edit folder' : 'New folder'}
          initialName={editingFolder.folder?.name}
          initialParentId={editingFolder.folder?.parent_id ?? editingFolder.parentId ?? null}
          parentOptions={(() => {
            const excluded = editingFolder.folder ? descendantIds(folders, editingFolder.folder.id) : new Set<string>()
            return folders.filter((f) => !excluded.has(f.id)).map((f) => ({ id: f.id, name: f.name }))
          })()}
          onSave={async (name, parentId) => {
            try {
              if (editingFolder.folder) await muesli.updateFolder(editingFolder.folder.id, name, parentId)
              else await muesli.createFolder(name, parentId)
              refresh()
            } catch (err) { notify(err instanceof Error ? err.message : 'Could not save folder', 'error'); throw err }
          }}
          onDelete={editingFolder.folder ? async () => {
            const id = editingFolder.folder!.id
            setEditingFolder(null)
            await deleteFolder(id)
          } : undefined}
          onClose={() => setEditingFolder(null)}
        />
      )}
      {renamingTag && (
        <TagRenameDialog
          open
          initialName={renamingTag.name}
          onSave={async (newName) => {
            const oldName = renamingTag.name
            try {
              await muesli.renameTag(renamingTag.id, newName)
              // Keep the active tag view selected if it pointed at the renamed tag.
              if (view.type === 'tag' && view.tag === oldName) setView({ type: 'tag', tag: newName })
              refresh()
            } catch (err) {
              notify(err instanceof Error ? err.message : 'Could not rename tag', 'error')
              throw err
            }
          }}
          onClose={() => setRenamingTag(null)}
        />
      )}
      {editing && (
        <RuleEditor
          open
          title={editing.list ? 'Edit smart list' : 'New smart list'}
          initial={editing.list ?? (editing.seed ? ({ id: '', created_at: '', name: editing.seed.name, rule: editing.seed.rule } as SmartList) : undefined)}
          knownTags={tags.map((t) => t.name)}
          knownFolders={folders}
          onSave={async (name, rule) => {
            try {
              if (editing.list) await muesli.updateSmartList(editing.list.id, name, rule)
              else await muesli.createSmartList(name, rule)
              refresh()
            } catch (err) {
              notify(err instanceof Error ? err.message : 'Could not save list', 'error')
              throw err
            }
          }}
          onDelete={editing.list ? async () => {
            try {
              await muesli.deleteSmartList(editing.list!.id)
              if (view.type === 'list' && view.id === editing.list!.id) setView({ type: 'all' })
              setEditing(null); refresh()
            } catch (err) {
              notify(err instanceof Error ? err.message : 'Could not delete list', 'error')
            }
          } : undefined}
          onClose={() => setEditing(null)}
        />
      )}
      <KeyboardShortcutsOverlay open={showShortcuts} onClose={() => setShowShortcuts(false)} />
      <ActivityFeed />
      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        notes={notes}
        folders={folders}
        lists={lists}
        tags={tags}
        onSelectNote={(id) => navigate(`/notes/${id}`)}
        onSelectView={(v) => { selectView(v); navigate('/') }}
        actions={[
          { label: 'New meeting', run: () => navigate('/new') },
          { label: 'Manage templates', run: () => navigate('/templates') },
          { label: 'Settings', run: () => navigate('/settings') },
        ]}
      />
    </div>
  )
}
