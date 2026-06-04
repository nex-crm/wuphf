// Capture the Composio-backed integrations catalog and detail/audit view.
//
// Run via:
//   web/e2e/screenshots/publish.sh --dry-run integrations-composio-audit <pr-number>

import {
  bootShell,
  installCommonMocks,
  launchBrowser,
  shotPage,
} from "./lib.mjs";
import process from "node:process";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const fixtureLogo = (label) =>
  `data:image/svg+xml,${encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="4" fill="#fff"/><rect x="1" y="1" width="30" height="30" rx="3" fill="none" stroke="#e5e7eb"/><text x="16" y="21" text-anchor="middle" font-family="Arial,sans-serif" font-size="13" font-weight="700" fill="#111827">${label}</text></svg>`,
  )}`;

const CONFIG = {
  llm_provider: "claude-code",
  llm_provider_configured: true,
  llm_provider_kinds: ["claude-code", "codex", "opencode"],
  gateway_kinds: ["openclaw", "hermes-agent"],
  provider_endpoints: {},
  memory_backend: "markdown",
  action_provider: "composio",
  team_lead_slug: "ceo",
  composio_key_set: true,
  telegram_token_set: true,
  openclaw_token_set: false,
  openclaw_gateway_url: "",
  config_path: "/Users/you/.wuphf/config.json",
};

const INTEGRATIONS = {
  providers: [
    {
      provider: "composio",
      label: "Composio",
      configured: true,
      supports_connect: true,
      supports_disconnect: true,
      detail: "Configured",
    },
  ],
  items: [
    {
      provider: "composio",
      platform: "gmail",
      name: "Gmail",
      description: "Read, draft, and send Gmail messages after approval.",
      category: "communication",
      state: "connected",
      connection_key: "ca_gmail_founder",
      connection_name: "Founder Gmail",
      can_connect: true,
      can_disconnect: true,
      last_action_at: "2026-06-04T12:00:00Z",
      last_action_summary: "Sent investor follow-up draft after approval",
    },
    {
      provider: "composio",
      platform: "slack",
      name: "Slack",
      description: "Post channel updates and read thread context.",
      category: "communication",
      state: "available",
      can_connect: true,
      can_disconnect: false,
    },
    {
      provider: "composio",
      platform: "github",
      name: "GitHub",
      description: "Create issues, inspect PRs, and summarize repository work.",
      category: "code",
      state: "connected",
      connection_key: "ca_github_eng",
      connection_name: "Engineering GitHub",
      can_connect: true,
      can_disconnect: true,
      last_action_at: "2026-06-04T11:40:00Z",
      last_action_summary: "Opened follow-up issue for failing CI",
    },
    {
      provider: "composio",
      platform: "googlecalendar",
      name: "Google Calendar",
      description: "Read availability and schedule meetings after approval.",
      category: "productivity",
      logo_url: fixtureLogo("31"),
      state: "available",
      can_connect: true,
      can_disconnect: false,
    },
    {
      provider: "composio",
      platform: "googledrive",
      name: "Google Drive",
      description: "Find, read, and organize workspace files.",
      category: "documents",
      logo_url: fixtureLogo("GD"),
      state: "available",
      can_connect: true,
      can_disconnect: false,
    },
    {
      provider: "composio",
      platform: "notion",
      name: "Notion",
      description: "Search pages and update project databases.",
      category: "knowledge",
      logo_url: fixtureLogo("N"),
      state: "available",
      can_connect: true,
      can_disconnect: false,
    },
    {
      provider: "composio",
      platform: "linear",
      name: "Linear",
      description: "Create issues and update engineering cycles.",
      category: "project management",
      logo_url: fixtureLogo("L"),
      state: "available",
      can_connect: true,
      can_disconnect: false,
    },
    {
      provider: "composio",
      platform: "hubspot",
      name: "HubSpot",
      description: "Update contacts, companies, and deals.",
      category: "revenue",
      logo_url: fixtureLogo("H"),
      state: "available",
      can_connect: true,
      can_disconnect: false,
    },
  ],
};

const AUDIT = {
  events: [
    {
      id: "action-4",
      event_type: "external_action_executed",
      provider: "composio",
      platform: "gmail",
      connection_key: "ca_gmail_founder",
      action_id: "GMAIL_SEND_EMAIL",
      status: "executed",
      actor: "growth",
      channel: "general",
      summary: "Sent investor follow-up draft after approval",
      created_at: "2026-06-04T12:00:00Z",
    },
    {
      id: "req-2",
      event_type: "approval_executed_ok",
      provider: "approval",
      platform: "gmail",
      connection_key: "ca_gmail_founder",
      action_id: "GMAIL_SEND_EMAIL",
      status: "executed_ok",
      actor: "growth",
      channel: "general",
      summary: "Approved by human",
      created_at: "2026-06-04T11:59:20Z",
    },
  ],
};

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 960 },
});

await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/onboarding/state", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          onboarded: true,
          checklist: [],
          checklist_dismissed: true,
        }),
      }),
    );
    await ctx.route("**/api/config", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(CONFIG),
      }),
    );
    await ctx.route("**/api/status/local-providers", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify([]),
      }),
    );
    await ctx.route(/\/api\/integrations(?:\?.*)?$/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(INTEGRATIONS),
      }),
    );
    await ctx.route(/\/api\/integrations\/audit(?:\?.*)?$/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(AUDIT),
      }),
    );
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
await shotPage(page, OUT, "01-integrations-catalog");

await page
  .getByRole("button", { name: /Open Hermes integration settings/i })
  .scrollIntoViewIfNeeded();
await page.waitForTimeout(300);
await shotPage(page, OUT, "02-integrations-gateway-logos");

await page
  .getByRole("button", { name: /Open Telegram integration settings/i })
  .scrollIntoViewIfNeeded();
await page.waitForTimeout(300);
await shotPage(page, OUT, "03-integrations-channel-logo");

await page
  .getByRole("button", { name: /Open Gmail integration settings/i })
  .click();
await page.getByText("Connection key").waitFor({ timeout: 5_000 });
await page.waitForTimeout(400);
await shotPage(page, OUT, "04-integrations-gmail-audit");

await page.getByRole("button", { name: /Back to integrations list/i }).click();
await page.setViewportSize({ width: 390, height: 844 });
await page
  .getByRole("heading", { name: "Integrations" })
  .waitFor({ timeout: 5_000 });
await page.waitForTimeout(400);
await shotPage(page, OUT, "05-integrations-mobile");

console.log(`captured screenshots to ${OUT}`);
await browser.close();
