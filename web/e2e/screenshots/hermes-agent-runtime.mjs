// Capture the onboarding runtime picker with Hermes Agent selected.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh hermes-agent-runtime <pr-number>

import process from "node:process";

import { launchBrowser, shotPage } from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1400, height: 940 },
});

await context.route("**/api-token", (r) =>
  r.fulfill({
    contentType: "application/json",
    body: JSON.stringify({ token: "stub", broker_url: null }),
  }),
);
await context.route("**/web-token", (r) =>
  r.fulfill({
    contentType: "application/json",
    body: JSON.stringify({ token: "stub" }),
  }),
);
await context.route("**/api/onboarding/state", (r) =>
  r.fulfill({
    contentType: "application/json",
    body: JSON.stringify({ onboarded: false }),
  }),
);
await context.route("**/api/onboarding/blueprints", (r) =>
  r.fulfill({
    contentType: "application/json",
    body: JSON.stringify({ templates: [] }),
  }),
);
await context.route("**/api/onboarding/prereqs", (r) =>
  r.fulfill({
    contentType: "application/json",
    body: JSON.stringify({
      prereqs: [
        { name: "claude", found: true, version: "Claude Code 2.1.138" },
        { name: "codex", found: true, version: "codex-cli 0.130.0" },
        { name: "opencode", found: true, version: "1.14.21" },
      ],
    }),
  }),
);
await context.route("**/api/config", (r) =>
  r.fulfill({
    contentType: "application/json",
    body: JSON.stringify({
      llm_provider: "hermes-agent",
      llm_provider_configured: true,
      llm_provider_priority: null,
      memory_backend: "markdown",
    }),
  }),
);
await context.route("**/api/status/local-providers", (r) =>
  r.fulfill({
    contentType: "application/json",
    body: JSON.stringify([
      {
        kind: "mlx-lm",
        binary_installed: false,
        endpoint: "http://127.0.0.1:8080/v1",
        model: "mlx-community/Qwen3-Coder-30B-A3B-Instruct-4bit",
        reachable: false,
        probed: true,
        platform_supported: true,
      },
      {
        kind: "ollama",
        binary_installed: false,
        endpoint: "http://127.0.0.1:11434/v1",
        model: "qwen3-coder",
        reachable: false,
        probed: true,
        platform_supported: true,
      },
      {
        kind: "exo",
        binary_installed: false,
        endpoint: "http://127.0.0.1:52415/v1",
        model: "llama-3.2-3b",
        reachable: false,
        probed: true,
        platform_supported: true,
      },
      {
        kind: "hermes-agent",
        binary_installed: true,
        binary_path: "/usr/local/bin/hermes",
        binary_version: "hermes 0.1.0",
        endpoint: "http://127.0.0.1:8642/v1",
        model: "hermes-agent",
        reachable: true,
        loaded_model: "hermes-agent",
        probed: true,
        platform_supported: true,
      },
    ]),
  }),
);

await page.goto(`${process.env.BASE_URL ?? "http://localhost:5273"}/`, {
  waitUntil: "load",
});
await page.getByRole("button", { name: "Continue" }).click();
await page.getByRole("textbox", { name: "Office name *" }).fill("Hermes Dev Office");
await page
  .getByRole("textbox", { name: "Short description *" })
  .fill("Testing Hermes Agent provider support in dev");
await page
  .getByRole("textbox", { name: "Top priority right now" })
  .fill("Verify Hermes Agent appears in runtime selection");
await page.getByRole("button", { name: "Continue" }).click();
await page.getByRole("button", { name: "Review the team" }).click();
await page.getByRole("button", { name: "Continue" }).click();

await page.getByText("How should agents run?").waitFor({ timeout: 10_000 });
await page
  .getByTestId("onboarding-local-llm-tile-hermes-agent")
  .waitFor({ timeout: 10_000 });

await shotPage(page, OUT, "01-hermes-agent-runtime-picker");

console.log(`captured Hermes Agent runtime screenshot to ${OUT}`);
await browser.close();
