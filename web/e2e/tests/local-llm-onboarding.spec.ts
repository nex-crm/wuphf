import { expect, type Page, type Route, test } from "@playwright/test";

// Wizard-phase e2e for the new "Run agents on a local model instead?"
// subsection inside the Setup step. Stubs /status/local-providers so
// the test asserts on UI behavior without depending on what the dev box
// has installed. Setup-step tile selection / submit flow itself isn't
// exercised end-to-end — wizard.spec.ts covers that.

const STATUS_FIXTURE = [
  {
    kind: "mlx-lm",
    binary_installed: true,
    endpoint: "http://127.0.0.1:8080/v1",
    model: "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit",
    reachable: true,
    loaded_model: "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit",
    probed: true,
    platform_supported: true,
  },
  {
    kind: "ollama",
    binary_installed: false,
    endpoint: "http://127.0.0.1:11434/v1",
    model: "qwen2.5-coder:7b-instruct-q4_K_M",
    reachable: false,
    probed: false,
    platform_supported: true,
  },
  {
    kind: "exo",
    binary_installed: false,
    endpoint: "http://127.0.0.1:52415/v1",
    model: "default",
    reachable: false,
    probed: false,
    platform_supported: false,
    windows_note: "WSL2 recommended.",
  },
];

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

// stubStatusEndpoint intercepts /status/local-providers only — wizard
// tests need the rest of the onboarding endpoints to behave normally
// against the live broker.
function stubStatusEndpoint(page: Page) {
  page.route("**/status/local-providers*", (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(STATUS_FIXTURE),
    }),
  );
}

// advanceToSetupStep walks the wizard from "welcome" through to the
// "setup" step where the local-LLM subsection lives. Each step exposes
// its primary action as `.wizard-step button.btn-primary`; the identity
// step gates that button on company + description. Same pattern as
// wizard.spec.ts::advanceToTemplatesStep.
async function advanceToSetupStep(page: Page) {
  await page.goto("/");
  await waitForReactMount(page);
  await expect(page.locator(".wizard-step").first()).toBeVisible({
    timeout: 10_000,
  });

  // welcome → identity
  await page.locator(".wizard-step button.btn-primary").first().click();
  // identity → templates (required fields)
  await page.locator("#wiz-company").fill("E2E Local LLM");
  await page.locator("#wiz-description").fill("Onboarding subsection test");
  await page.locator(".wizard-step button.btn-primary").first().click();
  // templates → team: the templates step needs a tile picked; the
  // primary button stays enabled when a default is preselected by the
  // backend, but we click a template tile defensively to keep the test
  // resilient if the default ever changes.
  const templateTile = page.locator(".blueprint-card, .template-tile").first();
  if (await templateTile.count()) {
    await templateTile.click();
  }
  await page.locator(".wizard-step button.btn-primary").first().click();
  // team → setup
  await page.locator(".wizard-step button.btn-primary").first().click();

  // We're on setup when the local-LLM toggle becomes visible.
  await expect(page.getByTestId("onboarding-local-llm-toggle")).toBeVisible({
    timeout: 10_000,
  });
}

test.describe("Onboarding → Run agents on a local model", () => {
  test("subsection toggles open and lists all three runtimes with installed/missing badges", async ({
    page,
  }) => {
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    // The toggle is collapsed by default.
    const toggle = page.getByTestId("onboarding-local-llm-toggle");
    await expect(toggle).toBeVisible();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-mlx-lm"),
    ).toHaveCount(0);

    // Expanding fetches /status/local-providers and shows three tiles.
    await toggle.click();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-mlx-lm"),
    ).toBeVisible();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-ollama"),
    ).toBeVisible();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-exo"),
    ).toBeVisible();

    // mlx-lm is reachable in the fixture → "Running" badge.
    await expect(
      page.getByTestId("onboarding-local-llm-tile-mlx-lm").getByText(/Running/),
    ).toBeVisible();

    // ollama is not installed → "Not installed".
    await expect(
      page
        .getByTestId("onboarding-local-llm-tile-ollama")
        .getByText(/Not installed/),
    ).toBeVisible();

    // exo is platform_supported=false → tile is disabled (button) and
    // surfaces "Not supported on this OS".
    const exoTile = page.getByTestId("onboarding-local-llm-tile-exo");
    await expect(exoTile).toBeDisabled();
    await expect(exoTile.getByText(/Not supported/)).toBeVisible();
  });

  test("clicking a supported tile toggles selection (so wizard state can persist on Continue)", async ({
    page,
  }) => {
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    await page.getByTestId("onboarding-local-llm-toggle").click();

    const mlxTile = page.getByTestId("onboarding-local-llm-tile-mlx-lm");
    await expect(mlxTile).toHaveAttribute("aria-pressed", "false");
    await mlxTile.click();
    await expect(mlxTile).toHaveAttribute("aria-pressed", "true");

    // Click again clears the selection — the wizard's Continue gate is
    // satisfied either way (cloud CLI or local), but allowing the
    // toggle-off path keeps the UI honest if the user changes their mind.
    await mlxTile.click();
    await expect(mlxTile).toHaveAttribute("aria-pressed", "false");
  });

  test("disabled tile (platform_supported=false) does not change selection", async ({
    page,
  }) => {
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    await page.getByTestId("onboarding-local-llm-toggle").click();

    const exoTile = page.getByTestId("onboarding-local-llm-tile-exo");
    // force:true bypasses Playwright's "actionability" check so we
    // exercise the JS guard in the click handler, not the DOM disabled
    // attribute. Real users can't reach this state via mouse, but we
    // still want the handler to refuse.
    await exoTile.click({ force: true });
    await expect(exoTile).toHaveAttribute("aria-pressed", "false");
  });
});
