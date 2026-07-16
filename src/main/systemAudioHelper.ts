import { spawn as nodeSpawn } from 'node:child_process'
import type { ChildProcessWithoutNullStreams } from 'node:child_process'

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

  async function start(): Promise<{ deviceId: string } | null> {
    if (!available()) return null

    try {
      const proc = spawn(binPath, ['--start'], { stdio: ['ignore', 'pipe', 'pipe'] }) as unknown as ChildProcessWithoutNullStreams
      child = proc

      const deviceId = await new Promise<string | null>((resolve) => {
        let buf = ''
        const onData = (d: Buffer) => {
          buf += d.toString('utf8')
          const line = buf.split('\n').find((l) => l.trim().length > 0)
          if (line) {
            proc.stdout.off('data', onData)
            resolve(line.trim())
          }
        }
        proc.stdout.on('data', onData)
        proc.once('exit', () => resolve(null))
      })

      return deviceId ? { deviceId } : null
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
