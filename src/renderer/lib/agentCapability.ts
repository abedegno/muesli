import { useEffect, useState } from 'react'
import { muesli } from '@/api'

let capabilityRequest: Promise<boolean> | null = null

export function clearAgentCapabilityCache(): void {
  capabilityRequest = null
}

function loadAgentCapability(): Promise<boolean> {
  // Older/test bridges do not expose the new method. Treat them as capable so
  // an unavailable bridge never incorrectly disables existing functionality.
  if (typeof muesli.getCapabilities !== 'function') return Promise.resolve(true)
  capabilityRequest ??= Promise.resolve(muesli.getCapabilities()).then((value) => value.agentConfigured)
  return capabilityRequest
}

export function useAgentCapability(): boolean | null {
  const hasCapabilityBridge = typeof muesli.getCapabilities === 'function'
  const [configured, setConfigured] = useState<boolean | null>(hasCapabilityBridge ? null : true)

  useEffect(() => {
    if (!hasCapabilityBridge) return
    let cancelled = false
    void loadAgentCapability()
      .then((value) => {
        if (!cancelled) setConfigured(value)
      })
      .catch(() => {
        // A capability probe failure is not evidence that the agent is absent.
        if (!cancelled) setConfigured(null)
      })
    return () => {
      cancelled = true
    }
  }, [hasCapabilityBridge])

  return configured
}
