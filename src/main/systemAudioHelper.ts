import { spawn as nodeSpawn } from 'node:child_process'
import type { ChildProcessWithoutNullStreams } from 'node:child_process'

export interface SystemAudioFormat {
  sampleRate: number
  channels: number
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

      const proc = spawn(binPath, ['--start'], { stdio: ['ignore', 'pipe', 'pipe'] }) as unknown as ChildProcessWithoutNullStreams
      child = proc

      return await new Promise<SystemAudioFormat | null>((resolve) => {
        let header = ''
        let headerDone = false
        let settled = false
        let startTimeout: ReturnType<typeof setTimeout> | null = null

        const settle = (value: SystemAudioFormat | null, clearActiveChild = false) => {
          if (settled) return
          settled = true
          if (startTimeout) clearTimeout(startTimeout)
          if (clearActiveChild && child === proc) child = null
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
          if (headerDone) {
            onPcm(new Uint8Array(d))
            return
          }

          const nl = d.indexOf(0x0a)
          if (nl === -1) {
            header += d.toString('utf8')
            return
          }

          header += d.subarray(0, nl).toString('utf8')
          headerDone = true
          const sr = /sr=(\d+)/.exec(header)
          const ch = /ch=(\d+)/.exec(header)
          const rest = d.subarray(nl + 1)
          if (rest.length > 0) onPcm(new Uint8Array(rest))
          if (!sr || !ch) {
            settle(null, true)
            return
          }
          settle({ sampleRate: Number(sr[1]), channels: Number(ch[1]) })
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
          if (child === proc) child = null
          if (!headerDone) settle(null, true)
        })
      })
    } catch {
      child = null
      return null
    }
  }

  async function stop(): Promise<void> {
    if (child && !child.killed) child.kill('SIGTERM')
    child = null
  }

  return { available, start, stop }
}
