import type { MuesliBridge } from '../shared/ipc'

// Typed accessor for the preload-exposed bridge. Centralises the `window` cast
// so components import a typed `muesli` rather than touching `window` directly.
declare global {
  interface Window {
    muesli: MuesliBridge
  }
}

export class BridgeError extends Error {
  constructor(
    message: string,
    public readonly kind: 'auth-invalidated' | 'ipc' = 'ipc',
  ) {
    super(message)
    this.name = 'BridgeError'
  }
}

const INVOKE_PREFIX = /^Error invoking remote method '[^']+':\s*/s
const AUTH_INVALIDATED_PREFIX = /^\[AUTH_INVALIDATED\]\s*/
const STATUS_PREFIX = /^\[(\d+)\]\s*(.*)$/s
const ERROR_NAME_PREFIX = /^[A-Za-z_$][\w$]*Error:\s*/

function normalizeBridgeError(err: unknown): BridgeError {
  if (err instanceof BridgeError) return err

  const raw =
    err instanceof Error
      ? err.message
      : typeof err === 'string'
        ? err
        : 'Something went wrong. Please try again.'
  const stripped = raw.replace(INVOKE_PREFIX, '').trim()
  const cleaned = stripped.replace(ERROR_NAME_PREFIX, '').trim()
  if (AUTH_INVALIDATED_PREFIX.test(cleaned)) {
    return new BridgeError(
      cleaned.replace(AUTH_INVALIDATED_PREFIX, '') || 'Your saved sign-in is no longer valid for this server. Sign in again to reconnect.',
      'auth-invalidated',
    )
  }

  const status = STATUS_PREFIX.exec(cleaned)
  if (status) {
    const [, code, message] = status
    if (code === '401') {
      return new BridgeError(
        'Your saved sign-in is no longer valid for this server. Sign in again to reconnect.',
        'auth-invalidated',
      )
    }
    return new BridgeError(message || 'Something went wrong. Please try again.')
  }

  return new BridgeError(cleaned || 'Something went wrong. Please try again.')
}

function isPromiseLike<T>(value: unknown): value is PromiseLike<T> {
  return !!value && typeof (value as PromiseLike<T>).then === 'function'
}

const rawBridge = window.muesli ?? ({} as MuesliBridge)

type AnyFn = (...args: unknown[]) => unknown

// Wrap one bridge call so both sync throws and promise rejections come back as
// a normalised `BridgeError` (see normalizeBridgeError).
function wrapCall(fn: AnyFn, thisArg: object): AnyFn {
  return (...args: unknown[]) => {
    try {
      const result = fn.apply(thisArg, args)
      return isPromiseLike(result)
        ? Promise.resolve(result).catch((err: unknown) => {
            throw normalizeBridgeError(err)
          })
        : result
    } catch (err) {
      throw normalizeBridgeError(err)
    }
  }
}

// Build a PLAIN object of wrapped members — deliberately NOT a `Proxy` over the
// bridge.
//
// `contextBridge.exposeInMainWorld` installs every member as a read-only,
// non-configurable data property. A proxy `get` trap that returns anything
// other than the target's own value for such a property violates a JavaScript
// proxy invariant, so the engine throws on the FIRST member access:
//
//   TypeError: 'get' on proxy: property 'onEmbeddedStartupStatus' is a read-only
//   and non-configurable data property on the proxy target but the proxy did not
//   return its actual value
//
// In the packaged app that fires while mounting the startup gate, before React
// renders anything — a blank window (shipped in desktop v0.1.10). Plain-object
// copies carry no such invariant. Copying eagerly is safe: the bridge is
// established once at preload and never gains members afterwards, which is the
// same shape the packaged-app smoke test exercises.
function buildBridge(raw: MuesliBridge): MuesliBridge {
  const source = raw as unknown as Record<string, unknown>
  const wrapped: Record<string, unknown> = {}
  for (const key of Object.keys(source)) {
    const value = source[key]
    wrapped[key] = typeof value === 'function' ? wrapCall(value as AnyFn, raw) : value
  }
  return wrapped as unknown as MuesliBridge
}

export const muesli: MuesliBridge = buildBridge(rawBridge)
