import { resolve } from 'node:path'
import { defineConfig, externalizeDepsPlugin } from 'electron-vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Main and preload run in Electron's Node/CommonJS context, so we use the
// electron-vite preset (rollupOptions.input + externalizeDepsPlugin) and force
// CJS output. `build.lib.entry` alone can emit ESM that Electron's main process
// (and a non-ESM preload) cannot load — so we set formats: ['cjs'] explicitly.
export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    build: {
      rollupOptions: {
        input: { main: resolve(__dirname, 'src/main/main.ts') },
      },
      lib: { entry: resolve(__dirname, 'src/main/main.ts'), formats: ['cjs'] },
    },
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
    build: {
      rollupOptions: {
        input: { preload: resolve(__dirname, 'src/preload/preload.ts') },
      },
      lib: { entry: resolve(__dirname, 'src/preload/preload.ts'), formats: ['cjs'] },
    },
  },
  renderer: {
    root: resolve(__dirname, 'src/renderer'),
    resolve: {
      alias: { '@': resolve(__dirname, 'src/renderer') },
    },
    build: {
      rollupOptions: {
        input: resolve(__dirname, 'src/renderer/index.html'),
      },
    },
    plugins: [react(), tailwindcss()],
  },
})
