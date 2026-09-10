import { useEffect, useState } from 'react'
import { muesli } from '@/api'

// Mirrors agentCapability.ts's caching/fallback shape for the sibling
// capability added by issue #12: whether folder sharing is meaningful on
// this deployment (a second user exists). A single-user deployment hides the
// share toggle client-side; the server behavior is identical either way.
let capabilityRequest: Promise<boolean> | null = null

export function clearTeamSharingAvailableCache(): void {
  capabilityRequest = null
}

function loadTeamSharingAvailable(): Promise<boolean> {
  // Older/test bridges do not expose the new method. Treat sharing as
  // unavailable so an unavailable bridge never surfaces a toggle whose
  // backing endpoint might not exist.
  if (typeof muesli.getCapabilities !== 'function') return Promise.resolve(false)
  capabilityRequest ??= Promise.resolve(muesli.getCapabilities()).then((value) => value.teamSharingAvailable)
  return capabilityRequest
}

export function useTeamSharingAvailable(): boolean {
  const hasCapabilityBridge = typeof muesli.getCapabilities === 'function'
  const [available, setAvailable] = useState(false)

  useEffect(() => {
    if (!hasCapabilityBridge) return
    let cancelled = false
    void loadTeamSharingAvailable()
      .then((value) => {
        if (!cancelled) setAvailable(value)
      })
      .catch(() => {
        if (!cancelled) setAvailable(false)
      })
    return () => {
      cancelled = true
    }
  }, [hasCapabilityBridge])

  return available
}
