import { test, expect, type Page } from '@playwright/test';

// Fresh-install onboarding smoke. Assumes wuphf was started WITHOUT a
// pre-seeded ~/.wuphf/onboarded.json, so App.tsx routes to the Wizard
// (see App.tsx — onboardingComplete=false → <Wizard>).
//
// This is the path Garry Tan's sudden traffic would have hit. If the
// wizard crashes on first paint for a fresh user, they bounce.

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

async function waitForReactMount(page: Page): Promise<void> {
  await page.waitForFunction(
    () => {
      const root = document.getElementById('root');
      if (!root) return false;
      if (document.getElementById('skeleton')) return false;
      return root.children.length > 0;
    },
    { timeout: 10_000 },
  );
}

test.describe('wuphf onboarding wizard smoke', () => {
  test('fresh install lands on the welcome step without crashing', async ({ page }) => {
    const getErrors = collectReactErrors(page);

    await page.goto('/');
    await waitForReactMount(page);

    // The Wizard renders `.wizard-step` as its root container
    // (see web/src/components/onboarding/Wizard.tsx — WelcomeStep).
    await expect(page.locator('.wizard-step').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByTestId('error-boundary')).toHaveCount(0);

    await page.waitForLoadState('networkidle');
    const errors = getErrors();
    expect(
      errors,
      `Uncaught errors rendering wizard:\n  ${errors.join('\n  ')}`,
    ).toHaveLength(0);
  });

  test('advancing from welcome → templates step does not crash', async ({ page }) => {
    // Verifies the wizard state machine actually transitions. The welcome
    // step has a single primary CTA; clicking it should render the
    // templates step. Assert via the progress-dot count staying > 0 and
    // a new `.wizard-panel` (only templates step onward has panels).
    const getErrors = collectReactErrors(page);

    await page.goto('/');
    await waitForReactMount(page);

    await expect(page.locator('.wizard-step').first()).toBeVisible({ timeout: 10_000 });
    await page.locator('.wizard-step button.btn-primary').first().click();

    // Templates step renders `.wizard-panel` (welcome does not).
    await expect(page.locator('.wizard-panel').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByTestId('error-boundary')).toHaveCount(0);

    await page.waitForLoadState('networkidle');
    const errors = getErrors();
    expect(
      errors,
      `Uncaught errors advancing wizard:\n  ${errors.join('\n  ')}`,
    ).toHaveLength(0);
  });
});
