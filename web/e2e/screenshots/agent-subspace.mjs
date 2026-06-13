// Capture the per-agent tabbed subspace (Chat · Tasks · Skills · Policies ·
// Live Stream · Config) for an agent, plus theme coverage.
//
// Run via:
//   web/e2e/screenshots/publish.sh agent-subspace <pr-number>

import process from "node:process";

import {
  bootShell,
  installCommonMocks,
  launchBrowser,
  shotElement,
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
      status: "active",
      activity: "drafting a launch plan",
      task: "Draft the Q3 outbound launch plan",
      built_in: false,
      online: true,
      provider: { kind: "codex", model: "gpt-5.4" },
    },
  ],
};

const CHANNELS = {
  channels: [
    { slug: "general", name: "general", members: ["ceo", "growth"] },
    { slug: "growth__human", name: "Growth Lead", members: ["growth"], type: "dm" },
  ],
};

const TASKS = {
  tasks: [
    {
      id: "GROW-12",
      title: "Draft the Q3 outbound launch plan",
      status: "in_progress",
      owner: "growth",
      created_at: "2026-06-10T09:00:00Z",
      updated_at: "2026-06-13T08:00:00Z",
    },
    {
      id: "GROW-9",
      title: "Audit the lifecycle email funnel",
      status: "blocked",
      owner: "growth",
      created_at: "2026-06-08T09:00:00Z",
      updated_at: "2026-06-12T08:00:00Z",
    },
    {
      id: "GROW-4",
      title: "Ship the pricing-page A/B test",
      status: "done",
      owner: "growth",
      created_at: "2026-06-01T09:00:00Z",
      updated_at: "2026-06-05T08:00:00Z",
    },
    {
      id: "OPS-3",
      title: "Reconcile billing export (CEO-owned)",
      status: "in_progress",
      owner: "ceo",
      created_at: "2026-06-09T09:00:00Z",
      updated_at: "2026-06-11T08:00:00Z",
    },
  ],
};

const SKILLS = {
  skills: [
    {
      name: "outbound-sequencer",
      title: "Outbound Sequencer",
      description: "Builds and schedules multi-touch outbound email sequences.",
      status: "active",
      owner_agents: ["growth"],
    },
    {
      name: "funnel-analytics",
      title: "Funnel Analytics",
      description: "Computes conversion + drop-off across the acquisition funnel.",
      status: "active",
      owner_agents: ["growth"],
    },
    {
      name: "wiki-curator",
      title: "Wiki Curator",
      description: "Promotes notebook entries into curated wiki articles.",
      status: "active",
      owner_agents: ["librarian"],
    },
    {
      name: "competitor-scan",
      title: "Competitor Scan",
      description: "Weekly scan of competitor pricing and positioning changes.",
      status: "active",
      owner_agents: [],
    },
  ],
};

const POLICIES = {
  policies: [
    {
      id: "policy-1",
      source: "human_directed",
      rule: "Never email a prospect more than twice in one week.",
      active: true,
      created_at: "2026-06-10T09:00:00Z",
      agents: ["growth"],
    },
    {
      id: "policy-2",
      source: "auto_detected",
      rule: "Always cite the source wiki article when quoting a metric.",
      active: true,
      created_at: "2026-06-11T09:00:00Z",
    },
    {
      id: "policy-3",
      source: "human_directed",
      rule: "Get human approval before sending anything to a named account.",
      active: true,
      created_at: "2026-06-12T09:00:00Z",
      agents: ["growth", "ceo"],
    },
  ],
};

const MESSAGES = {
  messages: [
    {
      id: "m1",
      channel: "growth__human",
      from: "human:you",
      content: "Hey, where are we on the Q3 outbound plan?",
      ts: "2026-06-13T08:00:00Z",
      created_at: "2026-06-13T08:00:00Z",
    },
    {
      id: "m2",
      channel: "growth__human",
      from: "growth",
      content:
        "Draft is ~70% done — sequencing + ICP segments locked, still costing the paid-amplification leg. I'll have it ready for review this afternoon.",
      ts: "2026-06-13T08:01:00Z",
      created_at: "2026-06-13T08:01:00Z",
    },
  ],
};

const AGENT_LOGS = {
  tasks: [
    { taskId: "GROW-12", toolCallCount: 14, hasError: false },
    { taskId: "GROW-9", toolCallCount: 6, hasError: true },
    { taskId: "GROW-4", toolCallCount: 21, hasError: false },
  ],
};

const FILES = {
  "agents/growth/SOUL.md": `# SOUL — @growth

## Who you are
The office's growth engine. Pipeline is the only scoreboard you trust.

## Values
- Numbers first, then the plan
- Bias to action over vanity metrics

## Voice
Direct without being cold. Lead with the number.

## Boundaries
Stay in your lane on execution; escalate scope changes to the lead.
`,
  "office/USER.md": `# USER — the human this office serves

Northwind is a seed-stage B2B startup. The operator is the founder.
Optimize for their time: handle routine work, surface only real decisions.
`,
};

function json(r, body) {
  r.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
}

