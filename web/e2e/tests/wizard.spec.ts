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

async function expectNoReactErrors(
  page: Page,
  getErrors: () => string[],
  context: string,
): Promise<void> {
  await expect(page.getByTestId('error-boundary')).toHaveCount(0);

  // Avoid networkidle here: onboarding also opens the long-lived broker SSE
  // stream, so the page is expected to keep an active request.
  const errors = getErrors();
  expect(errors, `Uncaught errors ${context}:\n  ${errors.join('\n  ')}`).toHaveLength(0);
}

// The wizard flow is welcome → identity → templates. Fill the two required
// identity fields so the primary CTA enables and we can advance.
async function advanceToTemplatesStep(page: Page): Promise<void> {
  await expect(page.locator('.wizard-step').first()).toBeVisible({ timeout: 10_000 });
  await page.locator('.wizard-step button.btn-primary').first().click();
  await page.locator('#wiz-company').fill('Smoke Test Co');
  await page.locator('#wiz-description').fill('Smoke test description');
  await page.locator('.wizard-step button.btn-primary').first().click();
}

test.describe('wuphf onboarding wizard smoke', () => {
  test('fresh install lands on the welcome step without crashing', async ({ page }) => {
    const getErrors = collectReactErrors(page);

    await page.goto('/');
    await waitForReactMount(page);

    // The Wizard renders `.wizard-step` as its root container
    // (see web/src/components/onboarding/Wizard.tsx — WelcomeStep).
    await expect(page.locator('.wizard-step').first()).toBeVisible({ timeout: 10_000 });
    await expectNoReactErrors(page, getErrors, 'rendering wizard');
  });

  test('advancing from welcome → identity → templates step does not crash', async ({ page }) => {
    // Verifies the wizard state machine actually transitions. Flow is:
    // welcome → identity (company + description required) → templates.
    // Assert via `.wizard-panel` on the templates step.
    const getErrors = collectReactErrors(page);

    await page.goto('/');
    await waitForReactMount(page);

    await advanceToTemplatesStep(page);

    // Templates step renders `.wizard-panel` (welcome + identity have different markers).
    await expect(page.locator('.wizard-panel').first()).toBeVisible({ timeout: 10_000 });
    await expectNoReactErrors(page, getErrors, 'advancing wizard');
  });

  test('blueprint picker shows shipped preset teams (not just "From scratch")', async ({
    page,
  }) => {
    // Regression guard for the bug where blueprint YAMLs were read from
    // the filesystem only — `npx wuphf` / `curl | bash` users saw the
    // hardcoded "From scratch" card as their only option.
    //
    // With embedded templates wired in (internal/operations fallback FS +
    // root templates_embed.go), the backend's GET /onboarding/blueprints
    // MUST return ≥1 preset regardless of cwd. The wizard renders one
    // `.template-card` per blueprint plus a hardcoded "From scratch"
    // card — so we expect strictly more than 1 card and at least one
    // card whose name differs from "From scratch".
    await page.goto('/');
    await waitForReactMount(page);

    await advanceToTemplatesStep(page);

    // Wait for at least one template grid (the blueprint picker now
    // renders one grid per category group — Services, Media & Community,
    // Products — so `.template-grid` is not unique). We rely on
    // `.template-card` instead as the unit of a rendered blueprint.
    const cards = page.locator('.template-card');
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });

    // The pre-embed bug rendered exactly zero preset cards — only the
    // separate "Start from scratch" button (which is NOT a .template-card
    // in the grouped layout). So requiring ≥1 card is the regression
    // guard: if embedded templates fail to load, the grouped layout
    // would still render the from-scratch button but produce zero cards.
    const count = await cards.count();
    expect(
      count,
      'expected ≥1 preset blueprint card — embedded templates may have failed to load',
    ).toBeGreaterThan(0);
  });
});
