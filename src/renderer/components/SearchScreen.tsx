import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search as SearchIcon } from 'lucide-react'
import { muesli } from '@/api'
import { EmptyState } from './EmptyState'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { Input } from './ui/Input'
import { Skeleton } from './ui/Skeleton'
import type { Folder, Note, PersonWithCompany, SearchMatch } from '../../shared/types'

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

function personLabel(person: PersonWithCompany): string {
  return person.display_name || person.primary_email || 'Untitled person'
}

function folderLabel(folder: Folder): string {
  return folder.name || 'Untitled folder'
}

function matchTypeLabel(matchType: SearchMatch['match_type']): string {
  switch (matchType) {
    case 'title':
      return 'Title'
    case 'transcript':
      return 'Transcript'
    case 'summary':
      return 'Summary'
  }
}

function matchPath(noteId: string, match: SearchMatch): string {
  if (match.match_type === 'transcript' && match.segment_id) {
    return `/notes/${noteId}?segment=${encodeURIComponent(match.segment_id)}`
  }
  return `/notes/${noteId}`
}

function noteTitle(note: Note | undefined, noteId: string): string {
  return note?.title || `Untitled note (${noteId.slice(0, 8)})`
}

function isFilled(value: string): boolean {
  return value.trim().length > 0
}

