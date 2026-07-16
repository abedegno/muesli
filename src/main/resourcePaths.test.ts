import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { resolveAudiotapBin, resolveResourceEnv, resolveServerBin } from './resourcePaths'

describe('resourcePaths', () => {
  it('resolves the packaged server binary path', () => {
    expect(resolveServerBin({
      isPackaged: true,
      resourcesPath: '/R',
      env: {},
    })).toBe(join('/R', 'server', 'muesli'))
  })

  it('resolves packaged resource env vars', () => {
    expect(resolveResourceEnv({
      isPackaged: true,
      resourcesPath: '/R',
    })).toEqual({
      MUESLI_MODE: 'embedded',
      MUESLI_EMBEDDED_PG_BINARIES: join('/R', 'pg'),
      MUESLI_EMBEDDED_PGVECTOR_DIR: join('/R', 'pgvector'),
      MUESLI_AUDIOTAP_BIN: join('/R', 'server', 'muesli-audiotap'),
      MUESLI_WHISPER_CPP_TRANSCRIBER_BIN: join('/R', 'server', 'whisper-cpp-transcriber'),
      MUESLI_WHISPER_MODEL_DIR: join('/R', 'models', 'whisper'),
      MUESLI_WHISPER_MODEL: 'ggml-tiny.en',
      MUESLI_FFMPEG_BIN: join('/R', 'bin', 'ffmpeg'),
    })
  })

  it('returns no resource env in development', () => {
    expect(resolveResourceEnv({
      isPackaged: false,
      resourcesPath: '/R',
    })).toEqual({})
  })

  it('resolves the development server binary from env', () => {
    expect(resolveServerBin({
      isPackaged: false,
      resourcesPath: '/R',
      env: {
        MUESLI_SERVER_BIN: '/tmp/muesli',
      },
    })).toBe('/tmp/muesli')
  })

  it('throws the development error when the server binary is unset', () => {
    expect(() => resolveServerBin({
      isPackaged: false,
      resourcesPath: '/R',
      env: {},
    })).toThrow('MUESLI_SERVER_BIN must point to the muesli server binary in development')
  })

  it('appends .exe to the packaged server binary on Windows', () => {
    expect(resolveServerBin({
      isPackaged: true,
      resourcesPath: '/R',
      env: {},
      platform: 'win32',
    })).toBe(join('/R', 'server', 'muesli.exe'))
  })

  it.each(['darwin', 'linux'] as const)('does not append an extension on %s', platform => {
    expect(resolveServerBin({
      isPackaged: true,
      resourcesPath: '/R',
      env: {},
      platform,
    })).toBe(join('/R', 'server', 'muesli'))
  })

  it('appends .exe to packaged resource env binaries on Windows', () => {
    expect(resolveResourceEnv({
      isPackaged: true,
      resourcesPath: '/R',
      platform: 'win32',
    })).toEqual({
      MUESLI_MODE: 'embedded',
      MUESLI_EMBEDDED_PG_BINARIES: join('/R', 'pg'),
      MUESLI_EMBEDDED_PGVECTOR_DIR: join('/R', 'pgvector'),
      MUESLI_AUDIOTAP_BIN: join('/R', 'server', 'muesli-audiotap.exe'),
      MUESLI_WHISPER_CPP_TRANSCRIBER_BIN: join('/R', 'server', 'whisper-cpp-transcriber.exe'),
      MUESLI_WHISPER_MODEL_DIR: join('/R', 'models', 'whisper'),
      MUESLI_WHISPER_MODEL: 'ggml-tiny.en',
      MUESLI_FFMPEG_BIN: join('/R', 'bin', 'ffmpeg.exe'),
    })
  })

  it.each([
    ['darwin', join('/R', 'server', 'muesli-audiotap')],
    ['linux', join('/R', 'server', 'muesli-audiotap')],
    ['win32', join('/R', 'server', 'muesli-audiotap.exe')],
  ] as const)('resolves the packaged audiotap binary on %s', (platform, expected) => {
    expect(resolveAudiotapBin({
      isPackaged: true,
      resourcesPath: '/R',
      env: {},
      platform,
    })).toBe(expected)
  })
})
