import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForShellReady,
} from "./_helpers";

// Drives the web /connect Telegram wizard end to end. The wizard talks to
// three new broker endpoints (POST /telegram/{verify,discover,connect}) that
// proxy the Telegram Bot API. We stub them with page.route() so the suite
// never touches t.me/api.telegram.org and never needs a live BotFather token.
//
// Checkpoints (some may be marked .fixme until the wiring lands):
//
//   1. /connect command runs and opens the Telegram modal.
//   2. The modal renders the token-entry step with a visible token input.
//   3. Submitting empty token shows an inline validation error.
//   4. Verify failure (broker says ok:false) keeps the user on the token step
//      with an editable input and an inline error banner.
//   5. Verify success advances to mode selection, then group mode discovers
//      chats and renders the picker step.
//   6. Empty group list shows "no groups" copy + retry/manual/dm controls.
//   7. Picking a group fires POST /telegram/connect with the right body and
//      lands on the done step showing the new channel slug.
//   8. Manual chat ID flow validates the input and posts an integer chat_id.
//   9. Choosing DM mode posts chat_id=0.
//  10. Connect failure keeps the picker step active with the error visible.
//  10b. Modal renders with a visible card surface (regression: wk-* CSS
//       variables must resolve outside `.wiki-root`).
//  10c. Modal renders styled when opened from a non-wiki route (regression).
//  11. Stale error from a failed verify clears on a successful retry.
//
// All steps assert no React errors so a render-time crash mid-wizard fails
// the test rather than degrading silently.

// Every test gets the React-error guard. Without this, error-state branches
// (verify-failed, connect-failed, manual-id-validation) would silently render
// undefined-deref bugs and the suite would still go green.
let getErrors: () => string[];

test.beforeEach(async ({ page }) => {
  getErrors = collectReactErrors(page);
});

test.afterEach(async ({ page, request }, info) => {
  await expectNoReactErrors(page, getErrors, `during ${info.title}`);
  await resetBroker(request);
});

const verifyOK = {
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ ok: true, bot_name: "wuphf-test-bot" }),
};

const verifyFail = {
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ ok: false, error: "401 Unauthorized" }),
};

const discoverEmpty = {
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ groups: [] }),
};

const discoverGroups = {
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({
    groups: [
      { chat_id: -123456, title: "Test Group A", type: "group" },
      { chat_id: -789012, title: "Test Group B", type: "supergroup" },
    ],
  }),
};

const connectOK = {
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({
    channel_slug: "tg-test-group-a",
    group_title: "Test Group A",
  }),
};

const connectFail = {
  status: 500,
  contentType: "text/plain",
  body: "could not verify chat: chat not found",
};

// Open the Telegram wizard directly via `/connect telegram` so the tests
// don't have to click through the provider picker first. The bare-`/connect`
// path that lands on the provider picker has its own dedicated test below.
async function openTelegramWizard(page: Page) {
  const composer = page.locator(".composer-input");
  await composer.click();
  // Trailing space defeats the autocomplete picker so Enter dispatches the
  // command instead of inserting the highlighted item — same trick the
  // slash-commands spec uses for /help.
  await composer.fill("/connect telegram ");
  await composer.press("Enter");
  await expect(page.getByTestId("tg-connect-modal")).toBeVisible({
    timeout: 5_000,
  });
  await expect(page.getByTestId("tg-step-token")).toBeVisible();
}

async function verifyTokenToMode(page: Page, token = "123456:ABC") {
  await page.getByTestId("tg-token-input").fill(token);
  await page.getByTestId("tg-token-submit").click();
  await expect(page.getByTestId("tg-step-mode")).toBeVisible({
    timeout: 5_000,
  });
}

async function verifyTokenAndOpenGroupPicker(page: Page, token = "123456:ABC") {
  await verifyTokenToMode(page, token);
  await page.getByTestId("tg-mode-group").click();
  await expect(page.getByTestId("tg-step-pick")).toBeVisible({
    timeout: 5_000,
  });
}

