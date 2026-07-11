import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useReducer,
  useRef,
  type ReactNode,
} from 'react'
import type { NoteStatus } from '../../shared/types'

// UploadPhase is defined locally here so the renderer stays decoupled from
// the main-process bundle. The string values must stay in sync with
// src/main/uploadMachine.ts.
export type UploadPhase =
  | 'requesting-url'
  | 'uploading-audio'
  | 'confirming-upload'
  | 'done'
  | 'error'

export type ActivityItem =
  | {
      kind: 'upload'
      id: string
      noteId: string
      noteTitle: string
      phase: UploadPhase
      done: boolean
    }
  | {
      kind: 'processing'
      id: string
      noteId: string
      noteTitle: string
      status: NoteStatus
      done: boolean
    }
// extensible: future | { kind: 'model-download'; id: string; label: string; progress: number; done: boolean }

type State = ActivityItem[]

type Action =
  | { type: 'ADD_UPLOAD'; noteId: string; noteTitle: string }
  | { type: 'UPDATE_UPLOAD'; noteId: string; phase: UploadPhase; done: boolean }
  | { type: 'ADD_PROCESSING'; noteId: string; noteTitle: string; status: NoteStatus }
  | { type: 'UPDATE_PROCESSING'; noteId: string; status: NoteStatus; done: boolean }
  | { type: 'DISMISS'; id: string }

export function activityReducer(state: State, action: Action): State {
  switch (action.type) {
    case 'ADD_UPLOAD': {
      // Don't duplicate if an upload item for this note already exists.
      if (state.some((item) => item.kind === 'upload' && item.noteId === action.noteId)) {
        return state
      }
      return [
        ...state,
        {
          kind: 'upload',
          id: action.noteId,
          noteId: action.noteId,
          noteTitle: action.noteTitle,
          phase: 'requesting-url',
          done: false,
        },
      ]
    }
    case 'UPDATE_UPLOAD': {
      return state.map((item) =>
        item.kind === 'upload' && item.noteId === action.noteId
          ? { ...item, phase: action.phase, done: action.done }
          : item,
      )
    }
    case 'ADD_PROCESSING': {
      // Don't duplicate if a processing item for this note already exists.
      if (state.some((item) => item.kind === 'processing' && item.noteId === action.noteId)) {
        return state
      }
      return [
        ...state,
        {
          kind: 'processing',
          id: action.noteId,
          noteId: action.noteId,
          noteTitle: action.noteTitle,
          status: action.status,
          done: false,
        },
      ]
    }
    case 'UPDATE_PROCESSING': {
      return state.map((item) =>
        item.kind === 'processing' && item.noteId === action.noteId
          ? { ...item, status: action.status, done: action.done }
          : item,
      )
    }
    case 'DISMISS': {
      return state.filter((item) => item.id !== action.id)
    }
    default:
      return state
  }
}

interface ActivityApi {
  items: ActivityItem[]
  addUpload: (noteId: string, noteTitle: string) => void
  updateUpload: (noteId: string, phase: UploadPhase, done: boolean) => void
  addProcessing: (noteId: string, noteTitle: string, status: NoteStatus) => void
  updateProcessing: (noteId: string, status: NoteStatus, done: boolean) => void
  dismiss: (id: string) => void
}

const noop = () => {}
const defaultApi: ActivityApi = {
  items: [],
  addUpload: noop,
  updateUpload: noop,
  addProcessing: noop,
  updateProcessing: noop,
  dismiss: noop,
}

const ActivityCtx = createContext<ActivityApi>(defaultApi)

export function useActivity(): ActivityApi {
  return useContext(ActivityCtx)
}

export function ActivityProvider({ children }: { children: ReactNode }) {
  const [items, dispatch] = useReducer(activityReducer, [])

  // Map of item id -> timer handle. A timer is started ONCE when an item first
  // transitions to done=true; other items-list changes do NOT reset the countdown.
  const autoRemoveTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())

  // Schedule auto-remove timers for newly-done items; cancel timers for items
  // that have already been synchronously dismissed.
  useEffect(() => {
    items.forEach((item) => {
      if (item.done && !autoRemoveTimers.current.has(item.id)) {
        const t = setTimeout(() => {
          dispatch({ type: 'DISMISS', id: item.id })
          autoRemoveTimers.current.delete(item.id)
        }, 2000)
        autoRemoveTimers.current.set(item.id, t)
      }
    })
    // Cancel timers for items that were synchronously dismissed (no longer in list).
    const currentIds = new Set(items.map((i) => i.id))
    autoRemoveTimers.current.forEach((t, id) => {
      if (!currentIds.has(id)) {
        clearTimeout(t)
        autoRemoveTimers.current.delete(id)
      }
    })
    // No cleanup return: the map owns the timer lifecycle.
  }, [items])

  // On unmount, clear any pending timers.
  useEffect(() => {
    return () => autoRemoveTimers.current.forEach(clearTimeout)
  }, [])

  const addUpload = useCallback((noteId: string, noteTitle: string) => {
    dispatch({ type: 'ADD_UPLOAD', noteId, noteTitle })
  }, [])

  const updateUpload = useCallback((noteId: string, phase: UploadPhase, done: boolean) => {
    dispatch({ type: 'UPDATE_UPLOAD', noteId, phase, done })
  }, [])

  const addProcessing = useCallback((noteId: string, noteTitle: string, status: NoteStatus) => {
    dispatch({ type: 'ADD_PROCESSING', noteId, noteTitle, status })
  }, [])

  const updateProcessing = useCallback((noteId: string, status: NoteStatus, done: boolean) => {
    dispatch({ type: 'UPDATE_PROCESSING', noteId, status, done })
  }, [])

  const dismiss = useCallback((id: string) => {
    dispatch({ type: 'DISMISS', id })
  }, [])

  return (
    <ActivityCtx.Provider
      value={{ items, addUpload, updateUpload, addProcessing, updateProcessing, dismiss }}
    >
      {children}
    </ActivityCtx.Provider>
  )
}
