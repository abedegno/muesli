/**
 * Electron-side orchestration for local iOS access (issue #767 Task 6):
 * candidate enumeration/selection, dispatching enable/disable over the fd-3
 * control channel to the embedded Go process, and building the pairing
 * display (origin/fingerprint/phrase/QR payload) from its response.
 *
 * All fd-3 I/O goes through the injected ControlChannel interface so this
 * class is fully testable without a real child process.
 */

import { type NetworkFamily, type RawInterface, eligibleCandidates, evaluateSelection } from './networkCandidates'
import { buildPairingPayload, encodePairingPayload } from './pairing'

export interface ControlChannel {
  /** Writes one already-terminated-or-not line; the channel appends '\n'. */
  send(line: string): void
  onLine(handler: (line: string) => void): void
  onClose(handler: () => void): void
}

/** Maximum single control-protocol line, mirroring Go's maxControlLineBytes. */
export const MAX_CONTROL_LINE_BYTES = 64 * 1024

/**
 * ProcessControlChannel adapts a Node duplex stream (child.stdio[3]) to
 * ControlChannel: newline-delimited, capped-length framing in both
 * directions.
 */
export class ProcessControlChannel implements ControlChannel {
  private buffer = ''
  private lineHandlers: Array<(line: string) => void> = []
  private closeHandlers: Array<() => void> = []

  constructor(private readonly stream: NodeJS.ReadWriteStream) {
    stream.on('data', (chunk: Buffer | string) => {
      this.buffer += typeof chunk === 'string' ? chunk : chunk.toString('utf8')
      if (this.buffer.length > MAX_CONTROL_LINE_BYTES * 4) {
        // A peer that never sends a newline within several max-line budgets
        // is misbehaving; drop the buffer rather than growing unbounded.
        this.buffer = ''
        return
      }
      let idx: number
      while ((idx = this.buffer.indexOf('\n')) !== -1) {
        const line = this.buffer.slice(0, idx)
        this.buffer = this.buffer.slice(idx + 1)
        if (line.length > MAX_CONTROL_LINE_BYTES) continue
        for (const handler of this.lineHandlers) handler(line)
      }
    })
    stream.on('close', () => {
      for (const handler of this.closeHandlers) handler()
    })
    stream.on('end', () => {
      for (const handler of this.closeHandlers) handler()
    })
  }

  send(line: string): void {
    this.stream.write(line + '\n')
  }

  onLine(handler: (line: string) => void): void {
    this.lineHandlers.push(handler)
  }

  onClose(handler: () => void): void {
    this.closeHandlers.push(handler)
  }
}

interface ControlRequestPair {
  InterfaceName: string
  Address: string
  Family: NetworkFamily
}

interface ControlRequest {
  id: string
  action: 'enable' | 'disable'
  pair?: ControlRequestPair
}

interface ControlResponse {
  id: string
  ok: boolean
  error?: string
  port?: number
  origin?: string
  fingerprint_sha256?: string
  phrase?: string
}

export interface NetworkCandidateTriple {
  interfaceName: string
  address: string
  family: NetworkFamily
}

export interface EnableResult {
  origin: string
  port: number
  fingerprintHex: string
  phrase: string
  /** Compact JSON string for the QR code / manual-entry display. */
  qrPayload: string
}

const DEFAULT_REQUEST_TIMEOUT_MS = 10_000

interface PendingRequest {
  resolve: (resp: ControlResponse) => void
  reject: (err: Error) => void
  timer: ReturnType<typeof setTimeout>
}

/**
 * IOSAccessController owns exactly one enable/disable relationship with the
 * embedded process's control channel — state belongs to this one instance
 * (issue #767 / AGENTS.md: no package-level session state).
 */
export class IOSAccessController {
  private readonly channel: ControlChannel
  private readonly requestTimeoutMs: number
  private readonly listInterfacesImpl: () => RawInterface[]
  private pending = new Map<string, PendingRequest>()
  private nextRequestId = 0
  private closed = false
  private inFlightEnable: Promise<EnableResult> | null = null
  private inFlightDisable: Promise<void> | null = null

  constructor(
    channel: ControlChannel,
    opts: { requestTimeoutMs?: number; listInterfaces?: () => RawInterface[] } = {},
  ) {
    this.channel = channel
    this.requestTimeoutMs = opts.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS
    this.listInterfacesImpl = opts.listInterfaces ?? (() => [])
    this.channel.onLine((line) => this.handleLine(line))
    this.channel.onClose(() => this.handleClose())
  }

  /** Enumerates eligible candidates and classifies zero/one/many. */
  enumerate() {
    return evaluateSelection(eligibleCandidates(this.listInterfacesImpl()))
  }

  /**
   * Enables local iOS access on exactly one candidate pair. Concurrent
   * callers while an enable is already in flight are coalesced onto the
   * same request rather than issuing a duplicate.
   */
  async enable(pair: NetworkCandidateTriple): Promise<EnableResult> {
    if (this.inFlightEnable) return this.inFlightEnable
    const promise = this.sendRequest('enable', pair)
      .then((resp) => this.toEnableResult(resp))
      .finally(() => {
        this.inFlightEnable = null
      })
    this.inFlightEnable = promise
    return promise
  }

  /** Disables local iOS access, coalescing concurrent callers. */
  async disable(): Promise<void> {
    if (this.inFlightDisable) return this.inFlightDisable
    const promise = this.sendRequest('disable')
      .then(() => undefined)
      .finally(() => {
        this.inFlightDisable = null
      })
    this.inFlightDisable = promise
    return promise
  }

  private toEnableResult(resp: ControlResponse): EnableResult {
    const origin = resp.origin ?? ''
    const fingerprintHex = resp.fingerprint_sha256 ?? ''
    const phrase = resp.phrase ?? ''
    const payload = buildPairingPayload(origin, fingerprintHex)
    return {
      origin,
      port: resp.port ?? 0,
      fingerprintHex,
      phrase,
      qrPayload: encodePairingPayload({ ...payload, phrase }),
    }
  }

  private sendRequest(action: 'enable' | 'disable', pair?: NetworkCandidateTriple): Promise<ControlResponse> {
    if (this.closed) {
      return Promise.reject(new Error('ios access control channel is closed'))
    }
    const id = `req-${++this.nextRequestId}`
    const req: ControlRequest = {
      id,
      action,
      ...(pair
        ? { pair: { InterfaceName: pair.interfaceName, Address: pair.address, Family: pair.family } }
        : {}),
    }
    return new Promise<ControlResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(`ios access control request "${action}" timed out`))
      }, this.requestTimeoutMs)
      this.pending.set(id, { resolve, reject, timer })
      this.channel.send(JSON.stringify(req))
    })
  }

  private handleLine(line: string): void {
    let parsed: unknown
    try {
      parsed = JSON.parse(line)
    } catch {
      return
    }
    if (typeof parsed !== 'object' || parsed === null) return
    const resp = parsed as ControlResponse
    const pending = resp.id ? this.pending.get(resp.id) : undefined
    if (!pending) return
    clearTimeout(pending.timer)
    this.pending.delete(resp.id)
    if (resp.ok) {
      pending.resolve(resp)
    } else {
      pending.reject(new Error(resp.error || 'ios access control request failed'))
    }
  }

  private handleClose(): void {
    this.closed = true
    for (const [, pending] of this.pending) {
      clearTimeout(pending.timer)
      pending.reject(new Error('ios access control channel closed'))
    }
    this.pending.clear()
  }
}
