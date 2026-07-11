// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, cleanup } from '@testing-library/react'
import React from 'react'
import { activityReducer, ActivityProvider, useActivity } from './activityStore'
import type { ActivityItem } from './activityStore'

// ---------------------------------------------------------------------------
// Reducer tests (pure logic — no jsdom needed, but we use the same file for
// consistency and to share the auto-remove timer test that needs renderHook).
// ---------------------------------------------------------------------------

describe('activityReducer — ADD_UPLOAD', () => {
  it('adds an upload item with default phase requesting-url', () => {
    const state = activityReducer([], {
      type: 'ADD_UPLOAD',
      noteId: 'n1',
      noteTitle: 'My Meeting',
    })
    expect(state).toHaveLength(1)
    expect(state[0]).toMatchObject({
      kind: 'upload',
      id: 'n1',
      noteId: 'n1',
      noteTitle: 'My Meeting',
      phase: 'requesting-url',
      done: false,
    })
  })

  it('does not duplicate if an upload item already exists for the same noteId', () => {
    const initial: ActivityItem[] = [
      { kind: 'upload', id: 'n1', noteId: 'n1', noteTitle: 'My Meeting', phase: 'uploading-audio', done: false },
    ]
    const state = activityReducer(initial, {
      type: 'ADD_UPLOAD',
      noteId: 'n1',
      noteTitle: 'My Meeting',
    })
    expect(state).toHaveLength(1)
    expect(state[0].kind === 'upload' && (state[0] as Extract<ActivityItem, { kind: 'upload' }>).phase).toBe('uploading-audio')
  })
})

describe('activityReducer — UPDATE_UPLOAD', () => {
  it('updates the phase of an existing upload item', () => {
    const initial: ActivityItem[] = [
      { kind: 'upload', id: 'n1', noteId: 'n1', noteTitle: 'My Meeting', phase: 'requesting-url', done: false },
    ]
    const state = activityReducer(initial, {
      type: 'UPDATE_UPLOAD',
      noteId: 'n1',
      phase: 'uploading-audio',
      done: false,
    })
    expect(state).toHaveLength(1)
    const item = state[0] as Extract<ActivityItem, { kind: 'upload' }>
    expect(item.phase).toBe('uploading-audio')
    expect(item.done).toBe(false)
  })

  it('marks done=true when updating to done phase', () => {
    const initial: ActivityItem[] = [
      { kind: 'upload', id: 'n1', noteId: 'n1', noteTitle: 'My Meeting', phase: 'confirming-upload', done: false },
    ]
    const state = activityReducer(initial, {
      type: 'UPDATE_UPLOAD',
      noteId: 'n1',
      phase: 'done',
      done: true,
    })
    const item = state[0] as Extract<ActivityItem, { kind: 'upload' }>
    expect(item.phase).toBe('done')
    expect(item.done).toBe(true)
  })
})

describe('activityReducer — ADD_PROCESSING', () => {
  it('adds a processing item with the given status', () => {
    const state = activityReducer([], {
      type: 'ADD_PROCESSING',
      noteId: 'n2',
      noteTitle: 'Sprint Retro',
      status: 'transcribing',
    })
    expect(state).toHaveLength(1)
    expect(state[0]).toMatchObject({
      kind: 'processing',
      id: 'n2',
      noteId: 'n2',
      noteTitle: 'Sprint Retro',
      status: 'transcribing',
      done: false,
    })
  })

  it('does not duplicate if processing item for same noteId exists', () => {
    const initial: ActivityItem[] = [
      { kind: 'processing', id: 'n2', noteId: 'n2', noteTitle: 'Sprint Retro', status: 'transcribing', done: false },
    ]
    const state = activityReducer(initial, {
      type: 'ADD_PROCESSING',
      noteId: 'n2',
      noteTitle: 'Sprint Retro',
      status: 'summarizing',
    })
    expect(state).toHaveLength(1)
    const item = state[0] as Extract<ActivityItem, { kind: 'processing' }>
    expect(item.status).toBe('transcribing')
  })
})

