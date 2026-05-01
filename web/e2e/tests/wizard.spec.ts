import { expect, type Page, type Route, test } from "@playwright/test";

// Fresh-install onboarding smoke. Assumes wuphf was started WITHOUT a
// pre-seeded ~/.wuphf/onboarded.json, so App.tsx routes to the Wizard
// (see App.tsx — onboardingComplete=false → <Wizard>).
//
// This is the path Garry Tan's sudden traffic would have hit. If the
// wizard crashes on first paint for a fresh user, they bounce.

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

async function expectNoReactErrors(
  page: Page,
  getErrors: () => string[],
  context: string,
): Promise<void> {
  await expect(page.getByTestId("error-boundary")).toHaveCount(0);

  // Avoid networkidle here: onboarding also opens the long-lived broker SSE
  // stream, so the page is expected to keep an active request.
  const errors = getErrors();
  expect(
    errors,
    `Uncaught errors ${context}:\n  ${errors.join("\n  ")}`,
  ).toHaveLength(0);
}

const BLUEPRINT_FIXTURE = {
  templates: [
    {
      id: "niche-crm",
      name: "Niche CRM",
      description: "Build and launch a focused CRM.",
      emoji: "🎯",
      agents: [
        {
          slug: "ceo",
          name: "CEO",
          role: "lead",
          checked: true,
          built_in: true,
        },
        {
          slug: "gtm-lead",
          name: "GTM Lead",
          role: "go-to-market",
          checked: true,
        },
        {
          slug: "designer",
          name: "Designer",
          role: "design",
          checked: true,
        },
      ],
      tasks: [
        {
          id: "draft-launch-plan",
          name: "Draft launch plan",
          description: "Plan the first customer loop.",
          prompt: "Draft a launch plan for the first customer loop.",
        },
      ],
    },
  ],
};

const PREREQS_FIXTURE = {
  prereqs: [
    {
      name: "claude",
      required: false,
      found: true,
      ok: true,
      version: "claude test fixture",
    },
    { name: "codex", required: false, found: false, ok: false },
    { name: "opencode", required: false, found: false, ok: false },
    { name: "cursor", required: false, found: false, ok: false },
    { name: "windsurf", required: false, found: false, ok: false },
  ],
};

