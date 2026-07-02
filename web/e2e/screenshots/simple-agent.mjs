// Capture the SIMPLE agent variant: the agent is its chat. Three sections
// only — Chat (main screen, multi-session), Tools, Integrations. All /api/*
// and /agent/* traffic is mocked so the shots are reproducible without a
// broker or agent service.

import { installCommonMocks, launchBrowser, shotPage } from "./lib.mjs";
import process from "node:process";

const BASE = process.env.BASE_URL ?? "http://localhost:5273";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error(
    "WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh / driver",
  );
  process.exit(2);
}

// Fixed clock so `at` values are stable across runs.
const NOW = Date.parse("2026-07-01T09:00:00Z");
const hrsAgo = (h) => new Date(NOW - h * 3_600_000).toISOString();

const AGENT_ID = "app_c0ffee0badc0ffee";

const AGENT = {
  id: AGENT_ID,
  slug: "pipeline-agent",
  name: "Pipeline Agent",
  icon: "📈",
  summary:
    "Keeps the sales pipeline honest: scores and routes inbound leads and drafts follow-ups for stalled deals.",
  entry: "index.html",
  version: 2,
  status: "ready",
  createdBy: "nex",
  createdAt: hrsAgo(240),
  updatedAt: hrsAgo(2),
  contentHash: "c0ffee",
};

const SESSIONS = [
  {
    id: "sess_main01",
    agent: AGENT_ID,
    title: "Chat 1",
    kind: "manual",
    at: hrsAgo(30),
  },
  {
    id: "sess_leads01",
    agent: AGENT_ID,
    title: "Lead routing",
    kind: "manual",
    at: hrsAgo(4),
  },
  {
    id: "sess_recap01",
    agent: AGENT_ID,
    title: "Pipeline recap",
    kind: "manual",
    at: hrsAgo(2),
  },
];

const TRANSCRIPTS = {
  sess_main01: [
    {
      from: "you",
      body: "When a new lead comes in, score its fit and route hot ones to the right AE.",
      at: hrsAgo(30),
    },
    {
      from: "nex",
      body: "Made scoreAndRouteLead(lead) callable — I will use it whenever you hand me a lead.",
      at: hrsAgo(30),
    },
  ],
  sess_leads01: [
    {
      from: "you",
      body: "Score the Hooli lead that just came in.",
      at: hrsAgo(4),
    },
    {
      from: "nex",
      body: "Hooli scores 86 (strong ICP fit: mid-market SaaS, 200 seats). Routed to Priya — she owns that segment.",
      at: hrsAgo(4),
    },
  ],
  sess_recap01: [
    {
      from: "you",
      body: "Give me a pipeline recap for last week.",
      at: hrsAgo(2),
    },
    {
      from: "nex",
      body: "3 stage moves — Globex to Negotiation, Acme to Evaluation, Initech to Discovery. 2 new leads (Hooli, Stark). Umbrella has been stalled 21 days: owner Priya, last touch was the pricing call.",
      at: hrsAgo(2),
    },
  ],
};

const TOOLS = [
  {
    name: "scoreAndRouteLead",
    title: "Score & route a lead",
    purpose: "Score a lead's fit and route hot ones to the right AE.",
    inputs: [{ name: "lead", type: "string" }],
    code: "async function scoreAndRouteLead(lead) {\n  const fit = await nex.ai.score(lead, { rubric: 'icp-fit' });\n  if (fit >= 80) {\n    const ae = await crm.ownerFor(lead);\n    await crm.assign(lead, ae);\n    return `Fit ${fit} -> routed to ${ae.name}`;\n  }\n  return `Fit ${fit} -> nurture`;\n}",
    version: 2,
  },
  {
    name: "weeklyPipelineSummary",
    title: "Weekly pipeline summary",
    purpose: "Summarize last week's pipeline into a glanceable exec recap.",
    inputs: [],
    code: "async function weeklyPipelineSummary() {\n  const deals = await crm.deals({ since: '7d' });\n  const moved = deals.filter((d) => d.stageChanged);\n  return nex.ai.summarize(moved, { style: 'exec recap' });\n}",
    version: 1,
  },
];

