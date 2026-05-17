// Capture the conversation view with a long message list to verify the
// scrolling fix. Without the .conversation-shell / .conversation-chat
// CSS, the message feed overflows below the viewport, the composer is
// pushed off-screen, and ChannelParticipants stacks below the composer.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh conversation-scroll-fix <pr-number>

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

const MEMBERS = [
  { slug: "ceo", name: "CEO", role: "Lead agent", online: true },
  { slug: "builder", name: "Builder", role: "Engineering", online: true },
  { slug: "design", name: "Designer", role: "Product design", online: false },
  { slug: "research", name: "Researcher", role: "Research", online: false },
  { slug: "human", name: "Human", role: "Viewer" },
];

const messages = Array.from({ length: 30 }, (_, i) => ({
  id: `msg-${i + 1}`,
  from: i % 3 === 0 ? "ceo" : i % 3 === 1 ? "builder" : "design",
  channel: "general",
  content: `Message ${i + 1}: lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`,
  timestamp: `2026-05-${String((i % 28) + 1).padStart(2, "0")}T12:00:00Z`,
}));

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1440, height: 900 },
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
          members: MEMBERS,
          meta: { humanHasPosted: true },
        }),
      }),
    );
    await ctx.route("**/api/members*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ members: MEMBERS }),
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
  },
});

await bootShell(page, {
  afterFlipSelector: ".conversation-shell",
  settleMs: 700,
});
await shotPage(page, OUT, "01-conversation-scroll-fix");

console.log(`captured conversation-scroll-fix screenshots to ${OUT}`);
await browser.close();
