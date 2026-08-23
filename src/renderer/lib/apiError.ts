/**
 * HTTP status of a failed bridge call, or null when the failure never reached
 * the server.
 *
 * `BridgeError` (src/renderer/api.ts) carries `status`, recovered from the
 * `[NNN] ` prefix `ipcHandlers` encodes into the message — Electron's
 * `ipcMain.handle` rejection path round-trips only `message`. Reading the
 * property structurally rather than importing the class keeps this usable from
 * components whose tests mock `@/api` wholesale.
 */
export function errorStatus(err: unknown): number | null {
  const status = (err as { status?: unknown } | null | undefined)?.status
  return typeof status === 'number' ? status : null
}

/** A conflict: the transcript changed under the caller and it must refetch. */
export function isConflictError(err: unknown): boolean {
  return errorStatus(err) === 409
}
