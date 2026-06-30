import { expect, test } from "@playwright/test";

import { collectReactErrors, waitForReactMount } from "./_helpers";

// Guards the class of regression that broke for users after the Garry Tan RT:
// React render-time crash ("Minified React error #31 — Objects are not valid
// as a React child") on first agent click. PR #101 fixed the specific bug;
// this test makes sure the next one gets caught in CI instead of in Slack.
//
// Assumes wuphf was started with ~/.wuphf/onboarded.json pre-seeded so the
// app lands in the Shell (where the React #31 crash lived) rather than the
// onboarding Wizard. Wizard coverage lives in wizard.spec.ts.
//
// The React-error and shell-readiness helpers live in `_helpers.ts` so the
// contract stays in one place (this spec used to inline its own copies).

test.describe("wuphf web UI smoke (shell)", () => {
  test("initial page render does not trip the React error boundary", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);

    // Operator is the index now; the office Shell (where the React #31 crash
    // lived) mounts on its deep routes, so enter through a channel route.
    await page.goto("/#/channels/general");
    await waitForReactMount(page);

    // Sidebar appearing is our "React committed and effects ran" signal.
    // (Agents moved to the Agents tool, so the old sidebar agent buttons are
    // no longer a home-page mount signal.) networkidle does NOT work here —
    // wuphf opens a long-lived SSE stream as soon as the shell mounts.
    await expect(page.locator("aside.sidebar")).toBeVisible({
      timeout: 10_000,
    });

    await expect(page.getByTestId("error-boundary")).toHaveCount(0);

    const errors = getErrors();
    expect(
      errors,
      `Uncaught errors during initial render:\n  ${errors.join("\n  ")}`,
    ).toHaveLength(0);
  });

  test("the Agents tool renders the seeded agents (broker wired)", async ({
    page,
  }) => {
    // Hard assertion: the broker seeds default agents on every boot
    // (see internal/team — 4+ default roles). Agents live in the Agents tool
    // now (not the sidebar). Zero agents is NEVER the happy path; treating it
    // as "skip" lets real regressions through (seed broken, /api/members
    // failing, useOfficeMembers broken, etc.).
    await page.goto("/#/agents");
    await waitForReactMount(page);

    const agentButtons = page.locator(".agents-tool-card[data-agent-slug]");
    await expect(agentButtons.first()).toBeVisible({ timeout: 10_000 });
    expect(await agentButtons.count()).toBeGreaterThan(0);
  });

  test("clicking an agent does not crash the UI (React #31 guard)", async ({
    page,
  }) => {
    // The React #31 crash surfaced on first "click CEO". Reproduce that
    // path: click any agent in the sidebar and assert no crash.
    const getErrors = collectReactErrors(page);

    await page.goto("/#/agents");
    await waitForReactMount(page);

    const agentButtons = page.locator(".agents-tool-card[data-agent-slug]");
    await expect(agentButtons.first()).toBeVisible({ timeout: 10_000 });
    await agentButtons.first().click();

    // Deterministic post-click signal: an Agents-tool card click navigates to
    // that agent's full-screen detail (AgentsTool AgentDetail → `[data-testid=
    // "agent-detail"]`). Waiting on the detail root — instead of networkidle,
    // which never settles due to the live SSE stream — gives the route a cycle
    // to render and any errors a cycle to fire.
    await expect(page).toHaveURL(/#\/agents\/[^/]+/, { timeout: 10_000 });
    await expect(page.getByTestId("agent-detail")).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByTestId("error-boundary")).toHaveCount(0);

    const errors = getErrors();
    expect(
      errors,
      `Uncaught errors after agent click:\n  ${errors.join("\n  ")}`,
    ).toHaveLength(0);
  });
});
