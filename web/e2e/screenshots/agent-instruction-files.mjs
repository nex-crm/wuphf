// Capture the Phase 4b agent instruction-file surfaces:
//   01: AgentProfilePanel — instructions section, file list (collapsed)
//   02: AgentProfilePanel — SOUL expanded (rendered markdown + Edit)
//   03: AgentProfilePanel — SOUL in the editor (reused wiki rich editor)
//   04: AgentProfilePanel (lead) — office USER.md card expanded
//   05: AgentWizard manual — Soul field that seeds SOUL.md
//
// Run via:
//   web/e2e/screenshots/publish.sh agent-instruction-files <pr-number>

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

const CONFIG = {
  llm_provider: "claude-code",
  llm_provider_configured: true,
  llm_provider_priority: [],
  llm_provider_kinds: LLM_KINDS,
  gateway_kinds: ["openclaw", "openclaw-http", "hermes-agent"],
  provider_endpoints: {},
  memory_backend: "markdown",
  action_provider: "auto",
  team_lead_slug: "ceo",
  company_name: "Northwind",
  config_path: "/Users/you/.wuphf/config.json",
};

const MEMBERS = {
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
      provider: { kind: "claude-code", model: "claude-opus-4-8" },
    },
    {
      slug: "growth",
      name: "Growth Lead",
      role: "Demand generation and pipeline",
      emoji: "📈",
      status: "active",
      activity: "drafting a launch plan",
      built_in: false,
      online: true,
      provider: { kind: "codex", model: "gpt-5.4" },
    },
  ],
};

// Realistic instruction-file content, keyed by repo-relative path, so the
// rendered-markdown view reads like a real operating manual.
const FILES = {
  "agents/growth/SOUL.md": `# SOUL — @growth

## Who you are
Relentless about pipeline, allergic to vanity metrics. You would rather ship one
real experiment than write three decks about it.

## Values
- Bias to action: leave durable state (tasks, records, deliverables), not just narration.
- Reuse first: an existing teammate, task, or wiki article beats creating a new one.
- Tell the human the truth, including failures. Never fabricate outcomes or proof artifacts.

## Voice
Direct and concrete. Lead with the number, then the plan.

## Boundaries
- Stay in your lane (demand generation) on execution; route cross-cutting scope calls through the lead.
- Do the smallest real step that moves the work; avoid substitute or busywork artifacts.
`,
  "agents/growth/IDENTITY.md": `# IDENTITY — @growth

- Name: Growth Lead
- Slug: growth
- Role: Demand generation and pipeline
- Expertise: acquisition, lifecycle, funnel analytics
- Runtime: codex / gpt-5.4
`,
  "agents/growth/OPERATIONS.md": `# OPERATIONS — @growth

## How you work
- Take work from durable tasks: claim with team_task, post status as you go, then submit for review or complete. A channel reply alone does not move the work.
- Work in your task's channel and worktree. Escalate scope changes to the lead.
- Write durable decisions to your notebook; @librarian curates notebooks into the team wiki.

## Escalation
- If blocked on something you cannot resolve, post the blocker and notify the owner/lead instead of producing a substitute artifact.
`,
  "agents/growth/TOOLS.md": `# TOOLS — @growth

## Available tools
- team_task
- team_status
- team_broadcast

## Notes
- Prefer the smallest real action over a proof or preview artifact unless the task explicitly asks for one.
- Request a skill or capability when one is missing rather than faking the result.
`,
  "agents/ceo/SOUL.md": `# SOUL — @ceo

## Who you are
The office lead. You coordinate, decompose, and make the final call.

## Boundaries
- You are the lead: you coordinate, decompose, and make the final call. Delegate execution to specialists rather than doing it all yourself.
`,
  "office/USER.md": `# USER — the human this office serves

Northwind is a seed-stage B2B startup. The operator is the founder.

This WUPHF office serves a single human operator. Optimize for their time:
- Handle routine work autonomously; surface only the decisions that genuinely need them.
- Be candid. Report outcomes, tradeoffs, and failures plainly.
- One human owns approvals (plan gates, new-agent creation, external actions). Wait for them on those.
`,
};