const CATALOG = {
  items: [
    {
      platform: "hubspot",
      name: "HubSpot",
      category: "CRM",
      state: "connected",
      can_connect: true,
    },
    {
      platform: "slack",
      name: "Slack",
      category: "Messaging",
      state: "connected",
      can_connect: true,
    },
    {
      platform: "gmail",
      name: "Gmail",
      category: "Email",
      state: "available",
      can_connect: true,
    },
    {
      platform: "notion",
      name: "Notion",
      category: "Docs",
      state: "available",
      can_connect: true,
    },
    {
      platform: "stripe",
      name: "Stripe",
      category: "Payments",
      state: "available",
      can_connect: true,
    },
    {
      platform: "linear",
      name: "Linear",
      category: "Issues",
      state: "available",
      can_connect: true,
    },
  ],
};

async function installAgentMocks(context) {
  await context.route("**/api/apps", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ apps: [AGENT] }),
    }),
  );
  await context.route("**/api/apps/integrations/catalog", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        connected: [
          {
            platform: "hubspot",
            name: "HubSpot",
            logo_url: "",
            read_actions: ["deals.list"],
          },
          { platform: "slack", name: "Slack", logo_url: "", read_actions: [] },
        ],
      }),
    }),
  );
  await context.route(`**/api/apps/${AGENT_ID}*`, (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ app: AGENT, html: "<html><body></body></html>" }),
    }),
  );
  await context.route("**/api/integrations?*", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify(CATALOG),
    }),
  );

  // /agent/* — the agent service's persistence endpoints.
  await context.route("**/agent/tools?*", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ tools: TOOLS }),
    }),
  );
  await context.route("**/agent/routines?*", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ routines: [] }),
    }),
  );
  await context.route("**/agent/sessions?*", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ sessions: SESSIONS }),
    }),
  );
  await context.route("**/agent/sessions/*", (r) => {
    const url = new URL(r.request().url());
    const id = url.pathname.split("/").pop() ?? "";
    const session = SESSIONS.find((s) => s.id === id);
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ session, messages: TRANSCRIPTS[id] ?? [] }),
    });
  });
  await context.route("**/agent/artifacts?*", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ artifacts: [] }),
    }),
  );
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 900 },
});

try {
  await installCommonMocks(context);
  await installAgentMocks(context);

  await page.goto(`${BASE}/#/operator`, { waitUntil: "domcontentloaded" });
  await page.getByText("Pipeline Agent").first().waitFor({ timeout: 30_000 });
  await page.waitForTimeout(800);

  // Open the agent — the chat IS the main screen. The detail chunk is lazy, so
  // give the first compile a generous deadline.
  await page.getByText("Pipeline Agent").first().click();
  await page.getByRole("tab", { name: "Chat" }).waitFor({ timeout: 45_000 });

  // 1. Chat — full main screen, session strip on top, hydrated transcript.
  await page
    .getByText("scoreAndRouteLead(lead) callable")
    .filter({ visible: true })
    .first()
    .waitFor({ timeout: 30_000 });
  await page.waitForTimeout(600);
  await shotPage(page, OUT, "01-chat-main");

  // 2. A different session via the strip.
  await page
    .getByRole("button", { name: /Pipeline recap/ })
    .filter({ visible: true })
    .first()
    .click();
  await page
    .getByText("3 stage moves")
    .filter({ visible: true })
    .first()
    .waitFor({ timeout: 15_000 });
  await page.waitForTimeout(400);
  await shotPage(page, OUT, "02-chat-session");

  // 3. Tools — everything the chat has built and can call.
  await page.getByRole("tab", { name: "Tools" }).click();
  await page
    .getByText("Score & route a lead")
    .filter({ visible: true })
    .first()
    .waitFor({ timeout: 10_000 });
  await page.waitForTimeout(400);
  await shotPage(page, OUT, "03-tools");

  // 4. Integrations — connected chips + the workspace catalog.
  await page.getByRole("tab", { name: "Integrations" }).click();
  await page
    .getByText("Connected", { exact: true })
    .filter({ visible: true })
    .first()
    .waitFor({ timeout: 10_000 });
  await page.waitForTimeout(600);
  await shotPage(page, OUT, "04-integrations");

  console.log("captured 4 states");
} finally {
  await context.close();
  await browser.close();
}
