// Capture the Routines reframe (PR #1158): a routine is a prompt the agent
// runs in its own chat on a schedule, with per-routine Disable / Publish and
// a multi-session Ask Agent dock. All /api/* and /agent/* traffic is mocked
// so the shots are reproducible without a broker or agent service.

import process from "node:process";

import { installCommonMocks, launchBrowser, shotPage } from "./lib.mjs";

const BASE = process.env.BASE_URL ?? "http://localhost:5273";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh / driver");
  process.exit(2);
}

// Fixed clock so lastRun / at values are stable across runs.
const NOW = Date.parse("2026-07-01T09:00:00Z");
const hrsAgo = (h) => new Date(NOW - h * 3_600_000).toISOString();

const AGENT_ID = "app_c0ffee0badc0ffee";

const AGENT = {
  id: AGENT_ID,
  slug: "pipeline-agent",
  name: "Pipeline Agent",
  icon: "📈",
  summary:
    "Keeps the sales pipeline honest: scores and routes inbound leads, posts a Monday recap of stage moves, and drafts follow-ups for stalled deals.",
  entry: "index.html",
  version: 3,
  status: "ready",
  createdBy: "nex",
  createdAt: hrsAgo(240),
  updatedAt: hrsAgo(2),
  contentHash: "c0ffee",
};

const APP_HTML = `<!doctype html><html><body style="font-family:system-ui;background:#101014;color:#d8d8de;margin:0;padding:28px">
<h2 style="margin:0 0 6px;font-size:16px">Pipeline overview</h2>
<p style="margin:0 0 18px;color:#8b8b95;font-size:12px">Live view produced by this agent's app artifact.</p>
<table style="border-collapse:collapse;font-size:13px;width:100%">
<tr style="text-align:left;color:#8b8b95"><th style="padding:6px 12px 6px 0">Deal</th><th style="padding:6px 12px 6px 0">Stage</th><th style="padding:6px 0">Owner</th></tr>
<tr><td style="padding:6px 12px 6px 0">Acme</td><td style="padding:6px 12px 6px 0">Evaluation</td><td style="padding:6px 0">Priya</td></tr>
<tr><td style="padding:6px 12px 6px 0">Globex</td><td style="padding:6px 12px 6px 0">Negotiation</td><td style="padding:6px 0">Sam</td></tr>
<tr><td style="padding:6px 12px 6px 0">Umbrella</td><td style="padding:6px 12px 6px 0">Stalled</td><td style="padding:6px 0">Priya</td></tr>
</table></body></html>`;

const ROUTINES = [
  {
    id: "rt_recap01",
    agent: AGENT_ID,
    name: "Monday pipeline recap",
    prompt:
      "Summarize last week's pipeline: stage moves, new leads, and anything stalled more than 14 days. Keep it glanceable.",
    schedule: "Every Monday 9:00",
    enabled: true,
    version: 2,
    lastRun: hrsAgo(2),
    sessionId: "sess_recap01",
  },
  {
    id: "rt_hygiene1",
    agent: AGENT_ID,
    name: "CRM hygiene sweep",
    prompt:
      "Find deals with no next step or a past-due close date and draft a nudge for each owner. Do not send anything.",
    schedule: "Every weekday 8:30",
    enabled: true,
    version: 1,
    draft: true,
    lastRun: hrsAgo(25),
    sessionId: "sess_hygiene1",
  },
  {
    id: "rt_digest01",
    agent: AGENT_ID,
    name: "Friday win/loss digest",
    prompt: "Recap the week's closed-won and closed-lost with one line on why.",
    schedule: "Every Friday 16:00",
    enabled: false,
    version: 1,
    sessionId: "sess_digest01",
  },
];

const SESSIONS = [
  { id: "sess_manual01", agent: AGENT_ID, title: "Chat 1", kind: "manual", at: hrsAgo(30) },
  { id: "sess_recap01", agent: AGENT_ID, title: "Monday pipeline recap", kind: "routine", at: hrsAgo(2) },
  { id: "sess_hygiene1", agent: AGENT_ID, title: "CRM hygiene sweep", kind: "routine", at: hrsAgo(25) },
];

