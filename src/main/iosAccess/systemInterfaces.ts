/**
 * Adapts Node's `os.networkInterfaces()` to `RawInterface[]` (issue #767 Task
 * 6), the shape `IOSAccessController.enumerate()` filters through
 * `eligibleCandidates`. Mirrors Go's `SystemInterfaceAddressSource` in
 * `internal/iosaccess/control.go`: both only report interfaces the OS
 * currently reports addresses for, deferring all eligibility
 * (private/loopback/link-local) filtering to the shared candidate logic.
 */

import { networkInterfaces } from 'node:os'
import type { RawInterface } from './networkCandidates'

export function systemRawInterfaces(): RawInterface[] {
  const out: RawInterface[] = []
  const ifaces = networkInterfaces()
  for (const [name, addrs] of Object.entries(ifaces)) {
    if (!addrs || addrs.length === 0) continue
    out.push({ name, addresses: addrs.map((addr) => addr.address) })
  }
  return out
}