test.describe("wuphf web /connect Telegram wizard", () => {
  test("[1] /connect telegram opens the Telegram wizard at the token step", async ({
    page,
  }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await openTelegramWizard(page);
    await expect(page.getByTestId("tg-step-token")).toBeVisible();
  });

  test("[1a] bare /connect opens the provider picker with TUI parity", async ({
    page,
  }) => {
    await page.goto("/");
    await waitForShellReady(page);

    const composer = page.locator(".composer-input");
    await composer.click();
    await composer.fill("/connect ");
    await composer.press("Enter");

    await expect(page.getByTestId("tg-connect-modal")).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByTestId("tg-step-provider")).toBeVisible();
    // All four options the TUI offers (cmd/wuphf/channel.go:4871-4880)
    // are surfaced — no silent shortcut to Telegram.
    await expect(page.getByTestId("tg-provider-telegram")).toBeVisible();
    await expect(page.getByTestId("tg-provider-openclaw")).toBeVisible();
    await expect(page.getByTestId("tg-provider-slack")).toBeVisible();
    await expect(page.getByTestId("tg-provider-discord")).toBeVisible();
    // Slack/Discord are placeholders today — confirm they're disabled so
    // the user can see what's coming without a confusing "click does nothing".
    await expect(page.getByTestId("tg-provider-slack")).toBeDisabled();
    await expect(page.getByTestId("tg-provider-discord")).toBeDisabled();
  });

  test("[1b] picker → Telegram advances to the token step", async ({
    page,
  }) => {
    await page.goto("/");
    await waitForShellReady(page);

    const composer = page.locator(".composer-input");
    await composer.click();
    await composer.fill("/connect ");
    await composer.press("Enter");

    await page.getByTestId("tg-provider-telegram").click();
    await expect(page.getByTestId("tg-step-token")).toBeVisible();
  });

  test("[1c] picker → OpenClaw closes the modal and tells the user to use the TUI", async ({
    page,
  }) => {
    // OpenClaw works in the TUI today but doesn't have a web wizard yet.
    // Surfacing that gap honestly is the whole point of this picker — the
    // previous behaviour silently routed every /connect to Telegram, which
    // was the bug bornaware reported. This test guards against regressing
    // back to that.
    await page.goto("/");
    await waitForShellReady(page);

    const composer = page.locator(".composer-input");
    await composer.click();
    await composer.fill("/connect ");
    await composer.press("Enter");

    await page.getByTestId("tg-provider-openclaw").click();
    await expect(page.getByTestId("tg-connect-modal")).toBeHidden({
      timeout: 5_000,
    });
  });

  test("[2,3] empty token shows inline validation", async ({ page }) => {
    await page.goto("/");
    await waitForShellReady(page);
    await openTelegramWizard(page);

    // Click verify with the input still empty — should NOT call the broker.
    let verifyCalled = false;
    await page.route("**/telegram/verify", async (route) => {
      verifyCalled = true;
      await route.fulfill(verifyOK);
    });

    await page.getByTestId("tg-token-submit").click();
    await expect(page.getByRole("alert")).toContainText(/required/i);
    expect(verifyCalled).toBe(false);
  });

  test("[4] verify failure keeps user on token step", async ({ page }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await page.route("**/telegram/verify", (route) =>
      route.fulfill(verifyFail),
    );
    await openTelegramWizard(page);

    await page.getByTestId("tg-token-input").fill("bogus");
    await page.getByTestId("tg-token-submit").click();

    await expect(page.getByTestId("tg-step-token")).toBeVisible();
    await expect(page.getByRole("alert")).toContainText(/401/);
  });

  test("[5,7] verify success → pick → connect → done", async ({ page }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await page.route("**/telegram/verify", (route) => route.fulfill(verifyOK));
    await page.route("**/telegram/discover", (route) =>
      route.fulfill(discoverGroups),
    );

    let connectBody: unknown = null;
    await page.route("**/telegram/connect", async (route, req) => {
      connectBody = req.postDataJSON();
      await route.fulfill(connectOK);
    });

    await openTelegramWizard(page);
    await verifyTokenAndOpenGroupPicker(page);
    await expect(page.getByTestId("tg-group-list")).toBeVisible();

    await page.getByTestId("tg-group--123456").click();

    await expect(page.getByTestId("tg-step-done")).toBeVisible({
      timeout: 5_000,
    });
    await expect(page.getByTestId("tg-created-slug")).toContainText(
      "tg-test-group-a",
    );

    expect(connectBody).toMatchObject({
      token: "123456:ABC",
      chat_id: -123456,
      title: "Test Group A",
    });
  });

  test("[6] empty group list shows retry/manual/dm controls", async ({
    page,
  }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await page.route("**/telegram/verify", (route) => route.fulfill(verifyOK));
    await page.route("**/telegram/discover", (route) =>
      route.fulfill(discoverEmpty),
    );

    await openTelegramWizard(page);
    await verifyTokenAndOpenGroupPicker(page);
    await expect(page.getByTestId("tg-retry-discover")).toBeVisible();
    await expect(page.getByTestId("tg-manual-chat-id")).toBeVisible();
    await page.getByText("Back", { exact: true }).click();
    await expect(page.getByTestId("tg-step-mode")).toBeVisible();
    await expect(page.getByTestId("tg-mode-dm")).toBeVisible();
  });
});