async function mockFeature(context) {
  await context.route("**/api/config", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify(CONFIG) }),
  );
  await context.route("**/api/office-members", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify(MEMBERS) }),
  );
  await context.route("**/api/status/local-providers", (r) =>
    r.fulfill({ contentType: "application/json", body: "[]" }),
  );
  await context.route("**/api/skills*", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ skills: [] }),
    }),
  );
  await context.route("**/api/agent-files/read*", (route) => {
    const url = new URL(route.request().url());
    const p = url.searchParams.get("path") || "";
    const content = FILES[p] ?? "# (not written yet)\n";
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ path: p, content, sha: "a1b2c3d", exists: true }),
    });
  });
  // 4c: LLM authoring returns a richer draft for review (never committed).
  await context.route("**/api/agent-files/generate", (route) => {
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        path: "agents/growth/SOUL.md",
        content: `# SOUL — @growth

## Who you are
You are the office's growth engine: every week you turn one sharp hypothesis
into a live experiment, measure it honestly, and kill it fast if the numbers
don't move. Pipeline is the only scoreboard you trust.

## Values
- Evidence over opinion: a 20-lead test beats a 40-slide narrative.
- Compounding: prefer the channel you can run again next week over a one-off spike.
- Radical candor with the founder: surface a flat funnel the day you see it.

## Voice
Crisp, numbers-first, allergic to hedging. Lead with the result, then the why.

## Boundaries
- Own demand generation end to end; route product or pricing calls to the lead.
- Never ship a vanity metric as a win. If it didn't move pipeline, say so.`,
      }),
    });
  });
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 1000 },
});

async function openProfile(slug) {
  // The /agents/<slug> route mounts AgentProfilePanel directly (AgentsTool's
  // AgentDetail), which is far more deterministic than clicking through the
  // restructured sidebar.
  await page.evaluate((s) => {
    window.location.hash = `/agents/${s}`;
  }, slug);
  await page.waitForSelector(".agent-profile-panel", { timeout: 10_000 });
  await page.waitForTimeout(400);
}

async function expandCard(label) {
  const header = page
    .locator(".agent-file-card-header")
    .filter({ hasText: label })
    .first();
  await header.scrollIntoViewIfNeeded();
  await header.click();
  await page.waitForTimeout(350);
}

// ── 01 + 02 + 03: specialist instruction files ─────────────────────────
await installCommonMocks(context, { extra: mockFeature });
await bootShell(page, { afterFlipSelector: ".status-bar" });
await openProfile("growth");

// Bring the instructions section into view, then capture the collapsed list.
await page
  .locator(".agent-profile-section-title", { hasText: "instructions" })
  .first()
  .scrollIntoViewIfNeeded();
await page.waitForTimeout(250);
await shotElement(page, ".agent-profile-panel", OUT, "01-instructions-list");

// Expand SOUL → rendered markdown + Edit affordance.
await expandCard("SOUL");
await page
  .locator(".agent-file-view")
  .first()
  .waitFor({ state: "visible", timeout: 8_000 });
await shotElement(page, ".agent-profile-panel", OUT, "02-soul-expanded");

// 4c: click "Generate with AI" → the LLM drafts a richer SOUL and the reused
// wiki rich editor opens seeded with it for human review. Lazy Tiptap chunk —
// give it room; fall back to a debug shot rather than failing the whole run.
try {
  await page.getByRole("button", { name: /Generate with AI/i }).first().click();
  await page.waitForSelector(".wk-editor", { timeout: 12_000 });
  await page
    .locator(".wk-editor-rich, .wk-editor-rich-fallback")
    .first()
    .waitFor({ state: "visible", timeout: 12_000 });
  await page.waitForTimeout(900);
  await shotElement(page, ".agent-profile-panel", OUT, "03-soul-ai-draft");
} catch (err) {
  console.warn(`editor shot skipped: ${err.message}`);
  await shotPage(page, OUT, "03-soul-ai-draft-DEBUG");
}

// ── 04: lead office USER.md ────────────────────────────────────────────
await installCommonMocks(context, { extra: mockFeature });
await bootShell(page, { afterFlipSelector: ".status-bar" });
await openProfile("ceo");
await page
  .locator(".agent-file-office-label")
  .first()
  .scrollIntoViewIfNeeded();
await page.waitForTimeout(200);
await expandCard("USER");
await page
  .locator(".agent-file-view")
  .first()
  .waitFor({ state: "visible", timeout: 8_000 });
await shotElement(page, ".agent-profile-panel", OUT, "04-office-user-file");

// ── 05: AgentWizard manual — the Soul field ────────────────────────────
await installCommonMocks(context, { extra: mockFeature });
await bootShell(page, { afterFlipSelector: ".status-bar" });
// Land on the Agents app, which hosts the "New agent" affordance.
await page.evaluate(() => {
  window.location.hash = "/agents";
});
await page.waitForTimeout(600);
await page
  .getByRole("button", { name: /new agent|add agent|create agent/i })
  .first()
  .click()
  .catch(async () => {
    await page
      .locator('[aria-label*="agent" i], [title*="agent" i]')
      .first()
      .click();
  });
await page.waitForSelector("text=Create agent", { timeout: 5_000 });
await page.getByRole("button", { name: "Manual" }).click();
await page.waitForTimeout(200);
await page.getByLabel("Name").fill("Revenue Ops");
await page
  .getByLabel(/Soul/i)
  .fill(
    "Relentless about pipeline, allergic to vanity metrics. Direct, never fluffy.",
  );
await page.waitForTimeout(200);
await shotPage(page, OUT, "05-wizard-soul-field");

console.log(`captured screenshots to ${OUT}`);
await browser.close();
