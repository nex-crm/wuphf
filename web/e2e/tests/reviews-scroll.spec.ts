import { expect, type Page, type Route, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

async function fulfillJson(route: Route, body: unknown): Promise<void> {
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function crowdedReviews() {
  return Array.from({ length: 28 }, (_, index) => {
    const n = index + 1;
    const ts = new Date(Date.now() - n * 3_600_000).toISOString();
    return {
      id: `review-${n}`,
      agent_slug: "pm",
      entry_slug: `promotion-${n}`,
      entry_title: `Promotion review ${n}`,
      proposed_wiki_path: `team/playbooks/promotion-${n}.md`,
      excerpt:
        "A long-lived wiki promotion that keeps the review card tall enough to exercise vertical scrolling.",
      reviewer_slug: "ceo",
      state: "pending",
      submitted_ts: ts,
      updated_ts: ts,
      comments: [],
    };
  });
}

async function stubReviews(page: Page): Promise<void> {
  await page.route(/\/api\/review\/list(?:\?|$)/, (route) =>
    fulfillJson(route, { reviews: crowdedReviews() }),
  );
}

test("reviews queue scrolls when wiki promotions overflow the viewport", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 640 });
  await stubReviews(page);
  const getErrors = collectReactErrors(page);

  await page.goto("/#/reviews");
  await waitForReactMount(page);

  const board = page.locator(".nb-review-columns");
  await expect(board).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText("Promotion review 28")).toBeAttached();

  const metrics = await board.evaluate((el) => {
    const styles = window.getComputedStyle(el);
    return {
      clientHeight: el.clientHeight,
      overflowY: styles.overflowY,
      scrollHeight: el.scrollHeight,
    };
  });

  expect(metrics.overflowY).toBe("auto");
  expect(metrics.scrollHeight).toBeGreaterThan(metrics.clientHeight);

  await board.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });

  await expect
    .poll(() => board.evaluate((el) => el.scrollTop))
    .toBeGreaterThan(0);
  const lastReview = page.getByText("Promotion review 28");
  await lastReview.scrollIntoViewIfNeeded();
  await expect(lastReview).toBeInViewport();
  await expectNoReactErrors(page, getErrors, "while scrolling reviews");
});