test.describe("wuphf web /connect Telegram wizard connect modes", () => {
  test("[8] manual chat ID validates and posts integer chat_id", async ({
    page,
  }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await page.route("**/telegram/verify", (route) => route.fulfill(verifyOK));
    await page.route("**/telegram/discover", (route) =>
      route.fulfill(discoverEmpty),
    );

    let connectBody: { chat_id?: number } | null = null;
    await page.route("**/telegram/connect", async (route, req) => {
      connectBody = req.postDataJSON();
      await route.fulfill(connectOK);
    });

    await openTelegramWizard(page);
    await verifyTokenAndOpenGroupPicker(page);

    await page.getByTestId("tg-manual-chat-id").click();
    await expect(page.getByTestId("tg-step-manual")).toBeVisible();

    // Non-numeric input → inline error, no POST.
    await page.getByTestId("tg-manual-id-input").fill("not-a-number");
    await page.getByTestId("tg-manual-submit").click();
    await expect(page.getByRole("alert")).toContainText(/integer/i);
    expect(connectBody).toBeNull();

    // Regression for "Number.parseInt accepts trailing junk": "-12abc" used to
    // be silently accepted as -12, which would bridge the wrong chat id. The
    // strict regex check now rejects any non-digit suffix.
    await page.getByTestId("tg-manual-id-input").fill("-12abc");
    await page.getByTestId("tg-manual-submit").click();
    await expect(page.getByRole("alert")).toContainText(/integer/i);
    expect(connectBody).toBeNull();

    // Valid integer → posts chat_id as a number.
    await page.getByTestId("tg-manual-id-input").fill("-5093020979");
    await page.getByTestId("tg-manual-submit").click();
    await expect(page.getByTestId("tg-step-done")).toBeVisible({
      timeout: 5_000,
    });
    expect(connectBody?.chat_id).toBe(-5093020979);
  });

  test("[8b] manual connect failure stays on the manual step", async ({
    page,
  }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await page.route("**/telegram/verify", (route) => route.fulfill(verifyOK));
    await page.route("**/telegram/discover", (route) =>
      route.fulfill(discoverEmpty),
    );
    await page.route("**/telegram/connect", (route) =>
      route.fulfill(connectFail),
    );

    await openTelegramWizard(page);
    await verifyTokenAndOpenGroupPicker(page);

    await page.getByTestId("tg-manual-chat-id").click();
    await page.getByTestId("tg-manual-id-input").fill("-5093020979");
    await page.getByTestId("tg-manual-submit").click();

    await expect(page.getByTestId("tg-step-manual")).toBeVisible();
    await expect(page.getByRole("alert")).toContainText(/chat not found/i);
  });

  test("[9] choosing DM mode posts chat_id=0", async ({ page }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await page.route("**/telegram/verify", (route) => route.fulfill(verifyOK));

    let connectBody: { chat_id?: number; type?: string } | null = null;
    await page.route("**/telegram/connect", async (route, req) => {
      connectBody = req.postDataJSON();
      await route.fulfill(connectOK);
    });

    await openTelegramWizard(page);
    await verifyTokenToMode(page);
    await page.getByTestId("tg-mode-dm").click();
    await expect(page.getByTestId("tg-step-done")).toBeVisible({
      timeout: 5_000,
    });
    expect(connectBody?.chat_id).toBe(0);
    expect(connectBody?.type).toBe("private");
  });

  test("[10] connect failure keeps picker step active with error", async ({
    page,
  }) => {
    await page.goto("/");
    await waitForShellReady(page);

    await page.route("**/telegram/verify", (route) => route.fulfill(verifyOK));
    await page.route("**/telegram/discover", (route) =>
      route.fulfill(discoverGroups),
    );
    await page.route("**/telegram/connect", (route) =>
      route.fulfill(connectFail),
    );

    await openTelegramWizard(page);
    await verifyTokenAndOpenGroupPicker(page);

    await page.getByTestId("tg-group--123456").click();

    await expect(page.getByTestId("tg-step-pick")).toBeVisible();
    await expect(page.getByRole("alert")).toContainText(/chat not found/i);
  });
});

