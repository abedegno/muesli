// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

const { getCapabilitiesMock } = vi.hoisted(() => ({ getCapabilitiesMock: vi.fn() }))

vi.mock('@/api', () => ({
  muesli: {
    getCapabilities: (...args: unknown[]) => getCapabilitiesMock(...args),
  },
}))

describe('useTeamSharingAvailable', () => {
  beforeEach(() => {
    getCapabilitiesMock.mockReset()
    vi.resetModules()
  })

  it('starts false and flips true once the bridge resolves teamSharingAvailable', async () => {
    getCapabilitiesMock.mockResolvedValue({ agentConfigured: true, teamSharingAvailable: true })
    const { useTeamSharingAvailable, clearTeamSharingAvailableCache } = await import('./teamSharing')
    clearTeamSharingAvailableCache()

    const { result } = renderHook(() => useTeamSharingAvailable())
    expect(result.current).toBe(false)

    await waitFor(() => expect(result.current).toBe(true))
  })

  it('stays false when the bridge reports teamSharingAvailable=false', async () => {
    getCapabilitiesMock.mockResolvedValue({ agentConfigured: true, teamSharingAvailable: false })
    const { useTeamSharingAvailable, clearTeamSharingAvailableCache } = await import('./teamSharing')
    clearTeamSharingAvailableCache()

    const { result } = renderHook(() => useTeamSharingAvailable())
    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current).toBe(false)
  })

  it('stays false when the capability probe rejects', async () => {
    getCapabilitiesMock.mockRejectedValue(new Error('offline'))
    const { useTeamSharingAvailable, clearTeamSharingAvailableCache } = await import('./teamSharing')
    clearTeamSharingAvailableCache()

    const { result } = renderHook(() => useTeamSharingAvailable())
    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current).toBe(false)
  })
})