export function SearchScreen() {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [personId, setPersonId] = useState('')
  const [folderId, setFolderId] = useState('')
  const [tag, setTag] = useState('')
  const [people, setPeople] = useState<PersonWithCompany[] | null>(null)
  const [folders, setFolders] = useState<Folder[] | null>(null)
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [results, setResults] = useState<SearchMatch[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [searchStarted, setSearchStarted] = useState(false)
  const searchRequestId = useRef(0)

  useEffect(() => {
    let cancelled = false

    void Promise.allSettled([muesli.listPeople(), muesli.listFolders(), muesli.listNotes()])
      .then(([peopleRes, foldersRes, notesRes]) => {
        if (cancelled) return
        if (peopleRes.status === 'fulfilled') setPeople(peopleRes.value)
        else setPeople([])
        if (foldersRes.status === 'fulfilled') setFolders(foldersRes.value)
        else setFolders([])
        if (notesRes.status === 'fulfilled') setNotes(notesRes.value)
        else setNotes([])
      })

    return () => {
      cancelled = true
    }
  }, [])

  const noteLookup = useMemo(() => new Map((notes ?? []).map((note) => [note.id, note] as const)), [notes])
  const sortedPeople = useMemo(() => [...(people ?? [])].sort((a, b) => personLabel(a).localeCompare(personLabel(b), undefined, { sensitivity: 'base' })), [people])
  const sortedFolders = useMemo(() => [...(folders ?? [])].sort((a, b) => folderLabel(a).localeCompare(folderLabel(b), undefined, { sensitivity: 'base' })), [folders])

  const hasCriteria = isFilled(query) || isFilled(from) || isFilled(to) || isFilled(personId) || isFilled(folderId) || isFilled(tag)

  useEffect(() => {
    if (!searchStarted && !hasCriteria) return

    const q = query.trim()
    const requestId = ++searchRequestId.current
    setLoading(true)
    setError(null)

    void muesli.search(q, {
      from: from || undefined,
      to: to || undefined,
      personId: personId || undefined,
      folderId: folderId || undefined,
      tag: tag.trim() || undefined,
    })
      .then((result) => {
        if (searchRequestId.current !== requestId) return
        setResults(Array.isArray(result) ? result : result.matches)
      })
      .catch((err) => {
        if (searchRequestId.current !== requestId) return
        setResults([])
        setError(errorMessage(err))
      })
      .finally(() => {
        if (searchRequestId.current !== requestId) return
        setLoading(false)
      })

    return () => {
      searchRequestId.current += 1
    }
  }, [query, from, to, personId, folderId, tag, searchStarted, hasCriteria])

  const groupedResults = useMemo(() => {
    const groups = new Map<string, SearchMatch[]>()
    const order: string[] = []
    for (const match of results ?? []) {
      if (!groups.has(match.note_id)) {
        groups.set(match.note_id, [])
        order.push(match.note_id)
      }
      groups.get(match.note_id)!.push(match)
    }
    return order.map((noteId) => ({
      noteId,
      note: noteLookup.get(noteId),
      matches: groups.get(noteId) ?? [],
    }))
  }, [results, noteLookup])

  const totalMatches = results?.length ?? 0
  const hasQueried = searchStarted || hasCriteria

  const clearFilters = () => {
    setSearchStarted(true)
    setFrom('')
    setTo('')
    setPersonId('')
    setFolderId('')
    setTag('')
  }

  return (
    <div className="mx-auto max-w-5xl p-6">
      <div className="mb-6 flex flex-col gap-2">
        <div className="flex flex-col gap-1">
          <h1 className="font-serif text-xl font-semibold">Search</h1>
          <p className="text-sm text-muted-foreground">
            Search note titles, transcripts, and summaries. Filters are combined with AND.
          </p>
          {hasQueried && !loading && !error ? (
            <p className="text-sm text-muted-foreground">
              {groupedResults.length} note{groupedResults.length === 1 ? '' : 's'} matched, {totalMatches} total match{totalMatches === 1 ? '' : 'es'}.
            </p>
          ) : null}
        </div>
      </div>

      <section className="rounded-[var(--radius)] border border-border bg-card p-4">
        <div className="grid gap-4">
          <label htmlFor="search-query" className="block">
            <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-muted-foreground">Query</span>
            <div className="relative">
              <SearchIcon size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
              <Input
                id="search-query"
                aria-label="Search query"
                placeholder="Type keywords to search..."
                value={query}
                onChange={(e) => {
                  setSearchStarted(true)
                  setQuery(e.target.value)
                }}
                className="pl-9"
              />
            </div>
          </label>

          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <label htmlFor="search-from" className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-muted-foreground">From</span>
              <Input
                id="search-from"
                type="date"
                aria-label="Search from date"
                value={from}
                onChange={(e) => {
                  setSearchStarted(true)
                  setFrom(e.target.value)
                }}
              />
            </label>

            <label htmlFor="search-to" className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-muted-foreground">To</span>
              <Input
                id="search-to"
                type="date"
                aria-label="Search to date"
                value={to}
                onChange={(e) => {
                  setSearchStarted(true)
                  setTo(e.target.value)
                }}
              />
            </label>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-muted-foreground">Person</span>
              <select
                aria-label="Search person"
                value={personId}
                onChange={(e) => {
                  setSearchStarted(true)
                  setPersonId(e.target.value)
                }}
                className="h-10 w-full rounded-[var(--radius)] border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <option value="">Any person</option>
                {sortedPeople.map((person) => (
                  <option key={person.id} value={person.id}>
                    {personLabel(person)}
                  </option>
                ))}
              </select>
            </label>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-muted-foreground">Folder</span>
              <select
                aria-label="Search folder"
                value={folderId}
                onChange={(e) => {
                  setSearchStarted(true)
                  setFolderId(e.target.value)
                }}
                className="h-10 w-full rounded-[var(--radius)] border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <option value="">Any folder</option>
                {sortedFolders.map((folder) => (
                  <option key={folder.id} value={folder.id}>
                    {folderLabel(folder)}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <label htmlFor="search-tag" className="block">
            <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-muted-foreground">Tag</span>
            <Input
              id="search-tag"
              aria-label="Search tag"
              placeholder="Optional tag name"
              value={tag}
              onChange={(e) => {
                setSearchStarted(true)
                setTag(e.target.value)
              }}
            />
          </label>

          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">
              Search params map to `q`, `from`, `to`, `person_id`, `folder_id`, and `tag`.
            </p>
            <Button type="button" variant="secondary" size="sm" onClick={clearFilters}>
              Clear filters
            </Button>
          </div>
        </div>
      </section>

      <div className="mt-6">
        {loading ? (
          <div className="flex flex-col gap-3" role="status" aria-label="Loading search results">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-20 w-full" />
          </div>
        ) : error ? (
          <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
            <p className="font-medium">Could not load search results</p>
            <p className="mt-1 break-words">{error}</p>
          </div>
        ) : !hasQueried ? (
          <EmptyState
            title="Search your notes"
            hint="Type a query or add filters to search across note titles, transcripts, and summaries."
          />
        ) : groupedResults.length === 0 ? (
          <EmptyState
            title="No matches"
            hint="Try a broader query or clear filters to search again."
          />
        ) : (
          <ul className="flex flex-col gap-3">
            {groupedResults.map(({ noteId, note, matches }) => (
              <li key={noteId} className="rounded-[var(--radius)] border border-border bg-card p-4">
                <button
                  type="button"
                  onClick={() => navigate(`/notes/${noteId}`)}
                  className="flex w-full items-start justify-between gap-4 text-left"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{noteTitle(note, noteId)}</p>
                    <p className="text-xs text-muted-foreground">
                      {matches.length} match{matches.length === 1 ? '' : 'es'}
                    </p>
                  </div>
                  <p className="shrink-0 text-xs text-muted-foreground">{noteId.slice(0, 8)}</p>
                </button>

                <div className="mt-3 flex flex-col gap-2">
                  {matches.map((match, index) => (
                    <button
                      key={`${noteId}-${match.match_type}-${index}`}
                      type="button"
                      aria-label={`${matchTypeLabel(match.match_type)} match${match.snippet ? `: ${match.snippet}` : ''}`}
                      onClick={() => navigate(matchPath(noteId, match))}
                      className="flex items-start gap-3 rounded-[var(--radius)] bg-muted/40 px-3 py-2 text-left text-sm hover:bg-muted"
                    >
                      <Badge tone="neutral" className="shrink-0">
                        {matchTypeLabel(match.match_type)}
                      </Badge>
                      <span className="min-w-0 flex-1 text-muted-foreground">
                        {match.snippet || 'Open note'}
                      </span>
                    </button>
                  ))}
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
