import { expect, type Page, type Route, test } from "@playwright/test";

// Settings → Local LLMs e2e: shell-phase (onboarded.json seeded). Stubs
// the /status/local-providers and /config endpoints with route fulfill
// so we don't need a live ollama / mlx-lm to assert the UI behavior.
// Real status is exercised by `internal/team/local_providers_status_test.go`.

const STATUS_FIXTURE = [
  {
    kind: "mlx-lm",
    binary_installed: false,
    endpoint: "http://127.0.0.1:8080/v1",
    model: "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit",
    reachable: false,
    probed: false,
    platform_supported: true,
    install: { macos: "pipx install mlx-lm" },
    start: {
      macos:
        "mlx_lm.server --model mlx-community/Qwen2.5-Coder-7B-Instruct-4bit --host 127.0.0.1 --port 8080",
    },
    notes: ["Requires Apple Silicon."],
  },
  {
    kind: "ollama",
    binary_installed: true,
    binary_version: "ollama version is 0.1.31",
    endpoint: "http://127.0.0.1:11434/v1",
    model: "qwen2.5-coder:7b-instruct-q4_K_M",
    reachable: true,
    loaded_model: "llama3.2:latest",
    probed: true,
    platform_supported: true,
  },
  {
    kind: "exo",
    binary_installed: true,
    endpoint: "http://127.0.0.1:52415/v1",
    model: "default",
    reachable: false,
    probed: true,
    platform_supported: true,
    start: { macos: "exo", linux: "exo" },
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

// stubLocalProviders intercepts /status/local-providers and /config so
// the test asserts on UI behavior rather than depending on what the
// developer's machine has installed. Returns a tracker that records
// each /config POST body so save assertions can be exact.
function stubLocalProviders(page: Page) {
  const configPosts: unknown[] = [];
  page.route("**/status/local-providers*", (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(STATUS_FIXTURE),
    }),
  );
  page.route("**/config*", async (route: Route) => {
    const req = route.request();
    if (req.method() === "POST") {
      const body = req.postDataJSON();
      configPosts.push(body);
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: '{"status":"ok"}',
      });
      return;
    }
    // GET returns a base config plus the latest provider_endpoints from
    // any prior POST so the UI's "save then reload" flow works.
    const merged: Record<string, unknown> = {
      llm_provider: "claude-code",
      memory_backend: "markdown",
      provider_endpoints: {},
    };
    for (const p of configPosts as Record<string, unknown>[]) {
      if (p.llm_provider) merged.llm_provider = p.llm_provider;
      if (p.provider_endpoints) {
        merged.provider_endpoints = {
          ...(merged.provider_endpoints as object),
          ...(p.provider_endpoints as object),
        };
      }
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(merged),
    });
  });
  return configPosts;
}

// goToSettingsLocalLLMs lands the page on the shell, opens Settings via
// the sidebar's "Open settings" button (aria-label set in Sidebar.tsx),
// then clicks the new "Local LLMs" nav. Centralised so each test starts
// at the same place regardless of how the shell evolves.
async function goToSettingsLocalLLMs(page: Page) {
  await page.goto("/");
  await waitForReactMount(page);
  await page.getByLabel("Open settings").click();
  await page.getByRole("button", { name: "Local LLMs" }).click();
}

