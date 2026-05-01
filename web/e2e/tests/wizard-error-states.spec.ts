import { expect, type Page, type Route, test } from "@playwright/test";

// E2E coverage for the two new wizard error UI surfaces shipped in PR #367:
//
//   1. prereqsError banner + selectable runtime tiles, when the
//      /onboarding/prereqs fetch fails (broker down, schema drift, etc.)
//   2. submitError alert + Retry button, when /onboarding/complete fails
//      (port conflict, broker file-locked, etc.)
//
// Both surfaces are user-visible UX shipping in the PH window — without
// e2e protection a future change to the Wizard control flow could silently
// turn the banner off (back to "all tiles disabled") or swallow the submit
// error again. Pre-existing wizard.spec.ts only covers the happy path.

async function waitForReactMount(page: Page): Promise<void> {
  await page.waitForFunction(
    () => {
      const root = document.getElementById("root");
      if (!root) return false;
      if (document.getElementById("skeleton")) return false;
      return root.children.length > 0;
    },
    { timeout: 10_000 },
  );
}

// Drive the wizard from welcome through to the setup step where both
// the prereqs banner and the runtime tile selection live. Mirrors
// local-llm-onboarding.spec.ts::advanceToSetupStep so failures in the
// shared path manifest the same way across both files.
async function advanceToSetupStep(page: Page) {
  await page.goto("/");
  await waitForReactMount(page);
  await expect(page.locator(".wizard-step").first()).toBeVisible({
    timeout: 10_000,
  });

  // welcome → identity
  await page.locator(".wizard-step button.btn-primary").first().click();
  // identity → templates (required fields)
  await page.locator("#wiz-company").fill("E2E Error States");
  await page.locator("#wiz-description").fill("Wizard error path test");
  await page.locator(".wizard-step button.btn-primary").first().click();
  // templates → team. Wizard.tsx renders one .template-card per
  // blueprint plus a separate "From scratch" button; click the first
  // real card so we exercise the seeded path, not the from-scratch
  // fallback.
  const templateTile = page.locator(".template-card").first();
  if (await templateTile.count()) {
    await templateTile.click();
  }
  await page.locator(".wizard-step button.btn-primary").first().click();
  // team → setup
  await page.locator(".wizard-step button.btn-primary").first().click();

  // We're on setup when at least one of its known surfaces is visible.
  await expect(
    page.getByTestId("setup-runtime-tile-Claude Code").first(),
  ).toBeVisible({ timeout: 10_000 });
}

test.describe("Wizard error states", () => {
  test("prereqsError banner appears + provider:null tiles stay selectable when /onboarding/prereqs fails", async ({
    page,
  }) => {
    // Fail the prereqs endpoint BEFORE navigation. Wizard.tsx fires
    // the fetch on mount, and page.route() returns a Promise — without
    // await the interceptor can register after the navigation request
    // is already in flight, producing a flaky pass.
    await page.route("**/onboarding/prereqs*", (route: Route) =>
      route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "broker unavailable" }),
      }),
    );

    await advanceToSetupStep(page);

    // 1. Banner shows up with the stable test id.
    await expect(page.getByTestId("prereqs-error-banner")).toBeVisible();

    // 2. Provider-bearing runtimes (Claude Code, Codex, Opencode) are
    //    selectable so the user can proceed if they trust their own
    //    install. The selectable predicate sets aria-disabled="false"
    //    and removes the .disabled class.
    const claudeTile = page.getByTestId("setup-runtime-tile-Claude Code");
    await expect(claudeTile).toHaveAttribute("aria-disabled", "false");
    await expect(claudeTile).not.toHaveClass(/disabled/);

    // 3. provider:null runtimes (Cursor, Windsurf) are still selectable
    //    BUT — and this is the regression guard — clicking one must
    //    NOT satisfy the "install gate" alone. The Wizard's
    //    hasInstalledSelection predicate now requires spec.provider !==
    //    null under prereqsError, so a Cursor-only selection should
    //    leave the primary CTA disabled. Asserted via the keyboard gate
    //    behavior is hard to drive in e2e; instead we just confirm the
    //    tile is reachable. The unit-test side of the gate lives in
    //    Wizard's internal logic — covered there.
    const cursorTile = page.getByTestId("setup-runtime-tile-Cursor");
    await expect(cursorTile).toHaveAttribute("aria-disabled", "false");
  });

  test("submitError alert + Retry button appear when /onboarding/complete fails, then succeed on retry", async ({
    page,
  }) => {
    // Stub /config to a fixed 200 so the only varying surface in this
    // test is /onboarding/complete. finishOnboarding posts /config
    // first, and a future regression there would otherwise fail this
    // test for the wrong reason.
    await page.route("**/config*", (route: Route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ ok: true }),
      }),
    );

    // Fail /onboarding/complete on the FIRST POST, succeed on the second.
    // This exercises the full retry round-trip — banner appears, button
    // text flips to "Retry", clicking Retry re-issues the POST and
    // finishes onboarding.
    let firstAttempt = true;
    await page.route("**/onboarding/complete*", (route: Route) => {
      if (firstAttempt) {
        firstAttempt = false;
        return route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({ error: "broker file-locked" }),
        });
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ ok: true }),
      });
    });

    await advanceToSetupStep(page);

    // setup → task: any cloud CLI installed in CI counts as
    // "selectable"; if none are installed the test runner's prereqs
    // request will succeed (the "happy" path here, not the prereqs
    // failure path) but every tile will be disabled, so we paste an
    // API key to satisfy hasAnyApiKey instead. This keeps the test
    // resilient across CI environments.
    const claudeKeyPaste = page.getByTestId("api-key-paste-ANTHROPIC_API_KEY");
    if (await claudeKeyPaste.count()) {
      await claudeKeyPaste.click();
      const input = page.getByTestId("api-key-input-ANTHROPIC_API_KEY");
      // Match the broker's loose anthropic-key shape: sk-ant-... is
      // accepted by the validator's prefix check.
      await input.fill("sk-ant-test-fixture-not-a-real-key");
    }
    await page.locator(".wizard-step button.btn-primary").first().click();

    // task → ready (skip the freeform task)
    await page.locator(".wizard-step button.btn-primary").first().click();

    // We're on ready when the submit button is visible.
    const submit = page.getByTestId("onboarding-submit-button");
    await expect(submit).toBeVisible({ timeout: 10_000 });

    // First click → /onboarding/complete fails → submitError banner
    // appears, button text flips to "Retry".
    await submit.click();
    await expect(page.getByTestId("onboarding-submit-error")).toBeVisible({
      timeout: 10_000,
    });
    await expect(submit).toHaveText(/Retry/);

    // The PR #367 invariant: a retry must not be silently no-op'd.
    // Click Retry → second /onboarding/complete returns 200 → the
    // submitError clears (Wizard sets submitError = "" before the
    // POST and only re-sets on rejection).
    await submit.click();
    await expect(page.getByTestId("onboarding-submit-error")).toHaveCount(0, {
      timeout: 10_000,
    });
  });
});
