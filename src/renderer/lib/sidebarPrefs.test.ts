// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import {
  clampWidth,
  readStoredWidth,
  readStoredCollapsed,
  useSidebarPrefs,
  SIDEBAR_DEFAULT_WIDTH,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MAX_WIDTH,
} from './sidebarPrefs'

beforeEach(() => localStorage.clear())

describe('clampWidth', () => {
  it('clamps below min and above max, keeps in-range, defaults NaN', () => {
    expect(clampWidth(50)).toBe(SIDEBAR_MIN_WIDTH)
    expect(clampWidth(9999)).toBe(SIDEBAR_MAX_WIDTH)
    expect(clampWidth(300)).toBe(300)
    expect(clampWidth(NaN)).toBe(SIDEBAR_DEFAULT_WIDTH)
  })
})

describe('readStored*', () => {
  it('defaults when storage empty', () => {
    expect(readStoredWidth()).toBe(SIDEBAR_DEFAULT_WIDTH)
    expect(readStoredCollapsed()).toBe(false)
  })
  it('reads + clamps persisted width', () => {
    localStorage.setItem('muesli.sidebar.width', '300')
    expect(readStoredWidth()).toBe(300)
    localStorage.setItem('muesli.sidebar.width', '9999')
    expect(readStoredWidth()).toBe(SIDEBAR_MAX_WIDTH)
    localStorage.setItem('muesli.sidebar.width', 'garbage')
    expect(readStoredWidth()).toBe(SIDEBAR_DEFAULT_WIDTH)
  })
  it('reads persisted collapsed flag', () => {
    localStorage.setItem('muesli.sidebar.collapsed', '1')
    expect(readStoredCollapsed()).toBe(true)
  })
})

describe('useSidebarPrefs', () => {
  it('seeds from storage, persists width (clamped) and collapsed on change', () => {
    localStorage.setItem('muesli.sidebar.width', '320')
    localStorage.setItem('muesli.sidebar.collapsed', '1')
    const { result } = renderHook(() => useSidebarPrefs())
    expect(result.current.width).toBe(320)
    expect(result.current.collapsed).toBe(true)

    act(() => result.current.setWidth(9999))
    expect(result.current.width).toBe(SIDEBAR_MAX_WIDTH)
    expect(localStorage.getItem('muesli.sidebar.width')).toBe(String(SIDEBAR_MAX_WIDTH))

    act(() => result.current.toggleCollapsed())
    expect(result.current.collapsed).toBe(false)
    expect(localStorage.getItem('muesli.sidebar.collapsed')).toBe('0')
  })
})
