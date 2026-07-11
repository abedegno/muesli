// Sentinel thrown by the connect handler when a plain-HTTP connection to a
// non-loopback server is blocked. The renderer matches on it to show the
// "connect anyway?" guardrail instead of a raw error.
export const INSECURE_CONNECTION_CODE = 'ERR_INSECURE_CONNECTION'

// isInsecureRemote reports whether connecting to serverUrl would send traffic
// unencrypted to a non-loopback host — i.e. plain `http:` to anything but
// localhost. `https:` is safe; loopback `http:` is safe (local dev). An
// unparseable URL returns false so the connect attempt surfaces the real error.
export function isInsecureRemote(serverUrl: string): boolean {
  let u: URL
  try {
    u = new URL(serverUrl)
  } catch {
    return false
  }
  if (u.protocol !== 'http:') return false // https: (or anything non-http) is fine
  return !isLoopbackHost(u.hostname)
}

// isLoopbackHost matches only true loopback addresses — NOT RFC-1918 LAN ranges,
// which (per the privacy decision) are blocked-with-override like any remote host.
function isLoopbackHost(host: string): boolean {
  const h = host.toLowerCase().replace(/^\[/, '').replace(/\]$/, '') // strip IPv6 brackets
  if (h === 'localhost' || h.endsWith('.localhost')) return true
  // 127.0.0.0/8 — match a real dotted-quad only, so a hostname like
  // "127.0.0.1.evil.com" (which starts with "127.") is NOT treated as loopback.
  if (/^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(h)) return true
  if (h === '::1' || h === '0:0:0:0:0:0:0:1') return true
  return false
}
