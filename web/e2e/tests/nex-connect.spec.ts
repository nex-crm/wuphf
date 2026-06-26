import { expect, type Page, type Route, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

// Settings → Integrations → "Connect Nex" e2e (shell phase). Stubs
// POST /nex/register with route.fulfill so the panel's behaviour is
// asserted without a live nex-cli — the real CLI shell-out is covered
// by internal/nex/cli_test.go and internal/team/broker_nex_register_test.go.
//
// Regression guard: a user with only the @nex-ai/nex npm shim on PATH
// (no backing binary) used to see the broker's raw JSON error blob in
// this panel. It must now degrade to the install-instructions fallback —
// a copy-paste install command plus a register-at-nex.ai escape hatch.

// stubNexRegister intercepts POST /nex/register and replies with `body`
// at `status`, mimicking the broker's handleNexRegister response.
function stubNexRegister(page: Page, status: number, body: unknown) {
  return page.route("**/nex/register", (route: Route) =>
    route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(body),
    }),
  );
}

// goToIntegrations lands on the shell and opens Settings → Integrations,
// where NexConnectPanel renders. The Settings nav button is matched by
// data-testid so it's not confused with the same-named Integrations app
// entry in the parent sidebar.
async function goToIntegrations(page: Page) {
  await page.goto("/");
  await waitForReactMount(page);
  await page.getByLabel("Open settings").click();
  await page.getByTestId("settings-nav-integrations").click();
  await expect(page.getByTestId("nex-connect-panel")).toBeVisible();
}

async function submitEmail(page: Page, email: string) {
  await page.getByLabel("Email address for Nex registration").fill(email);
  await page.getByRole("button", { name: "Connect Nex" }).click();
}

test.describe("Settings → Integrations → Connect Nex", () => {
  test("offers the install command and retry when nex-cli is not installed", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);
    // What the broker returns once a shim-without-binary is mapped to
    // ErrNotInstalled (see broker_nex_register_test.go).
    await stubNexRegister(page, 502, {
      status: "error",
      error: "nex-cli not installed",
    });
    await goToIntegrations(page);
    await submitEmail(page, "founder@example.com");

    const panel = page.getByTestId("nex-connect-panel");
    // Copy-paste install command surfaced (wuphf never runs it for you).
    await expect(panel).toContainText("install.sh | sh");
    // No-terminal escape hatch still offered.
    const fallback = page.getByRole("link", { name: /nex\.ai\/register/i });
    await expect(fallback).toHaveAttribute("href", "https://nex.ai/register");
    // Retry path returns to the email form without a reload.
    await page.getByRole("button", { name: /try again/i }).click();
    await expect(
      page.getByLabel("Email address for Nex registration"),
    ).toBeVisible();
    await expectNoReactErrors(page, getErrors, "after not-installed fallback");
  });

  test("does not leak the npm shim's raw error blob into the panel", async ({
    page,
  }) => {
    // The exact failure shape from the field report: the @nex-ai/nex shim
    // ran, found no binary, and the broker forwarded its stderr verbatim.
    await stubNexRegister(page, 502, {
      status: "error",
      error:
        "nex-cli setup hi@mustafa.li: nex-cli binary not found. Install it with: curl -fsSL https://...install.sh | sh",
    });
    await goToIntegrations(page);
    await submitEmail(page, "hi@mustafa.li");

    // Install instructions shown; the raw JSON / stderr blob does not leak.
    const panel = page.getByTestId("nex-connect-panel");
    await expect(panel).toContainText("install.sh | sh");
    await expect(panel).not.toContainText('"status":"error"');
    await expect(panel).not.toContainText("hi@mustafa.li");
    await expect(panel).not.toContainText("binary not found");
  });

  test("confirms success when registration succeeds", async ({ page }) => {
    await stubNexRegister(page, 200, {
      status: "ok",
      email: "founder@example.com",
    });
    await goToIntegrations(page);
    await submitEmail(page, "founder@example.com");

    await expect(page.getByTestId("nex-connect-panel")).toContainText(
      /check your inbox at founder@example.com/i,
    );
  });
});
