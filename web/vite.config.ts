/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7891',
        changeOrigin: true,
      },
      '/api-token': {
        target: 'http://127.0.0.1:7891',
        changeOrigin: true,
      },
      '/onboarding': {
        target: 'http://127.0.0.1:7891',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'happy-dom',
    globals: true,
    setupFiles: ['./tests/setup.ts'],
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      include: ['src/components/wiki/**', 'src/lib/wikilink.ts', 'src/api/wiki.ts'],
      thresholds: { lines: 80, branches: 80, functions: 80 },
    },
  },
})
