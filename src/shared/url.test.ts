import { describe, it, expect } from 'vitest'
import { isInsecureRemote } from './url'

describe('isInsecureRemote', () => {
  it('treats https as safe regardless of host', () => {
    expect(isInsecureRemote('https://muesli.example.com')).toBe(false)
    expect(isInsecureRemote('https://192.168.1.5')).toBe(false)
  })

  it('treats loopback http as safe (local dev)', () => {
    for (const u of [
      'http://localhost:8080',
      'http://127.0.0.1:8080',
      'http://127.5.5.5',
      'http://muesli.localhost',
      'http://[::1]:8080',
    ]) {
      expect(isInsecureRemote(u), u).toBe(false)
    }
  })

  it('flags plain http to any non-loopback host (LAN or internet)', () => {
    for (const u of [
      'http://192.168.1.5:8080',
      'http://10.0.0.4',
      'http://172.16.0.9',
      'http://muesli.example.com',
      'http://nas.local:8080',
    ]) {
      expect(isInsecureRemote(u), u).toBe(true)
    }
  })

  it('does not treat loopback-lookalike hostnames as loopback (no bypass)', () => {
    for (const u of [
      'http://127.0.0.1.evil.com',
      'http://localhost.evil.com',
      'http://127.0.0.1foo',
    ]) {
      expect(isInsecureRemote(u), u).toBe(true)
    }
  })

  it('returns false for an unparseable URL (let connect surface the real error)', () => {
    expect(isInsecureRemote('not a url')).toBe(false)
    expect(isInsecureRemote('')).toBe(false)
  })
})
