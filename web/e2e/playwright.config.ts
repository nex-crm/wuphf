import { defineConfig } from "@playwright/test";

// The wuphf backend serves the built web UI directly at :7891. CI boots the
// backend externally (see .github/workflows/ci.yml:web-e2e) — we don't use
// playwright's webServer because the backend is a Go binary the CI already
// built. For local runs use `web/e2e/run-local.sh` (see web/e2e/README.md);
// it sandboxes WUPHF_RUNTIME_HOME, seeds onboarded.json for the shell phase,
// and runs both wizard and smoke specs against alternate ports so the
// developer's real ~/.wuphf state is never touched.
export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  // No retries. These are smoke tests for UI crashes — a flaky pass is
  // exactly the failure mode we're trying to prevent. Investigate flakes,
  // don't paper over them.
  retries: 0,
  workers: 1,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.WUPHF_E2E_BASE_URL ?? "http://localhost:7891",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
});
