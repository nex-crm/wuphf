import { type APIRequestContext, expect, type Page } from "@playwright/test";

// Shared helpers for the wuphf web e2e suite. Kept under `tests/` with a
// leading underscore so Playwright still discovers spec files in the same
// directory but does not pick this up as a test (Playwright matches `.spec.`
// by default — see playwright.config.ts).
//
// Smoke + Wizard predate this file and inline their own copies. New specs
// (chat, slash-commands, dm, mentions, interview) import from here so the
// React-error and shell-readiness contracts stay in one place.

export function collectReactErrors(page: Page): () => string[] {
  const errors: string[] = [];
  page.on("pageerror", (err) => errors.push(err.message));
  page.on("console", (msg) => {
    if (msg.type() === "error") {
      const text = msg.text();
      // The boundary's own log line is `[WUPHF ErrorBoundary]` — see
      // web/src/App.tsx:69 (`console.error("[WUPHF ErrorBoundary]", ...)`).
      // The earlier "Error boundary" substring (with a space) never matched
      // and was silent dead code; the DOM check in expectNoReactErrors still
      // caught the rendered case but the textual path is what surfaces
      // mid-render crashes that don't unmount the tree.
      if (
        text.includes("Minified React error") ||
        text.includes("WUPHF ErrorBoundary")
      ) {
        errors.push(text);
      }
    }
  });
  return () => errors;
}

export async function waitForReactMount(page: Page): Promise<void> {
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

// Shell-ready means: React mounted, the composer is in the DOM. Avoids
// `networkidle` (long-lived SSE keeps the page non-idle indefinitely).
export async function waitForShellReady(page: Page): Promise<void> {
  await waitForReactMount(page);
  await expect(page.locator(".composer-input")).toBeVisible({
    timeout: 10_000,
  });
}

export async function expectNoReactErrors(
  page: Page,
  getErrors: () => string[],
  context: string,
): Promise<void> {
  await expect(page.getByTestId("error-boundary")).toHaveCount(0);
  const errors = getErrors();
  expect(
    errors,
    `Uncaught errors ${context}:\n  ${errors.join("\n  ")}`,
  ).toHaveLength(0);
}

// Reset broker state between tests in a file. Tests run serially in the same
// wuphf process, so without an explicit reset earlier messages bleed into
// later assertions. Intended for `test.afterEach(() => resetBroker(request))`.
//
// Uses the request fixture (NOT page) so the call works after page teardown.
// Failure here should not fail the test that just passed — log and move on.
export async function resetBroker(request: APIRequestContext): Promise<void> {
  try {
    await request.post("/api/reset", { data: {} });
  } catch (err) {
    console.warn("resetBroker: /api/reset failed (continuing):", err);
  }
}
