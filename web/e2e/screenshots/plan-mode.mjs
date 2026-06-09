// Capture the native plan-mode surfaces:
//   01: Task composer — the "Plan first" toggle that routes a task through
//       Planning (the entry point to native plan mode)
//   02: AgentWizard manual → "Autonomy" selector (Plan first / Auto)
//   03: AgentProfilePanel permissions → autonomy "plan first"
//   04: AgentProfilePanel permissions → autonomy "auto"
//   05: AgentPanel live stream → "plan ready" card (a planning turn's plan)
//
// Run via:
//   web/e2e/screenshots/publish.sh plan-mode <pr-number>

import process from "node:process";

import {
  bootShell,
  flipStore,
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

const CONFIG = {
  llm_provider: "claude-code",
  llm_provider_configured: true,
  llm_provider_priority: [],
  llm_provider_kinds: LLM_KINDS,
  gateway_kinds: GATEWAY_KINDS,
  provider_endpoints: {},
  memory_backend: "markdown",
  action_provider: "auto",
  team_lead_slug: "ceo",
  company_name: "Acme Co",
  config_path: "/Users/you/.wuphf/config.json",
};

// Two agents with distinct autonomy so the profile pill is shown both ways.
const MEMBERS_FIXTURE = {
  members: [
    {
      slug: "designer",
      name: "Designer",
      role: "UI / UX",
      emoji: "🎨",
      status: "idle",
      activity: "idle",
      online: true,
      permission_mode: "plan",
      provider: { kind: "claude-code", model: "claude-3-5-sonnet-latest" },
    },
    {
      slug: "eng",
      name: "Founding Engineer",
      role: "Full-stack",
      emoji: "🛠️",
      status: "idle",
      activity: "idle",
      online: true,
      permission_mode: "auto",
      provider: { kind: "codex", model: "gpt-5-codex" },
    },
  ],
  meta: { humanHasPosted: true },
};

// A `plan` HeadlessEvent — the read-only planning turn's harvested plan. Sent
// with status:"idle" after a replay-end marker so useAgentStream renders the
// card and then cleanly closes the stream (no reconnect loop).
const PLAN_EVENT = {
  kind: "headless_event",
  type: "plan",
  provider: "claude",
  agent: "designer",
  task_id: "ACME-7",
  status: "idle",
  text:
    "Goal: add a dark-mode toggle to the settings page.\n\n" +
    "Approach:\n" +
    "1. Add a `theme` field to the user-prefs store with a persisted default.\n" +
    "2. Render a Toggle in SettingsPanel bound to the store.\n" +
    "3. Apply `data-theme` on <html> and load the matching tokens CSS.\n\n" +
    "Acceptance: the toggle flips the theme, the choice survives reload, and " +
    "all three themes still pass the contrast check.\n\n" +
    "Risks: the tokens loader is sync today — confirm no flash of unstyled " +
    "content on first paint.",
};

function sseBody(event) {
  // replay-end flips useAgentStream into the "live" phase; the idle plan event
  // then renders + closes the stream.
  return `event: replay-end\ndata: \n\n` + `data: ${JSON.stringify(event)}\n\n`;
}

async function mockAll(context, { stream = false } = {}) {
  await context.route("**/api/config", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify(CONFIG) }),
  );
  await context.route("**/api/status/local-providers", (r) =>
    r.fulfill({ contentType: "application/json", body: "[]" }),
  );
  await context.route("**/api/office-members", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify(MEMBERS_FIXTURE),
    }),
  );
  if (stream) {
    await context.route("**/agent-stream/**", (r) =>
      r.fulfill({
        contentType: "text/event-stream",
        headers: { "cache-control": "no-cache" },
        body: sseBody(PLAN_EVENT),
      }),
    );
  }
}

async function gotoHash(page, hash) {
  await page.evaluate((h) => {
    window.location.hash = h;
  }, hash);
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 960 },
});

// ── 01: Task composer — the "Plan first" toggle ─────────────────────
await installCommonMocks(context, { extra: (ctx) => mockAll(ctx) });
await bootShell(page, { afterFlipSelector: ".status-bar" });
await page.waitForSelector(".task-composer", { timeout: 10_000 });
await page.waitForTimeout(300);
await shotElement(page, ".task-composer", OUT, "01-task-composer-plan-first");

// ── 02: AgentWizard — Autonomy selector ─────────────────────────────
await installCommonMocks(context, { extra: (ctx) => mockAll(ctx) });
await bootShell(page, { afterFlipSelector: ".status-bar" });
await gotoHash(page, "/agents");
await page.waitForSelector('[data-testid="agents-tool"]', { timeout: 10_000 });
await page.getByTitle("Create a new agent").first().click();
await page.waitForSelector("text=Create agent", { timeout: 5_000 });
await page.getByRole("button", { name: "Manual" }).click();
await page.getByLabel("Name").fill("Brand Designer");
await page.locator("#agent-permission-mode").scrollIntoViewIfNeeded();
await page.waitForTimeout(200);
await shotPage(page, OUT, "02-agent-wizard-autonomy");

// ── 03 & 04: AgentProfilePanel autonomy pill ────────────────────────
await installCommonMocks(context, { extra: (ctx) => mockAll(ctx) });
await bootShell(page, { afterFlipSelector: ".status-bar" });

// The autonomy pill lives in the panel's "permissions" section, below the
// fold of the scroll container — scroll it into view and capture that section.
const PERMISSIONS = ".agent-profile-section:has(.agent-profile-permissions)";

await gotoHash(page, "/agents/designer");
await page.waitForSelector(".agent-profile-panel", { timeout: 10_000 });
await page.locator(PERMISSIONS).scrollIntoViewIfNeeded();
await page.waitForTimeout(400);
await shotElement(page, PERMISSIONS, OUT, "03-agent-profile-plan-first");

await gotoHash(page, "/agents/eng");
await page.waitForSelector(".agent-profile-panel", { timeout: 10_000 });
await page.locator(PERMISSIONS).scrollIntoViewIfNeeded();
await page.waitForTimeout(400);
await shotElement(page, PERMISSIONS, OUT, "04-agent-profile-auto");

// ── 05: Plan-ready card in the agent live stream ────────────────────
await installCommonMocks(context, { extra: (ctx) => mockAll(ctx, { stream: true }) });
await bootShell(page, { afterFlipSelector: ".status-bar" });
await flipStore(page, { activeAgentSlug: "designer" });
await page.waitForSelector(".agent-panel", { timeout: 10_000 });
await page.waitForSelector(".stream-card-plan", { timeout: 10_000 });
await page.waitForTimeout(300);
await shotElement(page, ".stream-card-plan", OUT, "05-plan-ready-card");

console.log(`captured screenshots to ${OUT}`);
await browser.close();
