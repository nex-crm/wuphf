/// <reference types="vitest" />

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5273,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:7891",
        changeOrigin: true,
      },
      "/api-token": {
        target: "http://127.0.0.1:7891",
        changeOrigin: true,
      },
      "/onboarding": {
        target: "http://127.0.0.1:7891",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  test: {
    environment: "happy-dom",
    globals: true,
    setupFiles: ["./tests/setup.ts"],
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
    coverage: {
      provider: "v8",
      include: [
        "src/components/wiki/**",
        "src/lib/wikilink.ts",
        "src/api/wiki.ts",
      ],
      // Current scoped wiki baseline. Ratchet these upward as coverage improves
      // instead of letting the CI gate start red.
      thresholds: { statements: 70, lines: 73, branches: 64, functions: 71 },
    },
  },
});
