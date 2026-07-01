/// <reference types="vitest" />

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// The broker web server the dev proxy forwards /api to. Defaults to the standard
// :7891, but is overridable so a worktree can run its own broker on dedicated
// ports and never collide with another worktree's broker on the default ports.
// Set WUPHF_WEB_PROXY_TARGET (e.g. http://127.0.0.1:7893) or just
// WUPHF_WEB_PROXY_PORT (e.g. 7893).
const proxyTarget =
  process.env.WUPHF_WEB_PROXY_TARGET ||
  `http://127.0.0.1:${process.env.WUPHF_WEB_PROXY_PORT || "7891"}`;

const proxyEntry = { target: proxyTarget, changeOrigin: true };

// The Python harness (the operator chat agent). Its /tools/build endpoint authors
// tools the app's chat calls. Overridable so a worktree can point at its own
// harness; defaults to the harness dev port. The /harness prefix is stripped so
// the harness sees /tools/build.
const harnessTarget =
  process.env.WUPHF_HARNESS_TARGET ||
  `http://127.0.0.1:${process.env.WUPHF_HARNESS_PORT || "8810"}`;
const harnessEntry = {
  target: harnessTarget,
  changeOrigin: true,
  rewrite: (p: string) => p.replace(/^\/harness/, ""),
};

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
    dedupe: ["canonicalize"],
  },
  server: {
    port: 5273,
    strictPort: true,
    proxy: {
      "/api": proxyEntry,
      "/api-token": proxyEntry,
      "/onboarding": proxyEntry,
      "/harness": harnessEntry,
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
    // Hard timeouts so a runaway test/teardown fails the suite instead of
    // hanging the CI worker for 15+ minutes. We've hit this via SSE/timer
    // handles that outlive a test.
    testTimeout: 10_000,
    hookTimeout: 10_000,
    teardownTimeout: 10_000,
    coverage: {
      provider: "v8",
      include: [
        "src/components/wiki/**",
        "src/lib/wikilink.ts",
        "src/api/wiki.ts",
      ],
      // The refclone editor is a large embedded editor surface with its own
      // coverage ramp; keep the legacy wiki gate scoped to the current baseline.
      exclude: ["src/components/wiki/editor/refclone/**"],
      // Current scoped wiki baseline. Ratchet these upward as coverage improves
      // instead of letting the CI gate start red.
      thresholds: { statements: 70, lines: 73, branches: 64, functions: 71 },
    },
  },
});
