// Capture the redesigned provider + Integrations surfaces:
//   01: Settings → Default runtime for new agents (single picker, no lock)
//   02: Integrations app — list view with branded service marks
//   03: Integrations app — detail view (OpenClaw config screen)
//   04: AgentProfilePanel runtime section — editable picker + model
//   05: AgentProfilePanel runtime section — gateway-managed pill
//   06: AgentWizard manual → Runtime + Model fields with copy
//
// Run via:
//   web/e2e/screenshots/publish.sh agent-providers-integrations <pr-number>

import process from "node:process";

import {
  bootShell,
  installCommonMocks,
  launchBrowser,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const LLM_KINDS = ["claude-code", "codex", "opencode", "mlx-lm", "ollama", "exo"];
const GATEWAY_KINDS = ["openclaw", "openclaw-http", "hermes-agent"];

function configResponse({
  provider = "claude-code",
  openclawUrl = "",
  openclawTokenSet = false,
  telegramTokenSet = false,
} = {}) {
  return {
    llm_provider: provider,
    llm_provider_configured: true,
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

// Mock agents. "outreach" is the editable-runtime example used in screenshot
// 04. "imported-coder" is openclaw-bridged so its runtime section renders the
// MANAGED BY OpenClaw gateway pill in screenshot 05.
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
      provider: {
        kind: "openclaw",
        openclaw: { session_key: "wuphf-imported-1", agent_id: "main" },
      },
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
        hermesReachable
          ? HERMES_REACHABLE
          : { ...HERMES_REACHABLE, reachable: false },
        {
          kind: "mlx-lm",
          binary_installed: false,
          endpoint: "http://127.0.0.1:8080/v1",
          model: "qwen-30b",
          reachable: false,
          probed: true,
          platform_supported: true,
        },
        {
          kind: "ollama",
          binary_installed: true,
          binary_version: "0.4.0",
          endpoint: "http://127.0.0.1:11434/v1",
          model: "llama3.1:8b",
          reachable: true,
          loaded_model: "llama3.1:8b",
          probed: true,
          platform_supported: true,
        },
        {
          kind: "exo",
          binary_installed: false,
          endpoint: "http://127.0.0.1:52415/v1",
          model: "llama-3.2-3b",
          reachable: false,
          probed: true,
          platform_supported: false,
        },
      ]),
    }),
  );
}

async function mockMembers(context) {
  await context.route("**/api/office-members", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify(MEMBERS_FIXTURE),
    }),
  );
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 960 },
});

// ── 01: Settings → Default runtime for new agents ───────────────────
await installCommonMocks(context, {
  extra: async (ctx) => {
    await mockCfg(ctx);
    await mockProviders(ctx);
    await mockMembers(ctx);
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar" });
await page.getByLabel("Open settings").click();
await page.getByRole("button", { name: "General", exact: true }).click();
await page.waitForTimeout(400);
await shotPage(page, OUT, "01-settings-default-runtime");

// ── 02: Integrations app — list view ────────────────────────────────
await installCommonMocks(context, {
  extra: async (ctx) => {
    await mockCfg(ctx, {
      openclawUrl: "ws://127.0.0.1:18789",
      openclawTokenSet: true,
    });
    await mockProviders(ctx, true);
    await mockMembers(ctx);
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar" });
await page.evaluate(() => {
  window.location.hash = "/apps/integrations";
});
await page
  .getByRole("heading", { name: "Integrations" })
  .waitFor({ timeout: 10_000 });
await page.waitForTimeout(600);
await shotPage(page, OUT, "02-integrations-list");

// ── 03: Integrations app — detail view (OpenClaw config) ────────────
await page
  .getByRole("button", { name: /Open OpenClaw integration settings/i })
  .click();
await page.waitForSelector("text=Gateway URL", { timeout: 5_000 });
await page.waitForTimeout(300);
await shotPage(page, OUT, "03-integrations-detail-openclaw");

// ── 04 & 05: AgentProfilePanel runtime sections ─────────────────────
// Open Outreach to show the editable runtime picker; then Imported Coder
// to show the gateway-managed pill. The Profile button flips AgentPanel
// to AgentProfilePanel state.
async function openProfile(slug) {
  await page.locator(`[data-agent-slug="${slug}"]`).first().click();
  // Wait for the AgentPanel summary view to mount (which means the members
  // query has resolved and `agent` is non-null). Without this the Profile
  // click can race the panel mount and land on nothing.
  await page.waitForSelector(".agent-panel", { timeout: 8_000 });
  await page.waitForTimeout(200);
  // The Profile button's accessible name comes from its aria-label
  // (`View full profile for <agent name>`), not its visible text. Match
  // on the aria-label prefix so the lookup survives roster changes.
  await page
    .getByRole("button", { name: /View full profile for/i })
    .first()
    .click();
  await page.waitForTimeout(400);
}

await installCommonMocks(context, {
  extra: async (ctx) => {
    await mockCfg(ctx);
    await mockProviders(ctx);
    await mockMembers(ctx);
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar" });
await openProfile("outreach");
// Wait for either runtime-grid (editable) or runtime-managed pill (gateway).
// Outreach's binding is codex (non-gateway) so we expect the grid; the
// OR-pattern lets us debug into the panel even if the selector raced.
// If the wait times out, snapshot the page so we can see the failure
// state, then re-throw so the harness exits non-zero — silently writing
// a wrong screenshot 04 would be worse than no screenshot at all.
try {
  await page.waitForSelector(
    ".op-runtime-grid, .op-runtime-managed, .agent-profile-panel",
    { timeout: 8_000 },
  );
} catch (err) {
  await shotPage(page, OUT, "DEBUG-after-profile-click");
  throw err;
}
await page.waitForTimeout(700);
await shotPage(page, OUT, "04-agent-runtime-editable");

await openProfile("imported-coder");
await page.waitForSelector(".op-runtime-managed, .agent-profile-panel", {
  timeout: 8_000,
});
await page.waitForTimeout(700);
await shotPage(page, OUT, "05-agent-runtime-gateway-managed");

// ── 06: AgentWizard manual mode — Runtime + Model fields ────────────
await installCommonMocks(context, {
  extra: async (ctx) => {
    await mockCfg(ctx);
    await mockProviders(ctx);
    await mockMembers(ctx);
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar" });
await page
  .getByRole("button", { name: /new agent|add agent|create agent/i })
  .first()
  .click()
  .catch(async () => {
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
await shotPage(page, OUT, "06-agent-wizard-runtime-model");

console.log(`captured screenshots to ${OUT}`);
await browser.close();
