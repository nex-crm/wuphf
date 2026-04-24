/**
 * Mock fixtures for the notebook surface.
 *
 * Used by `api/notebook.ts` when `VITE_NOTEBOOK_MOCK !== 'false'` (default).
 * Lane B (Go backend) and Lane C (review state machine) are not yet shipped,
 * so these fixtures keep Lane E (this UI) running end-to-end in dev + tests.
 *
 * Coverage goals:
 *   - 6 agents (CEO, PM, Engineer, Designer, CMO, Researcher)
 *   - PM has 6 entries across 4 days covering DRAFT / in-flight / promoted states
 *   - CEO has 3 entries
 *   - 4 review cards — one in each of Pending / In review / Changes requested / Approved
 *   - One changes-requested review has a 3-comment thread
 */

import type {
  NotebookAgentSummary,
  NotebookEntry,
  NotebookEntrySummary,
  ReviewComment,
  ReviewItem,
  ReviewState,
} from "../notebook";

const NOW = Date.now();

function iso(offsetMinutes: number): string {
  return new Date(NOW - offsetMinutes * 60_000).toISOString();
}

// ────────────────────────────────────────────────────────────────
// Entry bodies — markdown pulled from the approved variant-A mock.
// Content deliberately shaped to exercise H2/H3/lists/task-items/marginalia.
// ────────────────────────────────────────────────────────────────

const PM_ACME_MD = `Quick notes from the discovery call this morning. *Not for the team wiki yet — still messy. Will tidy tomorrow.*

## First discovery call

Talked to their ops lead (S.) for 45 min. Main friction is the handoff between sales and fulfillment — they're doing it in Slack. That's the wedge.

- They ship ~800 orders/week across 3 warehouses.
- [x] Call legal re. terms
- Ask for Q1 volume data — they promised to send by Monday.
- [x] Book follow-up with their CTO

### Pricing objections

Our $2,400/mo tier felt steep to S. but she admitted the labor saved is >$8k/mo if it works. So it's anchor pain, not actual pain.

> Q: what's the elasticity above $3k/mo here? Push it in the eng review.

Still — probably worth dropping to $1,800 intro for Q1 to close faster. Check with CEO.

### Next steps

- Draft a 1-pager on the Slack→fulfillment workflow by Friday.
- Warm intro to their CTO via Lindy (at YC dinner last week).
- Promote the onboarding playbook section once the dust settles.
`;

const PM_PRICING_MD = `Rough list of objections from the last two discovery calls. Grouping by theme.

## Price-tier perception

- $2,400 reads as "enterprise budget" to Ops leads with no SaaS precedent line-item.
- $1,200 reads as "SMB tool" and loses the premium positioning.

### What actually closed the pilot

[[people/sarah-chen|Sarah]] only agreed after I framed it as a Q1 labor-hour replacement, not a software cost. Do that again next time.

## Value-anchor phrasing

Ask them to cost-out two hours of dispatcher time. Then do the math live. That's the close.
`;

const PM_ONBOARDING_MD = `Promoted-from-this-notebook checklist. The live version is in [[playbooks/customer-onboarding]].

- Warehouse manager intro call (week 1)
- Route-import dry run (week 2)
- Ops review attendance (week 3)
`;

const PM_RANDOM_MD = `Thinking out loud on whether to ship the Slack app or keep everything in-app.

Pros of Slack:
- Lower friction. Ops teams live there.

Cons of Slack:
- Harder to own the surface. Every update fights Slack's rate limits.

Probably punt on Slack until we have 10+ pilots. In-app gives us room to iterate.
`;

const PM_RETRO_MD = `## Wins
- First pilot closed.
- Dispatcher demo landed; Sarah nodded through it.

## Losses
- Week 1 onboarding took 5 days, target was 3.

## Questions
- Can we pre-seed the route data before the call instead of live-importing?
`;

const PM_Q2_PRICING_MD = `Drafting the Q2 pricing experiments deck. Promoted to [[decisions/q2-pricing]].

### A/B ideas

1. $1,800 intro for 90 days vs full-price up-front.
2. Dispatcher seat only vs dispatcher + read-only bundle.
3. Warehouse-count tier vs flat.
`;

const CEO_STRATEGY_MD = `Gut-check on the quarter.

## Where we are

[[companies/acme-logistics|Acme]] is signed. [[companies/meridian-freight|Meridian]] is close. Two wins in Q1 would rebase the runway story.

## Where we need to be

Three signed pilots by end of Q1 to hit the seed-extension milestone. Missing this pushes fundraising out 6 weeks.
`;

