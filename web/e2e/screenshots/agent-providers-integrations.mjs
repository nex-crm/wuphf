// Capture the redesigned provider + Integrations surfaces:
//   01: Settings → Default runtime (locked, friction gate visible)
//   02: Settings → Default runtime (unlock confirm dialog)
//   03: Integrations app — category groups with all three cards
//   04: Agent profile → Runtime section (editable picker + model field)
//   05: Agent profile → Runtime section (gateway-managed pill)
//   06: AgentWizard manual → Runtime + Model fields
//
// Run via:
//   web/e2e/screenshots/publish.sh agent-providers-integrations <pr-number>

import process from "node:process";

import {
  bootShell,
  installCommonMocks,
  launchBrowser,
  shotElement,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const LLM_KINDS = ["claude-code", "codex", "opencode", "mlx-lm", "ollama", "exo"];
const GATEWAY_KINDS = ["openclaw", "openclaw-http", "hermes-agent"];

function configResponse({ unlocked = false, provider = "claude-code", openclawUrl = "", openclawTokenSet = false, telegramTokenSet = false } = {}) {
  return {
    llm_provider: provider,
    llm_provider_configured: true,
    llm_provider_unlocked: unlocked,
    llm_provider_priority: [],
    llm_provider_kinds: LLM_KINDS,
    gateway_kinds: GATEWAY_KINDS,
    provider_endpoints: {},
    memory_backend: "markdown",
    action_provider: "auto",
    team_lead_slug: "ceo",
    company_name: "Acme Co",
    openclaw_gateway_url: openclawUrl,
    openclaw_token_set: openclawTokenSet,
    telegram_token_set: telegramTokenSet,
    config_path: "/Users/you/.wuphf/config.json",
  };
}

const HERMES_REACHABLE = {
  kind: "hermes-agent",
  binary_installed: true,
  binary_path: "/usr/local/bin/hermes",
  binary_version: "0.6.1",
  endpoint: "http://127.0.0.1:8642/v1",
  model: "hermes-agent",
  reachable: true,
  loaded_model: "hermes-3",
  probed: true,
  platform_supported: true,
};

const MEMBERS_FIXTURE = {
  members: [
    {
      slug: "ceo",
      name: "CEO",
      role: "Office lead",
      emoji: "🎯",
      status: "idle",
      activity: "idle",
      built_in: true,
      online: true,
      provider: { kind: "claude-code", model: "claude-3-5-sonnet-latest" },
    },
    {
      slug: "outreach",
      name: "Outreach",
      role: "Cold email SDR",
      emoji: "📨",
      status: "idle",
      activity: "idle",
      provider: { kind: "codex", model: "gpt-4o" },
    },
    {
      slug: "imported-coder",
      name: "Imported Coder",
      role: "Refactor specialist",
      emoji: "🦾",
      status: "idle",
      activity: "idle",
      provider: { kind: "openclaw", openclaw: { session_key: "wuphf-imported-1", agent_id: "main" } },
    },
  ],
  meta: { humanHasPosted: true },
};

async function mockCfg(context, opts) {
  await context.route("**/api/config", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify(configResponse(opts)),
    }),
  );
}

async function mockProviders(context, hermesReachable = true) {
  await context.route("**/api/status/local-providers", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify([
        hermesReachable ? HERMES_REACHABLE : { ...HERMES_REACHABLE, reachable: false },
        { kind: "mlx-lm", binary_installed: false, endpoint: "http://127.0.0.1:8080/v1", model: "qwen-30b", reachable: false, probed: true, platform_supported: true },
        { kind: "ollama", binary_installed: true, binary_version: "0.4.0", endpoint: "http://127.0.0.1:11434/v1", model: "llama3.1:8b", reachable: true, loaded_model: "llama3.1:8b", probed: true, platform_supported: true },
        { kind: "exo", binary_installed: false, endpoint: "http://127.0.0.1:52415/v1", model: "llama-3.2-3b", reachable: false, probed: true, platform_supported: false },
      ]),
    }),
  );
}

async function mockMembers(context) {
  await context.route("**/api/office-members", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify(MEMBERS_FIXTURE) }),
  );
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 960 },
});

// ── 01: Settings → Default runtime (locked) ─────────────────────────
await installCommonMocks(context, {
  extra: async (ctx) => {
    await mockCfg(ctx, { unlocked: false });
    await mockProviders(ctx);
    await mockMembers(ctx);
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar" });
await page.getByLabel("Open settings").click();
await page.getByRole("button", { name: "General", exact: true }).click();
await page.waitForTimeout(400);
await shotPage(page, OUT, "01-settings-default-runtime-locked");

// ── 02: Settings → Default runtime (unlock confirm dialog) ──────────
// Click (not check) — the checkbox opens an inline confirm panel without
// flipping its own checked state, so check() rightly complains. We want
// the confirm panel visible in the capture.
await page.getByLabel(/Unlock to override all current agents/i).click();
await page.waitForTimeout(300);
await shotPage(page, OUT, "02-settings-unlock-confirm");

// ── 03: Integrations app — category groups ──────────────────────────
await installCommonMocks(context, {
  extra: async (ctx) => {
    await mockCfg(ctx, {
      openclawUrl: "ws://127.0.0.1:18789",
      openclawTokenSet: true,
      telegramTokenSet: false,
    });
    await mockProviders(ctx, true);
    await mockMembers(ctx);
  },
});
// Boot the shell, then hash-navigate. The router uses createHashHistory
// so the URL is /#/apps/integrations, not /apps/integrations — going
// straight to the path bypasses the hash and the SPA falls through to
// its default channel.
await bootShell(page, { afterFlipSelector: ".status-bar" });
await page.evaluate(() => {
  window.location.hash = "/apps/integrations";
});
await page
  .getByRole("heading", { name: "Integrations" })
  .waitFor({ timeout: 10_000 });
await page.waitForTimeout(600);
await shotPage(page, OUT, "03-integrations-app-categories");

// ── 04: AgentWizard manual mode — Runtime + Model fields ────────────
await installCommonMocks(context, {
  extra: async (ctx) => {
    await mockCfg(ctx);
    await mockProviders(ctx);
    await mockMembers(ctx);
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar" });
// Open the agent wizard via the sidebar "+ Agent" button. Fall back to
// clicking the first link/button whose accessible name matches.
await page
  .getByRole("button", { name: /new agent|add agent|create agent/i })
  .first()
  .click()
  .catch(async () => {
    // Some shells use an icon-only trigger; try opening via the agent list.
    const newAgent = await page
      .locator('[aria-label*="agent" i], [title*="agent" i]')
      .first();
    await newAgent.click();
  });
await page.waitForSelector("text=Create agent", { timeout: 5_000 });
await page.getByRole("button", { name: "Manual" }).click();
await page.waitForTimeout(200);
await page.getByLabel("Name").fill("Outreach Specialist");
await page.waitForTimeout(150);
await shotPage(page, OUT, "04-agent-wizard-runtime-model");

console.log(`captured screenshots to ${OUT}`);
await browser.close();