describe('activityReducer — UPDATE_PROCESSING', () => {
  it('updates the status of an existing processing item', () => {
    const initial: ActivityItem[] = [
      { kind: 'processing', id: 'n2', noteId: 'n2', noteTitle: 'Sprint Retro', status: 'transcribing', done: false },
    ]
    const state = activityReducer(initial, {
      type: 'UPDATE_PROCESSING',
      noteId: 'n2',
      status: 'summarizing',
      done: false,
    })
    const item = state[0] as Extract<ActivityItem, { kind: 'processing' }>
    expect(item.status).toBe('summarizing')
    expect(item.done).toBe(false)
  })

  it('marks done=true when status becomes ready', () => {
    const initial: ActivityItem[] = [
      { kind: 'processing', id: 'n2', noteId: 'n2', noteTitle: 'Sprint Retro', status: 'summarizing', done: false },
    ]
    const state = activityReducer(initial, {
      type: 'UPDATE_PROCESSING',
      noteId: 'n2',
      status: 'ready',
      done: true,
    })
    const item = state[0] as Extract<ActivityItem, { kind: 'processing' }>
    expect(item.status).toBe('ready')
    expect(item.done).toBe(true)
  })
})

describe('activityReducer — DISMISS', () => {
  it('removes an item by id', () => {
    const initial: ActivityItem[] = [
      { kind: 'upload', id: 'n1', noteId: 'n1', noteTitle: 'Meeting', phase: 'uploading-audio', done: false },
      { kind: 'processing', id: 'n2', noteId: 'n2', noteTitle: 'Retro', status: 'transcribing', done: false },
    ]
    const state = activityReducer(initial, { type: 'DISMISS', id: 'n1' })
    expect(state).toHaveLength(1)
    expect(state[0].id).toBe('n2')
  })
})

describe('activityReducer — multiple concurrent items', () => {
  it('tracks multiple items independently', () => {
    let state: ActivityItem[] = []
    state = activityReducer(state, { type: 'ADD_UPLOAD', noteId: 'n1', noteTitle: 'Note 1' })
    state = activityReducer(state, { type: 'ADD_PROCESSING', noteId: 'n2', noteTitle: 'Note 2', status: 'transcribing' })
    expect(state).toHaveLength(2)

    state = activityReducer(state, { type: 'UPDATE_UPLOAD', noteId: 'n1', phase: 'uploading-audio', done: false })
    state = activityReducer(state, { type: 'UPDATE_PROCESSING', noteId: 'n2', status: 'summarizing', done: false })

    const upload = state.find((i) => i.kind === 'upload') as Extract<ActivityItem, { kind: 'upload' }>
    const processing = state.find((i) => i.kind === 'processing') as Extract<ActivityItem, { kind: 'processing' }>
    expect(upload.phase).toBe('uploading-audio')
    expect(processing.status).toBe('summarizing')
  })
})

// ---------------------------------------------------------------------------
// Provider tests — require jsdom for React rendering.
// ---------------------------------------------------------------------------

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('ActivityProvider — auto-remove after 2 s', () => {
  it('removes a done=true item after 2000 ms (fake timers)', async () => {
    vi.useFakeTimers()

    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(ActivityProvider, null, children)

    const { result } = renderHook(() => useActivity(), { wrapper })

    // Add and immediately mark done via dispatch through the public API.
    act(() => {
      result.current.addUpload('n1', 'Meeting')
    })
    act(() => {
      result.current.updateUpload('n1', 'done', true)
    })

    expect(result.current.items).toHaveLength(1)
    expect(result.current.items[0].done).toBe(true)

    // Advance past the 2 s auto-remove timer.
    act(() => {
      vi.advanceTimersByTime(2001)
    })

    expect(result.current.items).toHaveLength(0)
  })

  it('does NOT remove an item before 2000 ms have elapsed', () => {
    vi.useFakeTimers()

    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(ActivityProvider, null, children)

    const { result } = renderHook(() => useActivity(), { wrapper })

    act(() => {
      result.current.addUpload('n1', 'Meeting')
      result.current.updateUpload('n1', 'done', true)
    })

    act(() => {
      vi.advanceTimersByTime(1999)
    })

    // Still present — timer hasn't fired yet.
    expect(result.current.items).toHaveLength(1)
  })
})

describe('ActivityProvider — dismiss', () => {
  it('removes an item immediately when dismiss is called', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(ActivityProvider, null, children)

    const { result } = renderHook(() => useActivity(), { wrapper })

    act(() => {
      result.current.addUpload('n1', 'Meeting')
    })
    expect(result.current.items).toHaveLength(1)

    act(() => {
      result.current.dismiss('n1')
    })
    expect(result.current.items).toHaveLength(0)
  })
})