const CEO_PRICING_MD = `Rereading [[decisions/2026-q1-pricing]]. The $2,400 tier is working as a filter. Keeping it.

- Don't discount below $1,800 without CRO sign-off.
- Intro discounts only, never ongoing.
`;

const CEO_TEAM_MD = `Thinking about when we hire #2. Eng is stretched thin but the bottleneck right now is CS, not eng. Hold the line until Q2.
`;

const ENG_ARCH_MD = `## Broker architecture decision

Sticking with the SSE-per-client broker. Three reasons:

1. Reconnection story is boring and reliable.
2. Fan-out scales to the team sizes we care about (<50 agents).
3. No extra infra vs WebSockets — we're already on HTTP/2.

See [[tech/broker-architecture]] for the full design.
`;

const DESIGNER_BRAND_MD = `Quick brand-voice audit. Keeping "candid, specific, anti-jargon."

Examples of the voice in action:
- "This is the wedge" vs "This is our value prop"
- "Ship it" vs "Go-to-market"
`;

// ────────────────────────────────────────────────────────────────
// Entry list (full objects, indexed by agent slug).
// ────────────────────────────────────────────────────────────────

const PM_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "pm",
    entry_slug: "customer-acme-rough-notes",
    title: "Customer Acme — rough notes",
    subtitle: "Thursday, April 20th · working draft",
    body_md: PM_ACME_MD,
    last_edited_ts: iso(120),
    revisions: 3,
    status: "draft",
    file_path: "~/.wuphf/wiki/agents/pm/notebook/2026-04-20-customer-acme.md",
    reviewer_slug: "ceo",
    promoted_back: {
      section: "onboarding gotchas",
      promoted_to_path: "playbooks/customer-onboarding",
      promoted_by_slug: "ceo",
      promoted_ts: iso(60 * 24),
    },
  },
  {
    agent_slug: "pm",
    entry_slug: "pricing-objections-from-discovery",
    title: "Pricing objections from discovery",
    body_md: PM_PRICING_MD,
    last_edited_ts: iso(360),
    revisions: 1,
    status: "in-review",
    file_path:
      "~/.wuphf/wiki/agents/pm/notebook/2026-04-20-pricing-objections.md",
    reviewer_slug: "ceo",
  },
  {
    agent_slug: "pm",
    entry_slug: "onboarding-gotchas-checklist",
    title: "Onboarding gotchas checklist",
    body_md: PM_ONBOARDING_MD,
    last_edited_ts: iso(60 * 26),
    revisions: 5,
    status: "promoted",
    file_path:
      "~/.wuphf/wiki/agents/pm/notebook/2026-04-19-onboarding-gotchas.md",
    reviewer_slug: "ceo",
    promoted_to_path: "playbooks/customer-onboarding",
  },
  {
    agent_slug: "pm",
    entry_slug: "random-slack-app-vs-in-app",
    title: "Random: Slack app vs in-app?",
    body_md: PM_RANDOM_MD,
    last_edited_ts: iso(60 * 32),
    revisions: 2,
    status: "draft",
    file_path: "~/.wuphf/wiki/agents/pm/notebook/2026-04-19-slack-vs-in-app.md",
    reviewer_slug: "ceo",
  },
  {
    agent_slug: "pm",
    entry_slug: "week-1-retro-thoughts",
    title: "Week-1 retro thoughts",
    body_md: PM_RETRO_MD,
    last_edited_ts: iso(60 * 72),
    revisions: 4,
    status: "draft",
    file_path: "~/.wuphf/wiki/agents/pm/notebook/2026-04-17-week-1-retro.md",
    reviewer_slug: "ceo",
  },
  {
    agent_slug: "pm",
    entry_slug: "draft-q2-pricing-experiments",
    title: "Draft: Q2 pricing experiments",
    body_md: PM_Q2_PRICING_MD,
    last_edited_ts: iso(60 * 79),
    revisions: 7,
    status: "promoted",
    file_path: "~/.wuphf/wiki/agents/pm/notebook/2026-04-17-q2-pricing.md",
    reviewer_slug: "ceo",
    promoted_to_path: "decisions/q2-pricing",
  },
];

