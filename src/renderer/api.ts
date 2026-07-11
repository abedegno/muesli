import type { MuesliBridge } from '../shared/ipc'

// Typed accessor for the preload-exposed bridge. Centralises the `window` cast
// so components import a typed `muesli` rather than touching `window` directly.
declare global {
  interface Window {
    muesli: MuesliBridge
  }
}

export const muesli: MuesliBridge = window.muesli ?? ({} as MuesliBridge)
