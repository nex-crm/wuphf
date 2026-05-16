import process from "node:process";

import {
  DEFAULT_BASE,
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

const artifact = {
  id: "ra_0123456789abcdef",
  kind: "notebook_html",
  title: "ICP onboarding walkthrough",
  summary:
    "Interactive HTML plan comparing founder, operator, and reviewer onboarding paths.",
  trustLevel: "draft",
  representation: "html",
  htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
  sourceMarkdownPath: "agents/pm/notebook/icp-onboarding-walkthrough.md",
  createdBy: "pm",
  createdAt: "2026-05-16T12:00:00Z",
  updatedAt: "2026-05-16T12:00:00Z",
  contentHash: "hash",
  sanitizerVersion: "sandbox-v1",
};

const artifactHtml = `<!doctype html>
<html>
  <head>
    <style>
      body { margin: 0; font-family: Inter, system-ui, sans-serif; color: #1f2937; background: #f8fafc; }
      main { padding: 28px; display: grid; gap: 18px; }
      h1 { margin: 0; font-size: 26px; }
      .grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
      .lane { background: white; border: 1px solid #d7dde8; border-radius: 8px; padding: 14px; }
      .lane strong { display: block; margin-bottom: 8px; color: #0f766e; }
      .meter { height: 10px; border-radius: 99px; background: #e5e7eb; overflow: hidden; margin-top: 12px; }
      .meter span { display: block; height: 100%; background: #2563eb; }
    </style>
  </head>
  <body>
    <main>
      <h1>ICP onboarding walkthrough</h1>
      <p>Use the cards to compare what each WUPHF user needs from the first rich-artifact experience.</p>
      <section class="grid">
        <div class="lane"><strong>Founder</strong>Scan the product strategy, ask for tradeoffs, and promote the winning plan.<div class="meter"><span style="width: 82%"></span></div></div>
        <div class="lane"><strong>Operator</strong>Review the weekly status report, copy the next actions, and send it to the team.<div class="meter"><span style="width: 68%"></span></div></div>
        <div class="lane"><strong>Reviewer</strong>Open the code explainer, inspect annotated diffs, and approve with context.<div class="meter"><span style="width: 74%"></span></div></div>
      </section>
    </main>
  </body>
</html>`;

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1280, height: 850 },
});

await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/config", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          llm_provider: "claude-code",
          llm_provider_configured: true,
          memory_backend: "markdown",
          team_lead_slug: "ceo",
        }),
      }),
    );
    const memberPayload = {
      members: [
        {
          slug: "pm",
          name: "Mara",
          role: "Product",
          provider: "claude-code",
          built_in: true,
          online: true,
        },
      ],
      meta: { humanHasPosted: true },
    };
    await ctx.route("**/api/office-members", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(memberPayload),
      }),
    );
    await ctx.route("**/api/members*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(memberPayload),
      }),
    );
    await ctx.route("**/api/channels", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          channels: [
            {
              slug: "general",
              name: "General",
              description: "Shared office channel",
            },
          ],
        }),
      }),
    );
    await ctx.route("**/api/messages*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          messages: [
            {
              id: "msg-rich-artifact",
              from: "pm",
              channel: "general",
              content:
                "I made the interactive onboarding walkthrough for our ICP review.\n\nvisual-artifact:ra_0123456789abcdef",
              timestamp: "2026-05-16T12:00:00Z",
            },
          ],
        }),
      }),
    );
    await ctx.route("**/api/workspaces/list", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ workspaces: [] }),
      }),
    );
    await ctx.route("**/api/requests*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ requests: [] }),
      }),
    );
    await ctx.route("**/api/review/list*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ reviews: [] }),
      }),
    );
    await ctx.route("**/api/tasks/inbox", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          rows: [],
          counts: {
            decisionRequired: 0,
            running: 0,
            blocked: 0,
            mergedToday: 0,
          },
          refreshedAt: "2026-05-16T12:00:00Z",
        }),
      }),
    );
    await ctx.route("**/api/tasks?*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ tasks: [] }),
      }),
    );
    await ctx.route("**/api/usage", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          total: {
            input_tokens: 0,
            output_tokens: 0,
            cache_read_tokens: 0,
            cache_creation_tokens: 0,
            total_tokens: 0,
            cost_usd: 0,
            requests: 0,
          },
        }),
      }),
    );
    await ctx.route("**/api/commands", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify([]),
      }),
    );
    await ctx.route("**/api/upgrade-check", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          current: "0.83.5",
          latest: "0.83.5",
          upgrade_available: false,
          is_dev_build: false,
        }),
      }),
    );
    await ctx.route("**/api/notebook/visual-artifacts/ra_0123456789abcdef", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ artifact, html: artifactHtml }),
      }),
    );
  },
});

await page.goto(`${DEFAULT_BASE}/#/channels/general`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await flipStore(page);
try {
  await page.waitForSelector(".message-artifact-reference", { timeout: 10_000 });
} catch (err) {
  console.error(await page.locator("body").innerText());
  throw err;
}
await page.waitForTimeout(700);
await shotElement(
  page,
  ".thread-group:has(.message-artifact-reference)",
  OUT,
  "01-chat-artifact-card",
);

await page.getByRole("button", { name: "Open", exact: true }).click();
await page
  .getByRole("dialog", { name: "ICP onboarding walkthrough" })
  .waitFor({ timeout: 10_000 });
await shotPage(page, OUT, "02-chat-artifact-modal");

if (errors.length > 0) {
  console.error(errors.join("\n"));
  await browser.close();
  process.exit(1);
}

console.log(`captured 2 screenshots to ${OUT}`);
await browser.close();
