// Capture the conversation channel participant rail.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh channel-participants <pr-number>

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

const OFFICE_MEMBERS = [
  {
    slug: "ceo",
    name: "CEO",
    role: "Lead agent",
    status: "active",
    liveActivity: "Reviewing the launch plan",
    built_in: true,
    online: true,
  },
  {
    slug: "builder",
    name: "Builder",
    role: "Engineering",
    status: "shipping",
    task: "Implementing UI polish",
    online: true,
  },
  {
    slug: "design",
    name: "Designer",
    role: "Product design",
    detail: "Idle",
    online: false,
  },
  {
    slug: "research",
    name: "Researcher",
    role: "Market research",
    online: false,
  },
];

let channelMembers = [
  OFFICE_MEMBERS[0],
  OFFICE_MEMBERS[1],
  { ...OFFICE_MEMBERS[2], disabled: true },
  { slug: "human", name: "Human", role: "Viewer" },
];

const messages = [
  {
    id: "msg-1",
    from: "ceo",
    channel: "general",
    content: "Builder and Designer are assigned to this channel.",
    timestamp: "2026-05-11T12:00:00Z",
  },
  {
    id: "msg-2",
    from: "builder",
    channel: "general",
    content: "I have the participant rail wired with channel-level controls.",
    timestamp: "2026-05-11T12:03:00Z",
  },
];

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 940 },
});

await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/channels", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          channels: [
            {
              slug: "general",
              name: "general",
              description: "Team discussion",
              members: ["human", "ceo", "builder", "design"],
            },
          ],
        }),
      }),
    );
    await ctx.route("**/api/office-members", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          members: OFFICE_MEMBERS,
          meta: { humanHasPosted: true },
        }),
      }),
    );
    await ctx.route("**/api/members*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ members: channelMembers }),
      }),
    );
    await ctx.route("**/api/messages*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ messages }),
      }),
    );
    await ctx.route("**/api/requests*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ requests: [] }),
      }),
    );
    await ctx.route("**/api/channel-members", async (r) => {
      const body = JSON.parse(r.request().postData() || "{}");
      if (body.action === "add") {
        const profile = OFFICE_MEMBERS.find((m) => m.slug === body.slug);
        if (profile && !channelMembers.some((m) => m.slug === body.slug)) {
          channelMembers = [...channelMembers, profile];
        }
      }
      if (body.action === "remove") {
        channelMembers = channelMembers.filter((m) => m.slug !== body.slug);
      }
      if (body.action === "disable" || body.action === "enable") {
        channelMembers = channelMembers.map((m) =>
          m.slug === body.slug
            ? { ...m, disabled: body.action === "disable" }
            : m,
        );
      }
      await r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ ok: true }),
      });
    });
  },
});

await bootShell(page, {
  afterFlipSelector: ".channel-participants",
  settleMs: 700,
});
await shotPage(page, OUT, "01-channel-participants-rail");

await page.getByTitle("Add participant").click();
await page.getByRole("button", { name: "Add Researcher to #general" }).click();
await page
  .getByRole("status")
  .filter({ hasText: "Researcher added to #general" })
  .waitFor({ timeout: 5_000 });
await page.waitForTimeout(250);
await shotPage(page, OUT, "02-channel-participant-added");
await page
  .getByRole("status")
  .filter({ hasText: "Researcher added to #general" })
  .click();

await page.getByRole("button", { name: "Remove Builder from channel" }).click();
await page
  .getByRole("status")
  .filter({ hasText: "Builder removed from #general" })
  .waitFor({ timeout: 5_000 });
await page.waitForTimeout(250);
await shotPage(page, OUT, "03-channel-participant-remove-undo");

console.log(`captured channel participants screenshots to ${OUT}`);
await browser.close();
