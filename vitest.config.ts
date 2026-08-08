import { resolve } from 'node:path'
import { coverageConfigDefaults, defineConfig } from 'vitest/config'

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
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      thresholds: {
        branches: 84.19,
        functions: 77.26,
        lines: 79.98,
        statements: 79.98,
      },
      exclude: [
        ...coverageConfigDefaults.exclude,
        'out/**',
        'internal/adminui/dist/**',
        // The admin SPA owns its own Vitest config and test suite.
        'web/admin/**',
      ],
    },
  },
})
