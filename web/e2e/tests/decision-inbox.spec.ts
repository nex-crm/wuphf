import { expect, type Page, test } from "@playwright/test";

// Lane G of the multi-agent control loop. /inbox + /task/:id ride on top
// of mocked broker fixtures (USE_MOCKS=true in src/api/lifecycle.ts) until
// Lanes A/C merge real `/api/tasks` endpoints, so this E2E is fully
// deterministic without the broker.

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

test.describe("decision inbox happy path", () => {
  test("/inbox → click first row → /task/:id renders the Decision Packet", async ({
    page,
  }) => {
    await page.goto("/#/inbox");
    await waitForReactMount(page);

    const inbox = page.getByTestId("decision-inbox");
    await expect(inbox).toBeVisible({ timeout: 10_000 });

    // Three needs-decision rows render from the populated mock fixture.
    const firstRow = page.locator(".inbox-row").first();
    await expect(firstRow).toBeVisible();

    // State pill renders the human-readable label inside the row meta.
    await expect(
      firstRow.locator(".lifecycle-state-pill").first(),
    ).toContainText(/decision|blocked|running|review/i);

    // Click the first row → Decision Packet view.
    await firstRow.click();
    await expect(page.locator(".packet-shell")).toBeVisible({
      timeout: 10_000,
    });

    // The 3-column landmarks render: navigation (left), main (center),
    // complementary (right action sidebar).
    await expect(
      page.getByRole("navigation", { name: /task context/i }),
    ).toBeVisible();
    await expect(page.getByRole("main")).toBeVisible();
    await expect(
      page.getByRole("complementary", { name: /decision actions/i }),
    ).toBeVisible();

    // All five action buttons are present and not blank.
    await expect(page.getByRole("button", { name: /^merge/i })).toBeVisible();
    await expect(
      page.getByRole("button", { name: /request changes/i }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: /^defer/i })).toBeVisible();
    await expect(page.getByRole("button", { name: /^block/i })).toBeVisible();
    await expect(
      page.getByRole("button", { name: /open in worktree/i }),
    ).toBeVisible();

    // Reviewer grades render with severity tier text labels (color is
    // never the only signal).
    const grades = page.locator(".packet-grade");
    await expect(grades.first()).toBeVisible();
    expect(await grades.count()).toBeGreaterThanOrEqual(1);
  });
});
