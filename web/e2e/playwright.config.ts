import { defineConfig } from '@playwright/test';

// The wuphf backend serves the built web UI directly at :7891. CI boots the
// backend externally (see .github/workflows/ci.yml:web-e2e) — we don't use
// playwright's webServer because the backend is a Go binary the CI already
// built. For local runs: build wuphf, launch it with --no-open, then
// `cd web/e2e && bunx playwright test`.
export default defineConfig({
  testDir: './tests',
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.WUPHF_E2E_BASE_URL ?? 'http://localhost:7891',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
});
