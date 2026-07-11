import { useCallback, useState } from 'react'

// Sidebar width is user-resizable within these bounds; default matches the
// original fixed `w-64` (16rem = 256px).
export const SIDEBAR_MIN_WIDTH = 200
export const SIDEBAR_MAX_WIDTH = 420
export const SIDEBAR_DEFAULT_WIDTH = 256

const WIDTH_KEY = 'muesli.sidebar.width'
const COLLAPSED_KEY = 'muesli.sidebar.collapsed'
const FOLDER_COLLAPSED_KEY = 'muesli.sidebar.folderCollapsed'

// clampWidth keeps a width within [MIN, MAX]; NaN falls back to the default.
export function clampWidth(w: number): number {
  if (Number.isNaN(w)) return SIDEBAR_DEFAULT_WIDTH
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, w))
}

// readStoredWidth returns the persisted (clamped) width, or the default when
// absent/unparseable/unavailable.
export function readStoredWidth(): number {
  try {
    const raw = localStorage.getItem(WIDTH_KEY)
    if (raw == null) return SIDEBAR_DEFAULT_WIDTH
    const n = parseInt(raw, 10)
    if (Number.isNaN(n)) return SIDEBAR_DEFAULT_WIDTH
    return clampWidth(n)
  } catch {
    return SIDEBAR_DEFAULT_WIDTH
  }
}

// readStoredCollapsed returns the persisted collapsed flag (default false).
export function readStoredCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

// readStoredFolderCollapsed returns the persisted set of collapsed folder ids,
// filtered to only those still present in `existingIds` so stale ids (deleted
// folders) don't accumulate. Returns an empty set when absent/unparseable/
// unavailable or when the stored value isn't an array.
export function readStoredFolderCollapsed(existingIds: Iterable<string>): Set<string> {
  const present = existingIds instanceof Set ? existingIds : new Set(existingIds)
  try {
    const raw = localStorage.getItem(FOLDER_COLLAPSED_KEY)
    if (raw == null) return new Set()
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((id): id is string => typeof id === 'string' && present.has(id)))
  } catch {
    return new Set()
  }
}

// writeStoredFolderCollapsed persists the set as a JSON array of ids; failures
// (storage unavailable) are swallowed so the caller's in-memory state stands.
export function writeStoredFolderCollapsed(ids: Set<string>): void {
  try {
    localStorage.setItem(FOLDER_COLLAPSED_KEY, JSON.stringify([...ids]))
  } catch {
    /* storage unavailable — keep in-memory value */
  }
}

interface SidebarPrefs {
  width: number
  collapsed: boolean
  setWidth: (w: number) => void
  toggleCollapsed: () => void
}

// useSidebarPrefs owns the sidebar's width + collapsed state, seeded from and
// persisted to localStorage. Width is clamped on every set.
export function useSidebarPrefs(): SidebarPrefs {
  const [width, setWidthState] = useState<number>(() => readStoredWidth())
  const [collapsed, setCollapsedState] = useState<boolean>(() => readStoredCollapsed())

  const setWidth = useCallback((w: number) => {
    const c = clampWidth(w)
    setWidthState(c)
    try {
      localStorage.setItem(WIDTH_KEY, String(c))
    } catch {
      /* storage unavailable — keep in-memory value */
    }
  }, [])

  const toggleCollapsed = useCallback(() => {
    setCollapsedState((prev) => {
      const next = !prev
      try {
        localStorage.setItem(COLLAPSED_KEY, next ? '1' : '0')
      } catch {
        /* storage unavailable */
      }
      return next
    })
  }, [])

  return { width, collapsed, setWidth, toggleCollapsed }
}
