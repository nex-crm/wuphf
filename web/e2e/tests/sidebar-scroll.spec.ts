import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

// Left-sidebar structure + a11y across viewports.
//
// The task-scoped restructure replaced the old crowded sidebar (a scrollable
// roster of agents + a flat channel list) with three fixed labeled groups —
// Work / Knowledge / Config (see AppList NAV_SECTIONS). Agents moved to the
// Agents tool and there is no flat channel list, so the previous "stub 24
// agents + 24 channels to force scrolling" fixture no longer maps to anything.
// These tests assert the real section chrome instead: every section renders
// and is reachable within the scroll region, and a collapsed section is
// removed from the accessibility tree + tab order (the `.sidebar-collapsible`
// inert/aria-hidden contract in SidebarSection).

const VIEWPORTS = [
  { width: 1280, height: 900 },
  { width: 1024, height: 640 },
  { width: 390, height: 700 },
] as const;

const SECTION_TESTIDS = [
  "sidebar-section-work",
  "sidebar-section-knowledge",
  "sidebar-section-config",
] as const;

async function ensureSidebarExpanded(page: Page): Promise<void> {
  const expandedSidebar = page.locator("aside.sidebar:not(.sidebar-collapsed)");
  if (!(await expandedSidebar.isVisible())) {
    await page.getByRole("button", { name: "Expand sidebar" }).click();
  }
  await expect(expandedSidebar).toBeVisible();
}

test.describe("left sidebar scrolling", () => {
  for (const viewport of VIEWPORTS) {
    test(`all sidebar sections are reachable at ${viewport.width}x${viewport.height}`, async ({
      page,
    }) => {
      const getErrors = collectReactErrors(page);
      await page.setViewportSize(viewport);

      await page.goto("/");
      await waitForReactMount(page);
      await ensureSidebarExpanded(page);

      // The three labeled groups all render.
      for (const testId of SECTION_TESTIDS) {
        await expect(page.getByTestId(testId)).toHaveCount(1);
      }

      // The scroll region owns vertical overflow, and the last item in the
      // last (Config) section is reachable through it — content that is
      // clipped or orphaned with no scrollable path fails here even when the
      // sidebar is short enough not to actually scroll.
      const scroll = page.locator(".sidebar-scroll");
      await expect(scroll).toBeVisible();
      const lastItem = page
        .getByTestId("sidebar-section-config")
        .locator("button.sidebar-item")
        .last();
      await lastItem.scrollIntoViewIfNeeded();
      await expect(
        lastItem,
        "the last Config item should be reachable within the sidebar",
      ).toBeInViewport({ ratio: 0.2 });

      await expectNoReactErrors(page, getErrors, "while scrolling the sidebar");
    });

    test(`collapsed section body is removed from a11y tree and tab order at ${viewport.width}x${viewport.height}`, async ({
      page,
    }) => {
      const getErrors = collectReactErrors(page);
      await page.setViewportSize(viewport);

      await page.goto("/");
      await waitForReactMount(page);
      await ensureSidebarExpanded(page);

      // Collapse the Config section (sections default to open).
      const configSection = page.getByTestId("sidebar-section-config");
      const toggle = configSection.locator("button.sidebar-section-toggle");
      await expect(toggle).toHaveAttribute("aria-expanded", "true");
      await toggle.click();
      await expect(toggle).toHaveAttribute("aria-expanded", "false");

      // The collapsible body must be inert + aria-hidden so screen readers
      // skip it and Tab can't land on its buttons (SidebarSection contract).
      const body = configSection.locator(".sidebar-collapsible");
      await expect(body).toHaveAttribute("aria-hidden", "true");
      await expect(body).toHaveAttribute("inert", "");

      await expectNoReactErrors(
        page,
        getErrors,
        "while asserting collapsed-section a11y",
      );
    });
  }
});
