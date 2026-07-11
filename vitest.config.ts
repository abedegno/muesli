import { resolve } from 'node:path'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  resolve: {
    // Mirror the renderer `@/` alias so UI primitives resolve under vitest.
    alias: { '@': resolve(__dirname, 'src/renderer') },
  },
  test: {
    globals: true,
    // Default to node; DOM-touching test files opt in with a `// @vitest-environment jsdom` pragma.
    environment: 'node',
    setupFiles: ['./vitest.setup.ts'],
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'test/**/*.test.ts'],
  },
})
