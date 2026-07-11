export interface FindReplaceBarProps {
  mode: 'find' | 'replace'
  query: string
  replacement: string
  matchCount: number
  currentMatch: number
  onQueryChange: (value: string) => void
  onReplacementChange: (value: string) => void
  onNext: () => void
  onPrev: () => void
  onReplaceOne: () => void
  onReplaceAll: () => void
  onClose: () => void
}

export function FindReplaceBar({
  mode,
  query,
  replacement,
  matchCount,
  currentMatch,
  onQueryChange,
  onReplacementChange,
  onNext,
  onPrev,
  onReplaceOne,
  onReplaceAll,
  onClose,
}: FindReplaceBarProps) {
  const matchLabel =
    matchCount === 0 ? '0 of 0' : `${currentMatch + 1} of ${matchCount}`

  return (
    <div
      role="search"
      aria-label="Find and replace"
      className="flex flex-wrap items-center gap-1 rounded border border-border bg-muted/50 px-2 py-1 text-sm"
    >
      {/* Search row */}
      <input
        type="text"
        aria-label="Find"
        placeholder="Find…"
        value={query}
        onChange={e => onQueryChange(e.target.value)}
        className="h-6 min-w-[8rem] rounded border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
        autoFocus
      />

      <span
        aria-live="polite"
        aria-atomic="true"
        className="min-w-[4rem] text-center text-xs text-muted-foreground"
      >
        {matchLabel}
      </span>

      <button
        type="button"
        aria-label="Previous match"
        onClick={onPrev}
        disabled={matchCount === 0}
        className="flex h-6 w-6 items-center justify-center rounded hover:bg-accent disabled:opacity-40"
      >
        ▲
      </button>

      <button
        type="button"
        aria-label="Next match"
        onClick={onNext}
        disabled={matchCount === 0}
        className="flex h-6 w-6 items-center justify-center rounded hover:bg-accent disabled:opacity-40"
      >
        ▼
      </button>

      {/* Replace row — only in replace mode */}
      {mode === 'replace' && (
        <>
          <input
            type="text"
            aria-label="Replace"
            placeholder="Replace…"
            value={replacement}
            onChange={e => onReplacementChange(e.target.value)}
            className="h-6 min-w-[8rem] rounded border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
          />

          <button
            type="button"
            onClick={onReplaceOne}
            disabled={matchCount === 0}
            className="h-6 rounded bg-primary px-2 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-40"
          >
            Replace
          </button>

          <button
            type="button"
            onClick={onReplaceAll}
            disabled={matchCount === 0}
            className="h-6 rounded bg-primary px-2 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-40"
          >
            Replace All
          </button>
        </>
      )}

      {/* Close */}
      <button
        type="button"
        aria-label="Close find bar"
        onClick={onClose}
        className="ml-auto flex h-6 w-6 items-center justify-center rounded hover:bg-accent"
      >
        ×
      </button>
    </div>
  )
}