const TRANSCRIPTS = {
  sess_manual01: [
    { from: "you", body: "When a new lead comes in, score its fit and route hot ones to the right AE.", at: hrsAgo(30) },
    { from: "nex", body: "Made scoreAndRouteLead(lead) callable — I will use it whenever you hand me a lead.", at: hrsAgo(30) },
  ],
  sess_recap01: [
    { from: "you", body: "(scheduled) Summarize last week's pipeline: stage moves, new leads, and anything stalled more than 14 days.", at: hrsAgo(2) },
    { from: "nex", body: "3 stage moves — Globex to Negotiation, Acme to Evaluation, Initech to Discovery. 2 new leads (Hooli, Stark). Umbrella has been stalled 21 days: owner Priya, last touch was the pricing call.", at: hrsAgo(2) },
  ],
  sess_hygiene1: [
    { from: "you", body: "(scheduled) Find deals with no next step or a past-due close date and draft a nudge for each owner.", at: hrsAgo(25) },
    { from: "nex", body: "2 deals need attention. Drafted nudges for Priya (Umbrella, no next step) and Sam (Globex, close date slipped 9 days) — drafts only, nothing sent.", at: hrsAgo(25) },
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

const ARTIFACTS = [
  {
    id: "art_run01",
    type: "md",
    title: "monday-pipeline-recap-run-4.md",
    producedBy: "Monday pipeline recap",
    at: hrsAgo(2),
    content:
      "# Monday pipeline recap — run 4\n\n**Stage moves (3):** Globex → Negotiation, Acme → Evaluation, Initech → Discovery.\n\n**New leads (2):** Hooli, Stark Industries.\n\n**Stalled:** Umbrella — 21 days, owner Priya, last touch was the pricing call.",
    size: "1.2 KB",
  },
];

async function installAgentMocks(context) {
  // /api/* — the broker side: the agents list, this agent's detail, and the
  // integrations catalog. Order matters: register the more specific routes
  // AFTER the generic ones so they win.
  await context.route("**/api/apps", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ apps: [AGENT] }) }),
  );
  await context.route("**/api/apps/integrations/catalog", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        connected: [
          { platform: "hubspot", name: "HubSpot", logo_url: "", read_actions: ["deals.list"] },
          { platform: "slack", name: "Slack", logo_url: "", read_actions: [] },
        ],
      }),
    }),
  );
  await context.route(`**/api/apps/${AGENT_ID}*`, (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ app: AGENT, html: APP_HTML }),
    }),
  );

  // /agent/* — the agent service's persistence endpoints.
  await context.route("**/agent/tools?*", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ tools: TOOLS }) }),
  );
  await context.route("**/agent/routines?*", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ routines: ROUTINES }) }),
  );
  await context.route("**/agent/sessions?*", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ sessions: SESSIONS }) }),
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
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ artifacts: ARTIFACTS }) }),
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

  // Open the agent detail. The detail chunk is lazy — the first dev-mode
  // compile can take a while, so give the tab strip a generous deadline.
  await page.getByText("Pipeline Agent").first().click();
  await page.getByRole("tab", { name: "Routines" }).waitFor({ timeout: 45_000 });

  // 1. Routines tab — per-routine lifecycle (enable toggle, draft → Publish
  //    new version, Run now, Open its chat) + the new-routine composer.
  await page.getByRole("tab", { name: "Routines" }).click();
  await page.getByText("Monday pipeline recap").filter({ visible: true }).first().waitFor({ timeout: 10_000 });
  await page.waitForTimeout(600);
  await shotPage(page, OUT, "01-routines-tab");

  // 2. Ask Agent dock — jump straight into a routine's chat session; the
  //    session strip shows the agent's other sessions.
  await page.getByText("Open its chat").filter({ visible: true }).first().click();
  await page.getByText("3 stage moves").filter({ visible: true }).first().waitFor({ timeout: 30_000 });
  await page.waitForTimeout(600);
  await shotPage(page, OUT, "02-routine-session-dock");

  // Close the dock before the remaining tab shots.
  await page.keyboard.press("Escape");
  await page.waitForTimeout(400);

  // 3. Tools tab — hydrated from the agent service (agent-only, View code).
  await page.getByRole("tab", { name: "Tools" }).click();
  await page.getByText("Score & route a lead").filter({ visible: true }).first().waitFor({ timeout: 10_000 });
  await page.waitForTimeout(400);
  await shotPage(page, OUT, "03-tools-tab");

  // 4. Artifacts tab — the app artifact plus the routine's md run artifact.
  await page.getByRole("tab", { name: "Artifacts" }).click();
  await page.getByText("monday-pipeline-recap-run-4.md").filter({ visible: true }).first().waitFor({ timeout: 10_000 });
  await page.waitForTimeout(600);
  await shotPage(page, OUT, "04-artifacts-tab");

  console.log("captured 4 states");
} finally {
  await context.close();
  await browser.close();
}
