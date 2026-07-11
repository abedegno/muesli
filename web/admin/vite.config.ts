import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

// The SPA is served under /admin/ by the Go server, so asset URLs must be
// prefixed with /admin/. Build output goes straight into the Go embed dir.
export default defineConfig({
  base: '/admin/',
  plugins: [react()],
  build: {
    outDir: fileURLToPath(new URL('../../internal/adminui/dist', import.meta.url)),
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Use stable (non-hashed) filenames so the embed test can reference
        // assets/app.js by a fixed path.
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name].[ext]',
      },
    },
  },
})
