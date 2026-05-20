import { defineConfig } from "@playwright/test";

// End-to-end harness for `apps/desktop`. Boots the real Electron main
// process — which in turn forks the broker utility process — and drives
// the renderer via Playwright's Chromium runtime. The purpose is to
// catch "tests pass but the app does not boot" regressions that unit
// tests cannot see: the broker resolves `better-sqlite3` from
// `out/main/`, reads its `.sql` migrations, the renderer probes
// `/api-token` and `/api/health`, and the DOM mounts.
//
// We do not run these in CI by default — they require the packaged
// `out/` directory and an Electron binary download. Run locally with
// `bun --cwd apps/desktop run test:e2e` after `bun --cwd apps/desktop
// run build`.
export default defineConfig({
  testDir: "./tests",
  // Electron cold-boots (broker fork, SQLite open, vite preview build)
  // sit well under 10s on darwin-arm64; 30s is a generous ceiling that
  // still catches hangs.
  timeout: 30_000,
  expect: { timeout: 10_000 },
  // Electron tests share `localhost:5173` (vite dev) and the user-data
  // directory; serializing keeps them deterministic.
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
});
