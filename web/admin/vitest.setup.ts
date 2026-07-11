import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// Unmount React trees after every test so renders don't accumulate in the
// shared jsdom document (Testing Library does not auto-clean under Vitest).
afterEach(() => {
  cleanup()
})
