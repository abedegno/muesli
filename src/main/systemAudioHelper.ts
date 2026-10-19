import { randomUUID } from 'node:crypto'
import { spawn as nodeSpawn } from 'node:child_process'
import type { ChildProcessWithoutNullStreams } from 'node:child_process'

export interface SystemAudioFormat {
  sampleRate: number
  channels: number
  captureId?: string
}

export interface SystemAudioHelperDeps {
  platform?: NodeJS.Platform
  binPath?: string
  spawnImpl?: typeof nodeSpawn
  startTimeoutMs?: number
}

export function makeSystemAudioHelper(deps: SystemAudioHelperDeps = {}) {
  const platform = deps.platform ?? process.platform
  const binPath = deps.binPath ?? ''
  const spawn = deps.spawnImpl ?? nodeSpawn
  const startTimeoutMs = deps.startTimeoutMs ?? 5000
  let child: ChildProcessWithoutNullStreams | null = null
  let activeCaptureId: string | null = null

  const available = () => platform === 'darwin' && binPath.length > 0

  async function start(onPcm: (chunk: Uint8Array) => void): Promise<SystemAudioFormat | null> {
    if (!available()) return null

    try {
      if (child && !child.killed) {
        try {
          child.kill('SIGTERM')
        } catch {
          // Ignore errors while replacing a previous helper process.
        }
      }
      child = null
      activeCaptureId = null

      const proc = spawn(binPath, ['--start'], {
        stdio: ['ignore', 'pipe', 'pipe'],
      }) as unknown as ChildProcessWithoutNullStreams
      child = proc
      const captureId = randomUUID()
      activeCaptureId = captureId

      return await new Promise<SystemAudioFormat | null>((resolve) => {
        let header = ''
        let headerDone = false
        let settled = false
        let startTimeout: ReturnType<typeof setTimeout> | null = null

        const settle = (value: SystemAudioFormat | null, clearActiveChild = false) => {
          if (settled) return
          settled = true
          if (startTimeout) clearTimeout(startTimeout)
          if (clearActiveChild && child === proc) {
            child = null
            activeCaptureId = null
          }
          resolve(value)
        }

        startTimeout = setTimeout(() => {
          if (settled) return
          try {
            proc.kill('SIGTERM')
          } catch {
            // Ignore errors during timeout cleanup.
          }
          settle(null, true)
        }, startTimeoutMs)

        const onData = (d: Buffer) => {
          if (child !== proc) return
          if (headerDone) {
            onPcm(new Uint8Array(d))
            return
          }

          const nl = d.indexOf(0x0a)
          if (nl === -1) {
            header += d.toString('utf8')
            if (header.length > 4096) {
              proc.kill('SIGTERM')
              settle(null, true)
            }
            return
          }

          header += d.subarray(0, nl).toString('utf8')
          headerDone = true
          const sr = /sr=(\d+)/.exec(header)
          const ch = /ch=(\d+)/.exec(header)
          const rest = d.subarray(nl + 1)
          if (
            header.length > 4096 ||
            !sr ||
            !ch ||
            Number(sr[1]) < 8000 ||
            Number(sr[1]) > 192000 ||
            Number(ch[1]) < 1 ||
            Number(ch[1]) > 8
          ) {
            proc.kill('SIGTERM')
            settle(null, true)
            return
          }
          if (rest.length > 0) onPcm(new Uint8Array(rest))
          settle({ sampleRate: Number(sr[1]), channels: Number(ch[1]), captureId })
        }
        const onError = () => {
          try {
            if (!proc.killed) proc.kill('SIGTERM')
          } catch {
            // Ignore errors while cleaning up a failed helper start.
          }
          settle(null, true)
        }
        proc.stdout.on('data', onData)
        proc.stderr.on('data', () => {})
        proc.once('error', onError)
        proc.once('exit', () => {
          if (child === proc) {
            child = null
            activeCaptureId = null
          }
          if (!headerDone) settle(null, true)
        })
      })
    } catch {
      child = null
      activeCaptureId = null
      return null
    }
  }

  async function stop(captureId?: string): Promise<void> {
    if (captureId && captureId !== activeCaptureId) return
    if (child && !child.killed) child.kill('SIGTERM')
    child = null
    activeCaptureId = null
  }

  return { available, start, stop }
}
