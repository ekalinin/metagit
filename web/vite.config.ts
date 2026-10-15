/// <reference types="vitest/config" />
import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  // Address of a running "metagit serve" for the dev server proxy.
  const apiURL = loadEnv(mode, '.', 'METAGIT_').METAGIT_API_URL || 'http://localhost:8080'

  return {
    plugins: [react()],
    build: {
      // The Go binary embeds this directory, see internal/webui.
      outDir: '../internal/webui/dist/app',
      emptyOutDir: true,
    },
    server: {
      proxy: {
        '/api': apiURL,
      },
    },
    test: {
      environment: 'jsdom',
      globals: true,
      setupFiles: ['./src/test-setup.ts'],
    },
  }
})
