import { expect, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForShellReady,
} from "./_helpers";

// Slash-command parity with the TUI YAML suite (tests/e2e/ac-02-slash-autocomplete.yaml,
// ac-05-help-command.yaml). The TUI side asserts: typing "/" opens an
// autocomplete overlay, Tab completes, /help renders help, /clear clears the
// transcript. The web composer ships the same surface (Composer.tsx +
// Autocomplete.tsx + HelpModal.tsx) but had zero coverage before this file.
//
// Subtle landmine that shaped this spec: when autocomplete items are visible,
// Composer.tsx:429-445 intercepts Enter to *pick the highlighted item*, not
// dispatch the slash command. So "/help\n" types Enter while acItems is
// populated and ends up rewriting the textarea to the picked command instead
// of opening HelpModal. Workaround: send a trailing space ("/help "), which
// makes `currentTrigger` return null (Autocomplete.tsx — slash regex requires
// `/^\/\S*$/`), the panel closes, and Enter falls through to the dispatcher.

test.afterEach(async ({ request }) => {
  await resetBroker(request);
});

test.describe("wuphf web slash commands", () => {
  test('typing "/" opens the autocomplete panel; Tab completes "/he" → "/help "', async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    const composer = page.locator(".composer-input");
    await composer.click();
    // pressSequentially (not fill) — autocomplete is keystroke-driven; bulk
    // fill skips the per-key trigger that opens the panel. type() works but
    // is deprecated as of Playwright 1.38.
    await composer.pressSequentially("/");

    const panel = page.locator(".autocomplete.open");
    await expect(panel).toBeVisible({ timeout: 5_000 });

    // useCommands falls back to FALLBACK_SLASH_COMMANDS when the broker
    // registry is unreachable, so we always expect ≥2 items. A panel with
    // one entry signals the fallback wiring broke.
    const items = panel.locator(".autocomplete-item");
    expect(await items.count()).toBeGreaterThan(1);

    await composer.fill("");
    await composer.pressSequentially("/he");

    // Wait for the panel AND for ≥1 item to render before pressing Tab.
    // toBeVisible alone is a paint check; Composer's keydown handler reads
    // `acItems`, which only flips truthy after the parent's effect commits.
    await expect(panel).toBeVisible({ timeout: 5_000 });
    await expect(items.first()).toBeVisible({ timeout: 5_000 });

    await composer.press("Tab");

    // Exact value catches the regression where applyAutocomplete returns
    // the raw query (`'/he '`) because the picker misfired — a substring or
    // length-only check would silently pass that bug.
    await expect(composer).toHaveValue("/help ", { timeout: 1_000 });

    await expectNoReactErrors(page, getErrors, "during autocomplete");
  });

  test('"/help" Enter opens the help dialog', async ({ page }) => {
    // TUI parity: ac-05 asserts /help renders help. Web side opens the
    // HelpModal via store.setComposerHelpOpen(true) (Composer.tsx:97-99).
    //
    // Trailing space defeats autocomplete (see file-level comment), so Enter
    // reaches handleSend → handleSlashCommand. handleSlashCommand splits on
    // /\s+/, so parts[0] is still "/help" — the dispatch path matches.
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    const composer = page.locator(".composer-input");
    await composer.click();
    await composer.fill("/help ");
    await composer.press("Enter");

    // Help modal: role="dialog", className="help-overlay" (HelpModal.tsx:132).
    const dialog = page.locator('.help-overlay[role="dialog"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    // Composer must clear after a consumed command (Composer.tsx — resetComposer).
    await expect(composer).toHaveValue("");

    // Close the dialog so it does not bleed into other tests in this file.
    await page.locator(".help-close").click();
    await expect(dialog).toBeHidden({ timeout: 5_000 });

    await expectNoReactErrors(page, getErrors, "during /help");
  });

  test('"/clear" Enter clears the composer and shows a confirmation toast', async ({
    page,
  }) => {
    // The web build does not actually wipe broker history; /clear just emits
    // a "Messages cleared" toast (Composer.tsx:94-96). Asserting the toast
    // is the closest behavioral analogue — and the failure mode we want to
    // catch (slash dispatch broken) is identical.
    //
    // SearchModal.tsx:446 emits the same toast string. Anchor the locator
    // on the `.animate-fade` toast container so a future SearchModal that
    // opens during shell init can't satisfy this assertion accidentally.
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    const composer = page.locator(".composer-input");
    await composer.click();
    await composer.fill("/clear ");
    await composer.press("Enter");

    await expect(composer).toHaveValue("", { timeout: 5_000 });

    // Toasts auto-dismiss at 4s (Toast.tsx). 3s assertion window is enough
    // for paint and short of the dismiss horizon. Filter on the container
    // class so the locator can't latch onto an in-feed message that happens
    // to contain the string.
    const toast = page
      .locator(".animate-fade")
      .filter({ hasText: "Messages cleared" })
      .first();
    await expect(toast).toBeVisible({ timeout: 3_000 });

    await expectNoReactErrors(page, getErrors, "during /clear");
  });
});
