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

test.describe("Onboarding → Run a local model", () => {
  test("meta-tile reveals the picker grid with all three runtimes and their badges", async ({
    page,
  }) => {
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    // The meta-tile sits in the main runtime grid as a peer of the
    // cloud CLIs. The picker grid is hidden until it's clicked.
    const metaTile = page.getByTestId("onboarding-local-llm-toggle");
    await expect(metaTile).toBeVisible();
    await expect(metaTile).toHaveAttribute("aria-pressed", "false");
    await expect(page.getByTestId("onboarding-local-llm-picker")).toHaveCount(
      0,
    );

    // Click the meta-tile → picker appears + /status fetch runs.
    await metaTile.click();
    await expect(metaTile).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByTestId("onboarding-local-llm-picker")).toBeVisible();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-mlx-lm"),
    ).toBeVisible();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-ollama"),
    ).toBeVisible();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-exo"),
    ).toBeVisible();

    // mlx-lm is reachable in the fixture → "Running" badge, selectable.
    const mlxTile = page.getByTestId("onboarding-local-llm-tile-mlx-lm");
    await expect(mlxTile.getByText(/Running/)).toBeVisible();
    await expect(mlxTile).toBeEnabled();

    // ollama is not installed → tile is disabled (selecting a runtime
    // that isn't installed lands the user in a broken shell where
    // every agent turn fails connection-refused). Status copy nudges
    // them to Settings for install commands.
    const ollamaTile = page.getByTestId("onboarding-local-llm-tile-ollama");
    await expect(ollamaTile.getByText(/Not installed.*Settings/)).toBeVisible();
    await expect(ollamaTile).toBeDisabled();

    // exo is platform_supported=false → tile is disabled and surfaces
    // "Not supported on this OS".
    const exoTile = page.getByTestId("onboarding-local-llm-tile-exo");
    await expect(exoTile).toBeDisabled();
    await expect(exoTile.getByText(/Not supported/)).toBeVisible();
  });

  test("clicking a supported runtime tile toggles selection (so wizard state can persist on Continue)", async ({
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

  test("disabled runtime tile (platform_supported=false) does not change selection", async ({
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

  test("API key row defaults to CLI login and reveals the input only on demand", async ({
    page,
  }) => {
    // The API keys panel should default to "Use CLI login" (so a user
    // with claude/codex/gcloud already logged in doesn't see a wall of
    // empty password fields), and reveal the input only when the user
    // explicitly clicks "Use API key".
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    const cliButton = page.getByTestId("api-key-cli-ANTHROPIC_API_KEY");
    const pasteButton = page.getByTestId("api-key-paste-ANTHROPIC_API_KEY");
    const input = page.getByTestId("api-key-input-ANTHROPIC_API_KEY");

    // Default: CLI login active, input not in the DOM.
    await expect(cliButton).toHaveClass(/selected/);
    await expect(pasteButton).not.toHaveClass(/selected/);
    await expect(input).toHaveCount(0);

    // Click "Use API key" → input shows + selected state flips.
    await pasteButton.click();
    await expect(pasteButton).toHaveClass(/selected/);
    await expect(input).toBeVisible();

    // Type a value → still shown after toggling away (we don't drop
    // the user's pasted key when they reconsider).
    await input.fill("sk-ant-test");
    await cliButton.click();
    await expect(cliButton).toHaveClass(/selected/);
    // CLI was clicked → key cleared so the wizard doesn't ship a key
    // the user explicitly toggled away from.
    await expect(input).toHaveCount(0);
  });

  test("Nex API key panel hides unless memory backend needs it", async ({
    page,
  }) => {
    // The Nex API key panel only matters when memory_backend === "nex".
    // Team wiki (default) doesn't need a Nex key; surfacing the input
    // would suggest a missing config piece when there isn't one.
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    // Default backend is Markdown (Team wiki) → no Nex panel.
    await expect(page.getByTestId("wizard-nex-api-key-panel")).toHaveCount(0);

    // Click the Nex memory tile → panel appears.
    await page
      .locator(".wizard-panel")
      .filter({ hasText: "Organizational memory" })
      .getByRole("button", { name: /^Nex/ })
      .click();
    await expect(page.getByTestId("wizard-nex-api-key-panel")).toBeVisible();

    // Switch back to Team wiki → panel disappears again.
    await page
      .locator(".wizard-panel")
      .filter({ hasText: "Organizational memory" })
      .getByRole("button", { name: /Team wiki/ })
      .click();
    await expect(page.getByTestId("wizard-nex-api-key-panel")).toHaveCount(0);
  });

  test("picking a local runtime adds it to the fallback chain (alongside cloud CLIs)", async ({
    page,
  }) => {
    // The user's stated need: "run out of cloud tokens, fall through
    // to local — not onto pay-as-you-go API". Locked in by checking
    // the local label appears as a row in the fallback-order list
    // once it's selected and at least one cloud CLI is also active.
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    // The wizard auto-selects the first installed cloud CLI on mount,
    // so we already have one runtime in the priority list. Confirm by
    // looking for a single-item fallback list (which the UI hides
    // until length > 1 — so we won't see anything yet).
    await expect(page.locator(".runtime-priority-controls")).toHaveCount(0);

    // Pick a local runtime → fallback list now has 2 entries and is
    // shown.
    await page.getByTestId("onboarding-local-llm-toggle").click();
    await page.getByTestId("onboarding-local-llm-tile-mlx-lm").click();
    await expect(page.locator(".runtime-priority-controls")).toBeVisible();
    await expect(
      page.locator(".runtime-priority-row-label").filter({ hasText: "MLX-LM" }),
    ).toBeVisible();
  });

  test("removing a local row from the fallback chain clears the picker selection (no drift)", async ({
    page,
  }) => {
    // Regression for the v7 Major finding: localProvider and
    // runtimePriority were two sources of truth. Removing a local
    // row via the ✕ button only updated runtimePriority; localProvider
    // stayed set, so canContinue stayed true and finishOnboarding()
    // serialized a config without the local runtime in the chain.
    // The fix syncs both sides — the meta-tile must visibly un-select
    // and the picker tile must lose its aria-pressed=true state.
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    await page.getByTestId("onboarding-local-llm-toggle").click();
    const mlxTile = page.getByTestId("onboarding-local-llm-tile-mlx-lm");
    await mlxTile.click();
    await expect(mlxTile).toHaveAttribute("aria-pressed", "true");

    // Remove via the ✕ button on the fallback chain row.
    const mlxRow = page.locator(".runtime-priority-row").filter({
      has: page.locator(".runtime-priority-row-label", { hasText: "MLX-LM" }),
    });
    await mlxRow.getByRole("button", { name: /Remove MLX-LM/i }).click();

    // Picker tile is no longer pressed AND the row is gone.
    await expect(mlxTile).toHaveAttribute("aria-pressed", "false");
    await expect(
      page.locator(".runtime-priority-row-label").filter({ hasText: "MLX-LM" }),
    ).toHaveCount(0);
  });

  test("status fetch failure: fall-open so the picker isn't deadlocked", async ({
    page,
  }) => {
    // Regression for the contradiction CodeRabbit caught in v6: the
    // fetch-error banner says "you can still pick a runtime", but the
    // tile-disable logic was keying on `installed=Boolean(undefined)`
    // and disabling every tile. After this fix, an unreachable status
    // endpoint must NOT trap the user — tiles render selectable, the
    // banner is honest, and the agent-turn surface (or the doctor
    // card in Settings) will catch a real install gap later.
    page.route("**/status/local-providers*", (route: Route) =>
      route.fulfill({
        status: 500,
        contentType: "text/plain",
        body: "broker offline",
      }),
    );
    await advanceToSetupStep(page);

    await page.getByTestId("onboarding-local-llm-toggle").click();
    await expect(
      page.getByTestId("onboarding-local-llm-fetch-error"),
    ).toBeVisible();

    // All three tiles remain selectable. Each one shows the
    // "Status unknown" copy so the user knows we couldn't probe.
    for (const kind of ["mlx-lm", "ollama", "exo"] as const) {
      const tile = page.getByTestId(`onboarding-local-llm-tile-${kind}`);
      await expect(tile).toBeVisible();
      await expect(tile).toBeEnabled();
      await expect(tile.getByText(/Status unknown/)).toBeVisible();
    }

    // Click actually selects — locks in that the click handler isn't
    // short-circuiting on `selectable=false`.
    const mlxTile = page.getByTestId("onboarding-local-llm-tile-mlx-lm");
    await mlxTile.click();
    await expect(mlxTile).toHaveAttribute("aria-pressed", "true");
  });

  test("toggling meta-tile off hides the picker and clears selection", async ({
    page,
  }) => {
    // The meta-tile in the main grid acts as a peer of the cloud CLIs.
    // Turning it off must hide the second-step picker AND clear any
    // local-provider selection so the wizard doesn't ship the user a
    // local config they didn't intend.
    stubStatusEndpoint(page);
    await advanceToSetupStep(page);

    const metaTile = page.getByTestId("onboarding-local-llm-toggle");
    await metaTile.click();
    const mlxTile = page.getByTestId("onboarding-local-llm-tile-mlx-lm");
    await mlxTile.click();
    await expect(mlxTile).toHaveAttribute("aria-pressed", "true");

    // Turn the meta-tile off.
    await metaTile.click();
    await expect(metaTile).toHaveAttribute("aria-pressed", "false");
    await expect(page.getByTestId("onboarding-local-llm-picker")).toHaveCount(
      0,
    );

    // And the selection is gone — re-opening shows the runtime
    // unpressed.
    await metaTile.click();
    await expect(
      page.getByTestId("onboarding-local-llm-tile-mlx-lm"),
    ).toHaveAttribute("aria-pressed", "false");
  });
});
