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

export const muesli: MuesliBridge = new Proxy(rawBridge, {
  get(target, prop, receiver) {
    const value = Reflect.get(target, prop, receiver)
    if (typeof value !== 'function') return value
    return (...args: unknown[]) => {
      try {
        const result = value.apply(target, args)
        return isPromiseLike(result)
          ? Promise.resolve(result).catch((err: unknown) => {
              throw normalizeBridgeError(err)
            })
          : result
      } catch (err) {
        throw normalizeBridgeError(err)
      }
    }
  },
}) as MuesliBridge