async function mockFeature(context) {
  await context.route("**/api/config", (r) => json(r, CONFIG));
  await context.route("**/api/office-members", (r) => json(r, MEMBERS));
  await context.route("**/api/channels", (r) => json(r, CHANNELS));
  await context.route("**/api/status/local-providers", (r) =>
    r.fulfill({ contentType: "application/json", body: "[]" }),
  );
  await context.route("**/api/skills*", (r) => json(r, SKILLS));
  // Host-anchored regexes: a plain "**/api/tasks*" / "**/api/policies*" glob
  // also matches Vite's own module requests for /src/api/tasks.ts and
  // /src/api/policies.ts (served as JS), which would break the app boot.
  await context.route(/\/\/[^/]+\/api\/policies(\?|$|\/)/, (r) => json(r, POLICIES));
  await context.route(/\/\/[^/]+\/api\/tasks(\?|$|\/)/, (r) => json(r, TASKS));
  await context.route(/\/\/[^/]+\/api\/agent-logs(\?|$)/, (r) => json(r, AGENT_LOGS));
  await context.route("**/api/messages*", (r) => json(r, MESSAGES));
  await context.route("**/api/agent-files/read*", (route) => {
    const url = new URL(route.request().url());
    const p = url.searchParams.get("path") || "";
    const content = FILES[p] ?? "# (not written yet)\n";
    json(route, { path: p, content, sha: "a1b2c3d", exists: true });
  });
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1480, height: 1000 },
});

// Diagnostics: surface any client-side crash that would otherwise just
// manifest as a bootShell timeout.
page.on("pageerror", (err) => console.error(`[pageerror] ${err.message}`));
page.on("console", (msg) => {
  if (msg.type() === "error") console.error(`[console.error] ${msg.text()}`);
});

async function openTab(slug, tab) {
  await page.evaluate(
    ({ s, t }) => {
      window.location.hash = `/agents/${s}/${t}`;
    },
    { s: slug, t: tab },
  );
  await page.waitForSelector(".agent-subspace", { timeout: 10_000 });
  await page.waitForTimeout(500);
}

async function setTheme(id) {
  await page.evaluate(async (theme) => {
    const m = await import("/src/stores/app.ts");
    // Mirror RootRoute's theme loader: data-theme attr + the theme stylesheet.
    document.documentElement.setAttribute("data-theme", theme);
    const existing = document.getElementById("wuphf-theme-shot");
    if (existing) existing.remove();
    const link = document.createElement("link");
    link.id = "wuphf-theme-shot";
    link.rel = "stylesheet";
    link.href = `/themes/${theme}.css`;
    document.head.appendChild(link);
    if (m.useAppStore?.setState) {
      try {
        m.useAppStore.setState({ theme });
      } catch {
        /* theme may not be a store field; the attr+link is what renders */
      }
    }
  }, id);
  await page.waitForTimeout(450);
}

// ── Default theme: all six tabs ───────────────────────────────────────
await installCommonMocks(context, { extra: mockFeature });
await bootShell(page, { afterFlipSelector: ".status-bar" });

// The restored collapsible "Agents" roster rail in the sidebar — CEO +
// specialists with avatars, activity pills, and the peek affordance. Any
// row opens that agent's subspace.
const agentsSection = page.locator('[data-testid="sidebar-section-agents"]');
if (await agentsSection.count()) {
  await shotElement(
    page,
    '[data-testid="sidebar-section-agents"]',
    OUT,
    "00-sidebar-agents",
  );
}

await openTab("growth", "chat");
await shotElement(page, ".agent-subspace", OUT, "01-chat");

await openTab("growth", "tasks");
await shotElement(page, ".agent-subspace", OUT, "02-tasks");

await openTab("growth", "skills");
await shotElement(page, ".agent-subspace", OUT, "03-skills");

await openTab("growth", "policies");
await shotElement(page, ".agent-subspace", OUT, "04-policies");

await openTab("growth", "live-stream");
await shotElement(page, ".agent-subspace", OUT, "05-live-stream");

await openTab("growth", "config");
await shotElement(page, ".agent-subspace", OUT, "06-config");

// Config with the SOUL file expanded (purpose hint + rendered view).
const soulHeader = page
  .locator(".agent-file-card-header")
  .filter({ hasText: "SOUL" })
  .first();
if (await soulHeader.count()) {
  await soulHeader.scrollIntoViewIfNeeded();
  await soulHeader.click();
  await page.waitForTimeout(400);
  await shotElement(page, ".agent-subspace", OUT, "07-config-soul-view");

  // Click Edit → the structured block editor (default), one block per section.
  const editBtn = page.locator(".agent-file-edit").first();
  if (await editBtn.count()) {
    await editBtn.click();
    await page.waitForTimeout(400);
    await shotElement(page, ".agent-subspace", OUT, "08-soul-block-editor");
  }
}

// ── Theme coverage: dark + noir on two representative tabs ────────────
await setTheme("nex-dark");
await openTab("growth", "policies");
await shotElement(page, ".agent-subspace", OUT, "09-policies-dark");
await openTab("growth", "config");
await shotElement(page, ".agent-subspace", OUT, "10-config-dark");

await setTheme("noir-gold");
await openTab("growth", "skills");
await shotElement(page, ".agent-subspace", OUT, "11-skills-noir");

console.log(`captured screenshots to ${OUT}`);
await browser.close();
