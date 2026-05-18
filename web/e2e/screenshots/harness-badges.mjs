// Capture the harness badges that show which LLM runtime each agent is on.
// PR #890 fixes two regressions:
//   1. `hermes-agent` and `openclaw-http` kinds fell through to the Claude
//      icon — now Hermes shows the caduceus on indigo, OpenClaw shows the
//      lobster on white.
//   2. The badges now use the projects' actual SVGs (favicon.svg /
//      acp_registry/icon.svg) embedded verbatim, not hand-redrawn glyphs.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh harness-badges <pr-number>

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

// One agent per provider kind so each badge variation is visible in the
// participant rail. Includes the existing claude-code/codex/opencode/openclaw
// kinds for visual comparison alongside the new hermes-agent and the
// openclaw-http alias.
const OFFICE_MEMBERS = [
  {
    slug: "ceo",
    name: "CEO",
    role: "Lead agent on claude-code",
    expertise: ["coordination"],
    status: "active",
    liveActivity: "Reviewing the launch plan",
    built_in: true,
    online: true,
    provider: { kind: "claude-code" },
  },
  {
    slug: "codex-test",
    name: "Codex Test",
    role: "Test agent on codex",
    expertise: ["test"],
    status: "idle",
    online: true,
    provider: { kind: "codex" },
  },
  {
    slug: "opencode-test",
    name: "Opencode Test",
    role: "Test agent on opencode",
    expertise: ["test"],
    status: "idle",
    online: true,
    provider: { kind: "opencode" },
  },
  {
    slug: "openclaw-test",
    name: "OpenClaw Test",
    role: "Test agent on openclaw-http",
    expertise: ["test"],
    status: "idle",
    online: true,
    provider: { kind: "openclaw-http" },
  },
  {
    slug: "hermes-test",
    name: "Hermes Test",
    role: "Test agent on hermes-agent",
    expertise: ["test"],
    status: "idle",
    online: true,
    provider: { kind: "hermes-agent" },
  },
];

const CHANNEL_MEMBERS = [
  ...OFFICE_MEMBERS,
  { slug: "human", name: "Human", role: "Viewer" },
];

const MESSAGES = [
  {
    id: "msg-1",
    from: "human",
    channel: "general",
    content:
      "Roll call — each of you, one sentence: which runtime are you on?",
    timestamp: "2026-05-17T20:00:00Z",
  },
  {
    id: "msg-2",
    from: "ceo",
    channel: "general",
    content: "CEO here, running on **Claude Sonnet 4.6**.",
    timestamp: "2026-05-17T20:00:30Z",
    reply_to: "msg-1",
  },
  {
    id: "msg-3",
    from: "codex-test",
    channel: "general",
    content: "@codex-test: I'm running on **GPT-5**.",
    timestamp: "2026-05-17T20:00:35Z",
    reply_to: "msg-1",
  },
  {
    id: "msg-4",
    from: "opencode-test",
    channel: "general",
    content: "@opencode-test: running through **Opencode**.",
    timestamp: "2026-05-17T20:00:40Z",
    reply_to: "msg-1",
  },
  {
    id: "msg-5",
    from: "openclaw-test",
    channel: "general",
    content:
      "@openclaw-test: I'm openclaw-test, running on **openai-codex/gpt-5** via the OpenClaw HTTP gateway.",
    timestamp: "2026-05-17T20:00:45Z",
    reply_to: "msg-1",
  },
  {
    id: "msg-6",
    from: "hermes-test",
    channel: "general",
    content:
      "@hermes-test here — running on **gpt-5.5** via Hermes Agent's OpenAI-compatible api_server.",
    timestamp: "2026-05-17T20:00:50Z",
    reply_to: "msg-1",
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
              description: "Primary coordination channel.",
              members: OFFICE_MEMBERS.map((m) => m.slug).concat("human"),
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
        body: JSON.stringify({ members: CHANNEL_MEMBERS }),
      }),
    );
    await ctx.route("**/api/messages*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ messages: MESSAGES }),
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
  afterFlipSelector: ".sidebar-agent-avatar",
  settleMs: 800,
});

// 1. Full channel view — sidebar avatars + message bubbles, each with its
//    own harness badge. This is the headline "all five kinds at once" shot.
await shotPage(page, OUT, "01-cross-provider-channel");

// 2. Sidebar zoom — agents-rail only, so the badge differences are easy to
//    compare at the resolution the reviewer sees them in the app.
await shotElement(
  page,
  ".sidebar-agents, .agents-rail, nav.sidebar",
  OUT,
  "02-sidebar-harness-badges",
).catch(async () => {
  // Selectors evolved across refactors; fall back to whichever container
  // actually wraps the agent avatars so the spec doesn't fail-stop here.
  await shotElement(page, "aside, .left-sidebar", OUT, "02-sidebar-harness-badges");
});

// 3. Message bubble zoom — the badge attaches to the avatar at message-
//    bubble scale too. Capture the cluster of bubble avatars so reviewers
//    can see the same badge on a different surface.
await shotElement(
  page,
  ".messages-list, .channel-messages, main",
  OUT,
  "03-message-bubble-badges",
);

console.log(`captured cross-provider harness badge screenshots to ${OUT}`);
await browser.close();
