import { test, expect, type Page } from '@playwright/test';

// Guards the class of regression that broke for users after the Garry Tan RT:
// React render-time crash ("Minified React error #31 — Objects are not valid
// as a React child") on first agent click. PR #101 fixed the specific bug;
// this test makes sure the next one gets caught in CI instead of in Slack.

function collectReactErrors(page: Page): () => string[] {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      const text = msg.text();
      if (text.includes('Minified React error') || text.includes('Error boundary')) {
        errors.push(text);
      }
    }
  });
  return () => errors;
}

// Wait for React's first commit: the static #skeleton placeholder is gone
// (or replaced) and the boot-timeout diagnostic hasn't fired.
async function waitForReactMount(page: Page): Promise<void> {
  await page.waitForFunction(
    () => {
      const root = document.getElementById('root');
      if (!root) return false;
      // Skeleton replaced by React output, OR React has committed *something*
      // other than the skeleton div.
      const skeleton = document.getElementById('skeleton');
      if (skeleton) return false;
      return root.children.length > 0;
    },
    { timeout: 10_000 },
  );
}

test.describe('wuphf web UI smoke', () => {
  test('initial page render does not trip the React error boundary', async ({ page }) => {
    const getErrors = collectReactErrors(page);

    await page.goto('/');
    await waitForReactMount(page);

    // The error boundary prints this literal string on render failure
    // (see web/src/App.tsx — ErrorBoundary component). If it's visible, a
    // render crashed somewhere up the tree.
    await expect(page.locator('body')).not.toContainText('Something broke in the UI');
    await expect(page.locator('body')).not.toContainText('Minified React error');

    // Drain any lingering async render errors before asserting.
    await page.waitForTimeout(500);
    const errors = getErrors();
    expect(
      errors,
      `Uncaught errors during initial render:\n  ${errors.join('\n  ')}`,
    ).toHaveLength(0);
  });

  test('navigating into an agent channel does not crash the UI', async ({ page }) => {
    // The React #31 crash surfaced on first "click CEO". Reproduce that
    // navigation path: find any agent entry in the sidebar and click it.
    const getErrors = collectReactErrors(page);

    await page.goto('/');
    await waitForReactMount(page);

    // Agents are listed in the sidebar (AgentList.tsx renders a button with
    // data-agent-slug for each). Pack contents vary across environments, so
    // skip if none are present rather than flake.
    const agentButtons = page.locator('button[data-agent-slug]');
    const agentCount = await agentButtons.count();
    test.skip(agentCount === 0, 'no agents rendered in sidebar — onboarding or empty pack');

    await agentButtons.first().click();

    await expect(page.locator('body')).not.toContainText('Something broke in the UI');
    await expect(page.locator('body')).not.toContainText('Minified React error');

    await page.waitForTimeout(500);
    const errors = getErrors();
    expect(
      errors,
      `Uncaught errors after agent click:\n  ${errors.join('\n  ')}`,
    ).toHaveLength(0);
  });
});
