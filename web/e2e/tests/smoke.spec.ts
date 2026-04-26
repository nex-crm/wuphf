import { expect, type Page, test } from "@playwright/test";

// Guards the class of regression that broke for users after the Garry Tan RT:
// React render-time crash ("Minified React error #31 — Objects are not valid
// as a React child") on first agent click. PR #101 fixed the specific bug;
// this test makes sure the next one gets caught in CI instead of in Slack.
//
// Assumes wuphf was started with ~/.wuphf/onboarded.json pre-seeded so the
// app lands in the Shell (where the React #31 crash lived) rather than the
// onboarding Wizard. Wizard coverage lives in wizard.spec.ts.

function collectReactErrors(page: Page): () => string[] {
  const errors: string[] = [];
  page.on("pageerror", (err) => errors.push(err.message));
  page.on("console", (msg) => {
    if (msg.type() === "error") {
      const text = msg.text();
      // The boundary's own log line is `[WUPHF ErrorBoundary]` (single word,
      // no space) — see web/src/App.tsx:69. The previous "Error boundary"
      // substring never matched and was silent dead code.
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

// Wait for React's first commit: the static #skeleton placeholder is gone
// and React has committed something into #root.
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

test.describe("wuphf web UI smoke (shell)", () => {
  test("initial page render does not trip the React error boundary", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForReactMount(page);

    // Sidebar appearing is our "React committed and effects ran" signal.
    // networkidle does NOT work here — wuphf opens a long-lived SSE stream
    // as soon as the shell mounts, so the page is never idle.
    await expect(page.locator("button[data-agent-slug]").first()).toBeVisible({
      timeout: 10_000,
    });

    await expect(page.getByTestId("error-boundary")).toHaveCount(0);

    const errors = getErrors();
    expect(
      errors,
      `Uncaught errors during initial render:\n  ${errors.join("\n  ")}`,
    ).toHaveLength(0);
  });

  test("sidebar renders the seeded agents (broker wired)", async ({ page }) => {
    // Hard assertion: the broker seeds default agents on every boot
    // (see internal/team — 4+ default roles). Zero agents is NEVER the
    // happy path; treating it as "skip" lets real regressions through
    // (seed broken, /api/members failing, useOfficeMembers broken, etc.).
    await page.goto("/");
    await waitForReactMount(page);

    const agentButtons = page.locator("button[data-agent-slug]");
    await expect(agentButtons.first()).toBeVisible({ timeout: 10_000 });
    expect(await agentButtons.count()).toBeGreaterThan(0);
  });

  test("clicking an agent does not crash the UI (React #31 guard)", async ({
    page,
  }) => {
    // The React #31 crash surfaced on first "click CEO". Reproduce that
    // path: click any agent in the sidebar and assert no crash.
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForReactMount(page);

    const agentButtons = page.locator("button[data-agent-slug]");
    await expect(agentButtons.first()).toBeVisible({ timeout: 10_000 });
    await agentButtons.first().click();

    // Deterministic post-click signal: clicking a sidebar agent sets
    // activeAgentSlug in the store, which mounts <AgentPanel> → `.agent-panel`
    // (see components/agents/AgentPanel.tsx). Waiting on the panel — instead
    // of networkidle, which never settles due to the live SSE stream — gives
    // the panel a cycle to render and any errors a cycle to fire.
    await expect(page.locator(".agent-panel").first()).toBeVisible({
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