async function fulfillJson(route: Route, body: unknown): Promise<void> {
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function stubDeterministicWizardEndpoints(
  page: Page,
  captures: {
    config: Record<string, unknown> | null;
    complete: Record<string, unknown> | null;
  },
): Promise<void> {
  await page.route("**/onboarding/state*", (route) =>
    fulfillJson(route, { onboarded: false }),
  );
  await page.route("**/onboarding/blueprints*", (route) =>
    fulfillJson(route, BLUEPRINT_FIXTURE),
  );
  await page.route("**/onboarding/prereqs*", (route) =>
    fulfillJson(route, PREREQS_FIXTURE),
  );
  await page.route("**/config*", async (route) => {
    if (route.request().method() === "POST") {
      captures.config = route.request().postDataJSON() as Record<
        string,
        unknown
      >;
    }
    await fulfillJson(route, { ok: true });
  });
  await page.route("**/onboarding/complete*", async (route) => {
    captures.complete = route.request().postDataJSON() as Record<
      string,
      unknown
    >;
    await fulfillJson(route, { ok: true });
  });
}

// The wizard flow is welcome → identity → templates. Fill the two required
// identity fields so the primary CTA enables and we can advance.
async function advanceToTemplatesStep(page: Page): Promise<void> {
  await expect(page.locator(".wizard-step").first()).toBeVisible({
    timeout: 10_000,
  });
  await page.locator(".wizard-step button.btn-primary").first().click();
  await page.locator("#wiz-company").fill("Smoke Test Co");
  await page.locator("#wiz-description").fill("Smoke test description");
  await page.locator(".wizard-step button.btn-primary").first().click();
}

test.describe("wuphf onboarding wizard smoke", () => {
  test("fresh install lands on the welcome step without crashing", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForReactMount(page);

    // The Wizard renders `.wizard-step` as its root container
    // (see web/src/components/onboarding/Wizard.tsx — WelcomeStep).
    await expect(page.locator(".wizard-step").first()).toBeVisible({
      timeout: 10_000,
    });
    await expectNoReactErrors(page, getErrors, "rendering wizard");
  });

  test("advancing from welcome → identity → templates step does not crash", async ({
    page,
  }) => {
    // Verifies the wizard state machine actually transitions. Flow is:
    // welcome → identity (company + description required) → templates.
    // Assert via `.wizard-panel` on the templates step.
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForReactMount(page);

    await advanceToTemplatesStep(page);

    // Templates step renders `.wizard-panel` (welcome + identity have different markers).
    await expect(page.locator(".wizard-panel").first()).toBeVisible({
      timeout: 10_000,
    });
    await expectNoReactErrors(page, getErrors, "advancing wizard");
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
    await page.goto("/");
    await waitForReactMount(page);

    await advanceToTemplatesStep(page);

    // Wait for at least one template grid (the blueprint picker now
    // renders one grid per category group — Services, Media & Community,
    // Products — so `.template-grid` is not unique). We rely on
    // `.template-card` instead as the unit of a rendered blueprint.
    const cards = page.locator(".template-card");
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });

    // The pre-embed bug rendered exactly zero preset cards — only the
    // separate "Start from scratch" button (which is NOT a .template-card
    // in the grouped layout). So requiring ≥1 card is the regression
    // guard: if embedded templates fail to load, the grouped layout
    // would still render the from-scratch button but produce zero cards.
    const count = await cards.count();
    expect(
      count,
      "expected ≥1 preset blueprint card — embedded templates may have failed to load",
    ).toBeGreaterThan(0);
  });

  test("completes welcome → identity → templates → team → setup → task → ready", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);
    const captures: {
      config: Record<string, unknown> | null;
      complete: Record<string, unknown> | null;
    } = { config: null, complete: null };
    await stubDeterministicWizardEndpoints(page, captures);

    await page.goto("/");
    await waitForReactMount(page);

    await expect(page.locator(".wizard-step").first()).toBeVisible({
      timeout: 10_000,
    });
    await page.locator(".wizard-step button.btn-primary").first().click();

    await expect(page.getByText("Tell us about this office")).toBeVisible();
    await page.locator("#wiz-company").fill("E2E Full Flow Co");
    await page.locator("#wiz-description").fill("Deterministic wizard path");
    await page.locator("#wiz-priority").fill("Launch the first customer loop");
    await page.locator(".wizard-step button.btn-primary").first().click();

    await expect(page.getByText("What should your office run?")).toBeVisible();
    const templateTile = page
      .locator(".template-card")
      .filter({ hasText: "Niche CRM" })
      .first();
    await expect(templateTile).toBeVisible({ timeout: 10_000 });
    await templateTile.click();
    await page.locator(".wizard-step button.btn-primary").first().click();

    await expect(page.getByText("Your team")).toBeVisible();
    await expect(page.getByText("GTM Lead")).toBeVisible();
    await page.locator(".wizard-step button.btn-primary").first().click();

    const claudeTile = page.getByTestId("setup-runtime-tile-Claude Code");
    await expect(claudeTile).toBeVisible({ timeout: 10_000 });
    await expect(claudeTile).toHaveAttribute("aria-pressed", "true");
    await page.locator(".wizard-step button.btn-primary").first().click();

    await expect(page.locator("#wiz-task-input")).toBeVisible();
    await page.locator("#wiz-task-input").fill("Draft the launch plan");
    await page.locator(".wizard-step button.btn-primary").first().click();

    await expect(page.getByText("You're set")).toBeVisible();
    await expect(page.getByText("LLM runtime")).toBeVisible();
    await page.getByTestId("onboarding-submit-button").click();

    await expect
      .poll(() => (captures.complete === null ? "pending" : "done"))
      .toBe("done");

    if (!(captures.config && captures.complete)) {
      throw new Error("expected config and complete payloads to be captured");
    }
    expect(captures.config).toMatchObject({
      memory_backend: "markdown",
      llm_provider: "claude-code",
      llm_provider_priority: ["claude-code"],
    });
    expect(captures.complete).toMatchObject({
      company: "E2E Full Flow Co",
      description: "Deterministic wizard path",
      priority: "Launch the first customer loop",
      runtime: "Claude Code",
      runtime_priority: ["Claude Code"],
      memory_backend: "markdown",
      blueprint: "niche-crm",
      task: "Draft the launch plan",
      skip_task: false,
    });
    expect(captures.complete.agents).toEqual(
      expect.arrayContaining(["ceo", "gtm-lead", "designer"]),
    );
    await expectNoReactErrors(page, getErrors, "completing all wizard steps");
  });
});
