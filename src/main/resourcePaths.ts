import { join } from 'node:path'

export function exeSuffix(platform: NodeJS.Platform): '.exe' | '' {
  return platform === 'win32' ? '.exe' : ''
}

export function resolveServerBin(opts: {
  isPackaged: boolean
  resourcesPath: string
  env: NodeJS.ProcessEnv
  platform?: NodeJS.Platform
}): string {
  const platform = opts.platform ?? process.platform

  if (opts.isPackaged) {
    return join(opts.resourcesPath, 'server', `muesli${exeSuffix(platform)}`)
  }

  const configured = opts.env.MUESLI_SERVER_BIN?.trim()
  if (configured) return configured

  throw new Error('MUESLI_SERVER_BIN must point to the muesli server binary in development')
}

export function resolveResourceEnv(opts: { isPackaged: boolean; resourcesPath: string; platform?: NodeJS.Platform }): Record<string, string> {
  const platform = opts.platform ?? process.platform

  if (!opts.isPackaged) return {}

  return {
    // Signal embedded mode to the Go server via env: config.Load reads
    // MUESLI_MODE (not the --embedded arg), and only then skips the external
    // DATABASE_URL requirement and provisions the managed Postgres itself.
    MUESLI_MODE: 'embedded',
    MUESLI_EMBEDDED_PG_BINARIES: join(opts.resourcesPath, 'pg'),
    MUESLI_EMBEDDED_PGVECTOR_DIR: join(opts.resourcesPath, 'pgvector'),
    MUESLI_WHISPER_CPP_TRANSCRIBER_BIN: join(opts.resourcesPath, 'server', `whisper-cpp-transcriber${exeSuffix(platform)}`),
    MUESLI_WHISPER_MODEL_DIR: join(opts.resourcesPath, 'models', 'whisper'),
    MUESLI_WHISPER_MODEL: 'ggml-tiny.en',
    MUESLI_FFMPEG_BIN: join(opts.resourcesPath, 'bin', `ffmpeg${exeSuffix(platform)}`),
  }
}
