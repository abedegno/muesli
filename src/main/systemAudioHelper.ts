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
}

export function makeSystemAudioHelper(deps: SystemAudioHelperDeps = {}) {
  const platform = deps.platform ?? process.platform
  const binPath = deps.binPath ?? ''
  const spawn = deps.spawnImpl ?? nodeSpawn
  let child: ChildProcessWithoutNullStreams | null = null

  const available = () => platform === 'darwin' && binPath.length > 0

  async function start(onPcm: (chunk: Uint8Array) => void): Promise<SystemAudioFormat | null> {
    if (!available()) return null

    try {
      const proc = spawn(binPath, ['--start'], { stdio: ['ignore', 'pipe', 'pipe'] }) as unknown as ChildProcessWithoutNullStreams
      child = proc

      return await new Promise<SystemAudioFormat | null>((resolve) => {
        let header = ''
        let headerDone = false
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
            resolve(null)
            return
          }
          resolve({ sampleRate: Number(sr[1]), channels: Number(ch[1]) })
        }
        proc.stdout.on('data', onData)
        proc.once('exit', () => {
          if (!headerDone) resolve(null)
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
