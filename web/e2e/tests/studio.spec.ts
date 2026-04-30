import { expect, type Page, test } from "@playwright/test";

async function waitForReactMount(page: Page) {
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

test.describe("Studio app", () => {
  test("loads the workspace route and creates a channel-bound surface", async ({
    page,
  }) => {
    const requested: string[] = [];
    page.on("request", (req) => requested.push(req.url()));

    await page.goto("/#/apps/studio");
    await waitForReactMount(page);

    await expect(page.getByTestId("studio-app")).toBeVisible({
      timeout: 10_000,
    });
    await page.getByRole("button", { name: /New surface/i }).click();

    await expect(page.getByText(/command center/i).first()).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByTestId("error-boundary")).toHaveCount(0);
    expect(requested.some((url) => url.includes("/surfaces/stream"))).toBe(
      false,
    );
  });
});