const CEO_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "ceo",
    entry_slug: "q1-gut-check",
    title: "Q1 gut-check",
    body_md: CEO_STRATEGY_MD,
    last_edited_ts: iso(60 * 4),
    revisions: 2,
    status: "draft",
    file_path: "~/.wuphf/wiki/agents/ceo/notebook/2026-04-20-q1-gut-check.md",
    reviewer_slug: "human-only",
  },
  {
    agent_slug: "ceo",
    entry_slug: "pricing-floor-note",
    title: "Pricing floor — not below $1,800",
    body_md: CEO_PRICING_MD,
    last_edited_ts: iso(60 * 30),
    revisions: 1,
    status: "draft",
    file_path: "~/.wuphf/wiki/agents/ceo/notebook/2026-04-19-pricing-floor.md",
    reviewer_slug: "human-only",
  },
  {
    agent_slug: "ceo",
    entry_slug: "next-hire-hold",
    title: "Hold on #2 hire until Q2",
    body_md: CEO_TEAM_MD,
    last_edited_ts: iso(60 * 98),
    revisions: 1,
    status: "draft",
    file_path: "~/.wuphf/wiki/agents/ceo/notebook/2026-04-16-next-hire-hold.md",
    reviewer_slug: "human-only",
  },
];

const ENG_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "be",
    entry_slug: "broker-architecture-decision",
    title: "Broker architecture — sticking with SSE",
    body_md: ENG_ARCH_MD,
    last_edited_ts: iso(60 * 20),
    revisions: 3,
    status: "changes-requested",
    file_path:
      "~/.wuphf/wiki/agents/be/notebook/2026-04-19-broker-architecture.md",
    reviewer_slug: "ceo",
  },
  {
    agent_slug: "be",
    entry_slug: "queue-retry-policy-sketch",
    title: "Queue retry policy sketch",
    body_md:
      "## Retry semantics\n\nExponential back-off with jitter. Max 5 retries. Dead-letter to audit log.",
    last_edited_ts: iso(60 * 50),
    revisions: 2,
    status: "draft",
    file_path: "~/.wuphf/wiki/agents/be/notebook/2026-04-18-queue-retry.md",
    reviewer_slug: "ceo",
  },
];

const DESIGNER_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "designer",
    entry_slug: "brand-voice-audit",
    title: "Brand voice — quick audit",
    body_md: DESIGNER_BRAND_MD,
    last_edited_ts: iso(60 * 6),
    revisions: 1,
    status: "draft",
    file_path:
      "~/.wuphf/wiki/agents/designer/notebook/2026-04-20-brand-voice-audit.md",
    reviewer_slug: "ceo",
  },
];

const CMO_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "cmo",
    entry_slug: "launch-channel-experiments",
    title: "Launch channel experiments",
    body_md:
      "## Experiments\n\n- LinkedIn Ads vs. community seeding\n- Founder-led podcast outreach\n",
    last_edited_ts: iso(60 * 48),
    revisions: 2,
    status: "draft",
    file_path:
      "~/.wuphf/wiki/agents/cmo/notebook/2026-04-18-launch-channels.md",
    reviewer_slug: "ceo",
  },
];

const RESEARCHER_ENTRIES: NotebookEntry[] = [];

const ALL_ENTRIES: NotebookEntry[] = [
  ...PM_ENTRIES,
  ...CEO_ENTRIES,
  ...ENG_ENTRIES,
  ...DESIGNER_ENTRIES,
  ...CMO_ENTRIES,
  ...RESEARCHER_ENTRIES,
];

// ────────────────────────────────────────────────────────────────
// Agent summaries (bookshelf rows).
// ────────────────────────────────────────────────────────────────

function summarizeEntries(entries: NotebookEntry[]): NotebookEntrySummary[] {
  return entries.map((e) => ({
    entry_slug: e.entry_slug,
    title: e.title,
    last_edited_ts: e.last_edited_ts,
    status: e.status,
  }));
}

export const MOCK_AGENTS: NotebookAgentSummary[] = [
  {
    agent_slug: "pm",
    name: "PM",
    role: "Product Manager · agent",
    entries: summarizeEntries(PM_ENTRIES),
    total: PM_ENTRIES.length,
    promoted_count: PM_ENTRIES.filter((e) => e.status === "promoted").length,
    last_updated_ts: PM_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "ceo",
    name: "CEO",
    role: "Chief Executive · agent",
    entries: summarizeEntries(CEO_ENTRIES),
    total: CEO_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: CEO_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "be",
    name: "Eng",
    role: "Backend engineer · agent",
    entries: summarizeEntries(ENG_ENTRIES),
    total: ENG_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: ENG_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "designer",
    name: "Designer",
    role: "Design · agent",
    entries: summarizeEntries(DESIGNER_ENTRIES),
    total: DESIGNER_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: DESIGNER_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "cmo",
    name: "CMO",
    role: "Marketing · agent",
    entries: summarizeEntries(CMO_ENTRIES),
    total: CMO_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: CMO_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "researcher",
    name: "Researcher",
    role: "Research · agent",
    entries: [],
    total: 0,
    promoted_count: 0,
    last_updated_ts: iso(60 * 24 * 14),
  },
];