test.describe("wuphf web /connect Telegram wizard regressions", () => {
  test("[10b] modal renders with a visible card surface (regression: wk-* CSS vars must resolve outside .wiki-root)", async ({
    page,
  }) => {
    // Regression for the "wiki.css imported but variables don't resolve"
    // class of bug. The wizard reuses wk-modal-* / wk-editor-* primitives
    // that read CSS custom properties (--wk-paper, --wk-border) — those
    // were originally scoped to .wiki-root, so when the modal opened from
    // anywhere except the Wiki page it rendered as transparent text on
    // top of the underlying view: heading and buttons floating with no
    // card, no border, no backdrop.
    //
    // Visibility-only assertions (toBeVisible / toContainText) couldn't
    // catch this — the testids were present, just unstyled. We assert the
    // computed background and border on the modal card so any future
    // "class exists but variables undefined" regression fails loudly.
    await page.goto("/");
    await waitForShellReady(page);
    await page.route("**/telegram/verify", (r) => r.fulfill(verifyOK));
    await openTelegramWizard(page);

    const styles = await page.evaluate(() => {
      const backdrop = document.querySelector(
        ".wk-modal-backdrop",
      ) as HTMLElement | null;
      const card = document.querySelector(".wk-modal") as HTMLElement | null;
      if (!(backdrop && card)) return null;
      const bs = getComputedStyle(backdrop);
      const cs = getComputedStyle(card);
      return {
        backdropPosition: bs.position,
        backdropBg: bs.backgroundColor,
        cardBg: cs.backgroundColor,
        cardBorderColor: cs.borderColor,
        cardBorderWidth: cs.borderTopWidth,
      };
    });
    if (!styles) {
      throw new Error("modal DOM should be present");
    }
    // Backdrop must dim the page (non-zero alpha) and be position:fixed.
    expect(styles.backdropPosition).toBe("fixed");
    expect(styles.backdropBg).not.toBe("rgba(0, 0, 0, 0)");
    expect(styles.backdropBg).not.toBe("transparent");
    // Card must have an opaque background — the original bug had this as
    // rgba(0,0,0,0) because var(--wk-paper) didn't resolve.
    expect(styles.cardBg).not.toBe("rgba(0, 0, 0, 0)");
    expect(styles.cardBg).not.toBe("transparent");
    // Card must have a visible border (non-zero width). Original bug:
    // border-width was 0px because var(--wk-border) didn't resolve.
    expect(styles.cardBorderWidth).not.toBe("0px");
  });

  test("[10c] verify the wizard renders styled when opened from a non-wiki route (regression)", async ({
    page,
  }) => {
    // The original bug only manifested when the wizard was opened from
    // outside the Wiki page (where wiki.css is imported as a side effect).
    // After the fix, the relevant CSS vars live on :root, so the modal
    // styles correctly regardless of route. This test guards against a
    // regression where a contributor scopes the variables back to
    // .wiki-root, which would re-introduce the unstyled-modal bug.
    await page.goto("/");
    await waitForShellReady(page);
    await page.route("**/telegram/verify", (r) => r.fulfill(verifyOK));
    await openTelegramWizard(page);

    // Confirm we're NOT inside a .wiki-root ancestor (which would mask the
    // bug under test).
    const insideWikiRoot = await page.evaluate(() => {
      return Boolean(
        document.querySelector(".wk-modal")?.closest(".wiki-root"),
      );
    });
    expect(insideWikiRoot).toBe(false);

    // And confirm the variables still resolve. CSS custom properties are
    // inherited, so reading from the modal root tells us whether :root has
    // them defined.
    const paperColor = await page.evaluate(() => {
      const card = document.querySelector(".wk-modal") as HTMLElement | null;
      if (!card) return "";
      return getComputedStyle(card).getPropertyValue("--wk-paper").trim();
    });
    expect(
      paperColor,
      "--wk-paper must be defined globally so the modal renders styled outside .wiki-root",
    ).not.toBe("");
  });

  test("[11] verify ok:false then retry with valid token clears the error and proceeds", async ({
    page,
  }) => {
    // Guards the "stale error state" class of bug: the user types a bad
    // token, sees 401, types a good token, and the modal advances. If
    // verifyAndDiscover doesn't clear `error` on the new attempt, the
    // banner sticks around on the picker step — confusing at best.
    await page.goto("/");
    await waitForShellReady(page);

    let verifyCallCount = 0;
    await page.route("**/telegram/verify", async (route) => {
      verifyCallCount += 1;
      if (verifyCallCount === 1) {
        await route.fulfill(verifyFail);
      } else {
        await route.fulfill(verifyOK);
      }
    });
    await page.route("**/telegram/discover", (route) =>
      route.fulfill(discoverEmpty),
    );

    await openTelegramWizard(page);
    await page.getByTestId("tg-token-input").fill("bogus");
    await page.getByTestId("tg-token-submit").click();
    await expect(page.getByRole("alert")).toContainText(/401/);

    await page.getByTestId("tg-token-input").fill("123456:GOOD");
    await page.getByTestId("tg-token-submit").click();
    await expect(page.getByTestId("tg-step-mode")).toBeVisible({
      timeout: 5_000,
    });
    await page.getByTestId("tg-mode-group").click();
    await expect(page.getByTestId("tg-step-pick")).toBeVisible({
      timeout: 5_000,
    });
    // Error banner from the prior attempt must be gone — a stale `error`
    // state would render here and break the user's mental model.
    await expect(page.getByRole("alert")).toHaveCount(0);
  });
});