test.describe("Settings → Local LLMs", () => {
  test("renders one card per registered runtime with copy-paste install command for not-installed runtimes", async ({
    page,
  }) => {
    stubLocalProviders(page);
    await goToSettingsLocalLLMs(page);

    // All three cards render.
    await expect(page.getByTestId("local-llm-card-mlx-lm")).toBeVisible();
    await expect(page.getByTestId("local-llm-card-ollama")).toBeVisible();
    await expect(page.getByTestId("local-llm-card-exo")).toBeVisible();

    // MLX-LM is not installed → install command appears verbatim.
    const mlxCard = page.getByTestId("local-llm-card-mlx-lm");
    await expect(mlxCard.getByText("pipx install mlx-lm")).toBeVisible();

    // Ollama is installed AND reachable → no "Install" snippet should
    // surface (the doctor dot already says everything is fine).
    const ollamaCard = page.getByTestId("local-llm-card-ollama");
    await expect(ollamaCard.getByText("brew install ollama")).toHaveCount(0);
    await expect(
      ollamaCard.getByText("ollama version is 0.1.31"),
    ).toBeVisible();

    // Exo is installed but not reachable → start command surfaces.
    const exoCard = page.getByTestId("local-llm-card-exo");
    await expect(exoCard.getByText(/exo/, { exact: false })).toBeVisible();
  });

  test("Set as default posts llm_provider to /config", async ({ page }) => {
    const configPosts = stubLocalProviders(page);
    await goToSettingsLocalLLMs(page);

    await page.getByTestId("local-llm-set-default-ollama").click();

    // Wait for the POST to land (mutation runs through react-query so it's async).
    await expect
      .poll(() => configPosts.length, { timeout: 5_000 })
      .toBeGreaterThan(0);
    const post = configPosts[0] as Record<string, unknown>;
    expect(post.llm_provider).toBe("ollama");
  });

  test("Save endpoint persists provider_endpoints map for the kind", async ({
    page,
  }) => {
    const configPosts = stubLocalProviders(page);
    await goToSettingsLocalLLMs(page);

    const mlxCard = page.getByTestId("local-llm-card-mlx-lm");
    await mlxCard
      .getByTestId("local-llm-base-url-mlx-lm")
      .fill("http://127.0.0.1:9000/v1");
    await mlxCard.getByTestId("local-llm-model-mlx-lm").fill("custom-model");
    await mlxCard.getByRole("button", { name: "Save endpoint" }).click();

    await expect
      .poll(() => configPosts.length, { timeout: 5_000 })
      .toBeGreaterThan(0);
    const post = configPosts.at(-1) as Record<string, unknown>;
    const endpoints = post.provider_endpoints as Record<
      string,
      {
        base_url?: string;
        model?: string;
      }
    >;
    expect(endpoints["mlx-lm"]).toEqual({
      base_url: "http://127.0.0.1:9000/v1",
      model: "custom-model",
    });
  });

  test("Save endpoint with empty inputs is a no-op (does not write resolved defaults as override)", async ({
    page,
  }) => {
    // Reviewer caught a regression where leaving the inputs at their
    // resolved defaults and clicking Save would persist those defaults
    // as a permanent provider_endpoints override — locking future
    // users out of upstream default changes. The fix is dirty-gating
    // Save; this test asserts it.
    const configPosts = stubLocalProviders(page);
    await goToSettingsLocalLLMs(page);

    // Click Save without touching either input.
    const mlxCard = page.getByTestId("local-llm-card-mlx-lm");
    await mlxCard.getByRole("button", { name: "Save endpoint" }).click();

    // Wait long enough for any write to land — none should.
    await page.waitForTimeout(500);
    const sawProviderEndpointsWrite = (
      configPosts as Record<string, unknown>[]
    ).some((p) => p.provider_endpoints !== undefined);
    expect(sawProviderEndpointsWrite).toBe(false);
  });

  test("Saved endpoint round-trips through a page reload", async ({ page }) => {
    // The user-stated need: "I want to point mlx-lm at a custom
    // model, hit save, refresh, and have my override stick." Stub
    // /config so the same recorded POST also drives the next GET.
    const configPosts = stubLocalProviders(page);
    await goToSettingsLocalLLMs(page);

    const mlxCard = page.getByTestId("local-llm-card-mlx-lm");
    await mlxCard
      .getByTestId("local-llm-base-url-mlx-lm")
      .fill("http://127.0.0.1:9000/v1");
    await mlxCard.getByTestId("local-llm-model-mlx-lm").fill("custom-model-v2");
    await mlxCard.getByRole("button", { name: "Save endpoint" }).click();
    await expect
      .poll(() => configPosts.length, { timeout: 5_000 })
      .toBeGreaterThan(0);

    // Reload the shell — Settings → Local LLMs again should show the
    // user's override pre-filled, not the placeholder.
    await page.reload();
    await waitForReactMount(page);
    await page.getByLabel("Open settings").click();
    await page.getByRole("button", { name: "Local LLMs" }).click();
    const baseURLAfter = await page
      .getByTestId("local-llm-base-url-mlx-lm")
      .inputValue();
    const modelAfter = await page
      .getByTestId("local-llm-model-mlx-lm")
      .inputValue();
    expect(baseURLAfter).toBe("http://127.0.0.1:9000/v1");
    expect(modelAfter).toBe("custom-model-v2");
  });

  test("doctor card surfaces a clear failure state when /status/local-providers errors", async ({
    page,
  }) => {
    // Settings page must NOT crash or render a half-filled state
    // when the broker can't service /status/local-providers (5xx,
    // network error, missing endpoint). The user reads the dot color
    // + status copy, so an inline failure note is required.
    page.route("**/status/local-providers*", (route: Route) =>
      route.fulfill({ status: 500, contentType: "text/plain", body: "down" }),
    );
    page.route("**/config*", (route: Route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          llm_provider: "claude-code",
          provider_endpoints: {},
        }),
      }),
    );

    await goToSettingsLocalLLMs(page);
    // Section header still renders (no React crash).
    await expect(page.getByText(/Local LLMs/i).first()).toBeVisible();
    // An inline failure note must surface so the user sees WHY their
    // detection didn't run — silent empty section was the bug.
    await expect(page.getByText(/Failed to load status/i).first()).toBeVisible({
      timeout: 5_000,
    });
  });

  test("Recheck button re-fetches /status/local-providers", async ({
    page,
  }) => {
    let calls = 0;
    page.route("**/status/local-providers*", async (route: Route) => {
      calls += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(STATUS_FIXTURE),
      });
    });
    page.route("**/config*", (route: Route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          llm_provider: "claude-code",
          provider_endpoints: {},
        }),
      }),
    );

    await goToSettingsLocalLLMs(page);
    // Initial mount fetch.
    await expect
      .poll(() => calls, { timeout: 5_000 })
      .toBeGreaterThanOrEqual(1);
    const before = calls;

    await page.getByTestId("local-llms-refresh").click();
    await expect.poll(() => calls, { timeout: 5_000 }).toBeGreaterThan(before);
  });
});