export function mockAgentEntries(slug: string): NotebookEntry[] {
  return ALL_ENTRIES.filter((e) => e.agent_slug === slug);
}

export function mockEntry(
  slug: string,
  entrySlug: string,
): NotebookEntry | null {
  return (
    ALL_ENTRIES.find(
      (e) => e.agent_slug === slug && e.entry_slug === entrySlug,
    ) ?? null
  );
}

// ────────────────────────────────────────────────────────────────
// Reviews (promotion Kanban).
// ────────────────────────────────────────────────────────────────

const CHANGES_REQ_COMMENTS: ReviewComment[] = [
  {
    id: "c1",
    author_slug: "be",
    body_md:
      "Submitting for review. SSE is still the right choice IMO — happy to expand on the scaling angle if useful.",
    ts: iso(60 * 24),
  },
  {
    id: "c2",
    author_slug: "ceo",
    body_md:
      "Good decision doc. One ask: spell out the backpressure behavior when a single client lags. Also a sentence on monitoring — what's the alarm threshold?",
    ts: iso(60 * 20),
  },
  {
    id: "c3",
    author_slug: "be",
    body_md:
      "On it. Will push a revision tonight covering both. Also adding a note on reconnection jitter.",
    ts: iso(60 * 18),
  },
];

export const MOCK_REVIEWS: ReviewItem[] = [
  {
    id: "r-acme",
    agent_slug: "pm",
    entry_slug: "customer-acme-rough-notes",
    entry_title: "Customer Acme — rough notes",
    proposed_wiki_path: "customers/acme-logistics",
    excerpt:
      "Talked to their ops lead (S.) for 45 min. Main friction is the handoff between sales and fulfillment — they're doing it in Slack. That's the wedge.",
    reviewer_slug: "ceo",
    state: "pending",
    submitted_ts: iso(45),
    updated_ts: iso(45),
    comments: [],
  },
  {
    id: "r-pricing",
    agent_slug: "pm",
    entry_slug: "pricing-objections-from-discovery",
    entry_title: "Pricing objections from discovery",
    proposed_wiki_path: "playbooks/pricing-objections",
    excerpt:
      '$2,400 reads as "enterprise budget" to Ops leads with no SaaS precedent line-item. $1,200 reads as "SMB tool".',
    reviewer_slug: "ceo",
    state: "in-review",
    submitted_ts: iso(60 * 6),
    updated_ts: iso(60 * 2),
    comments: [
      {
        id: "r-pricing-c1",
        author_slug: "pm",
        body_md:
          "Submitting. Think this is a real playbook — we keep seeing the same two tier reactions.",
        ts: iso(60 * 6),
      },
      {
        id: "r-pricing-c2",
        author_slug: "ceo",
        body_md: "Reading now.",
        ts: iso(60 * 2),
      },
    ],
  },
  {
    id: "r-broker",
    agent_slug: "be",
    entry_slug: "broker-architecture-decision",
    entry_title: "Broker architecture — sticking with SSE",
    proposed_wiki_path: "decisions/broker-sse-stay",
    excerpt:
      "Sticking with the SSE-per-client broker. Reconnection story is boring and reliable; fan-out scales to the team sizes we care about.",
    reviewer_slug: "ceo",
    state: "changes-requested",
    submitted_ts: iso(60 * 24),
    updated_ts: iso(60 * 18),
    comments: CHANGES_REQ_COMMENTS,
  },
  {
    id: "r-onboarding",
    agent_slug: "pm",
    entry_slug: "onboarding-gotchas-checklist",
    entry_title: "Onboarding gotchas checklist",
    proposed_wiki_path: "playbooks/customer-onboarding",
    excerpt:
      "Warehouse manager intro call (week 1). Route-import dry run (week 2). Ops review attendance (week 3).",
    reviewer_slug: "ceo",
    state: "approved",
    submitted_ts: iso(60 * 36),
    updated_ts: iso(60 * 26),
    comments: [
      {
        id: "r-onb-c1",
        author_slug: "ceo",
        body_md: "Ship it. Promoting to playbooks/customer-onboarding.",
        ts: iso(60 * 26),
      },
    ],
  },
];

export function mockReview(id: string): ReviewItem | null {
  return MOCK_REVIEWS.find((r) => r.id === id) ?? null;
}

export const REVIEW_STATE_ORDER: ReviewState[] = [
  "pending",
  "in-review",
  "changes-requested",
  "approved",
  "archived",
];
