/**
 * Opt-in mock fixtures for the notebook surface.
 *
 * These are intentionally use-case agnostic. They exercise draft, review, and
 * promoted states with operating notes any agent can benefit from: handoffs,
 * decisions, evidence, blockers, and retros.
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

const HANDOFF_MD = `# Reliable handoffs

Working draft for making handoffs easier to reuse.

## Context packet

Every handoff should include:

- The current goal in one sentence.
- The files, tools, or systems already touched.
- The last verified result.
- The exact next action, owner, and blocker.

## Open questions

- Should the next owner continue in the same thread or start a focused task?
- Is there a review gate, or can the next owner ship directly?
`;

const DECISION_MD = `# Decision records that age well

## Minimum shape

- Decision
- Reason
- Date
- Owner
- Revisit trigger

## Note

Do not record a preference as a decision until the team has actually acted on it.
`;

const EVIDENCE_MD = `# Evidence quality checklist

Before a result is promoted, check:

- The source is named.
- The command or tool result is reproducible.
- Any uncertainty is explicit.
- The note says what changed, not just what was inspected.
`;

const BLOCKER_MD = `# Blocker escalation notes

## Pattern

When blocked, separate the missing input from the proposed path forward.

Good blocker reports answer:

1. What is blocked?
2. What exact input is missing?
3. What work can proceed without that input?
`;

const REVIEW_MD = `# Review feedback loop

## Reviewer checklist

- Is the claim durable enough for the wiki?
- Does the target path match the article shape?
- Is the source notebook entry preserved?
- Are follow-up changes concrete?
`;

const RETRO_MD = `# End-of-turn retro

## Keep

- State the outcome before the implementation detail.
- Link the worktree or artifact that changed.

## Change

- Avoid "done" language until the durable state or file actually exists.
`;

const RUNBOOK_MD = `# Command verification notes

## Before sharing a command

- Run the narrowest command that proves the behavior.
- Capture the important output.
- Mention anything not run.
`;

const CATALOG_MD = `# Notebook catalog hygiene

Use titles that will still make sense next week. Prefer:

- "Review feedback loop"
- "Decision record shape"
- "Handoff checklist"

Avoid titles that only make sense inside the current chat.
`;

const PLANNER_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "planner",
    entry_slug: "reliable-handoffs",
    title: "Reliable handoffs",
    subtitle: "Working draft",
    body_md: HANDOFF_MD,
    last_edited_ts: iso(90),
    revisions: 3,
    status: "draft",
    file_path:
      "~/.wuphf/wiki/agents/planner/notebook/2026-04-20-reliable-handoffs.md",
    reviewer_slug: "reviewer",
  },
  {
    agent_slug: "planner",
    entry_slug: "decision-records-that-age-well",
    title: "Decision records that age well",
    body_md: DECISION_MD,
    last_edited_ts: iso(240),
    revisions: 2,
    status: "in-review",
    file_path:
      "~/.wuphf/wiki/agents/planner/notebook/2026-04-20-decision-records.md",
    reviewer_slug: "reviewer",
  },
  {
    agent_slug: "planner",
    entry_slug: "evidence-quality-checklist",
    title: "Evidence quality checklist",
    body_md: EVIDENCE_MD,
    last_edited_ts: iso(60 * 28),
    revisions: 4,
    status: "promoted",
    file_path:
      "~/.wuphf/wiki/agents/planner/notebook/2026-04-19-evidence-quality.md",
    reviewer_slug: "reviewer",
    promoted_to_path: "team/playbooks/evidence-quality.md",
  },
];

const CEO_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "ceo",
    entry_slug: "blocker-escalation-notes",
    title: "Blocker escalation notes",
    body_md: BLOCKER_MD,
    last_edited_ts: iso(60 * 6),
    revisions: 1,
    status: "draft",
    file_path:
      "~/.wuphf/wiki/agents/ceo/notebook/2026-04-20-blocker-escalation.md",
    reviewer_slug: "human-only",
  },
  {
    agent_slug: "ceo",
    entry_slug: "end-of-turn-retro",
    title: "End-of-turn retro",
    body_md: RETRO_MD,
    last_edited_ts: iso(60 * 42),
    revisions: 2,
    status: "draft",
    file_path:
      "~/.wuphf/wiki/agents/ceo/notebook/2026-04-19-end-of-turn-retro.md",
    reviewer_slug: "human-only",
  },
];

const BUILDER_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "builder",
    entry_slug: "command-verification-notes",
    title: "Command verification notes",
    body_md: RUNBOOK_MD,
    last_edited_ts: iso(60 * 18),
    revisions: 2,
    status: "changes-requested",
    file_path:
      "~/.wuphf/wiki/agents/builder/notebook/2026-04-19-command-verification.md",
    reviewer_slug: "reviewer",
  },
];

const REVIEWER_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "reviewer",
    entry_slug: "review-feedback-loop",
    title: "Review feedback loop",
    body_md: REVIEW_MD,
    last_edited_ts: iso(60 * 8),
    revisions: 1,
    status: "draft",
    file_path:
      "~/.wuphf/wiki/agents/reviewer/notebook/2026-04-20-review-feedback-loop.md",
    reviewer_slug: "ceo",
  },
];

const OPERATOR_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "operator",
    entry_slug: "notebook-catalog-hygiene",
    title: "Notebook catalog hygiene",
    body_md: CATALOG_MD,
    last_edited_ts: iso(60 * 36),
    revisions: 1,
    status: "draft",
    file_path:
      "~/.wuphf/wiki/agents/operator/notebook/2026-04-18-catalog-hygiene.md",
    reviewer_slug: "reviewer",
  },
];

const RESEARCHER_ENTRIES: NotebookEntry[] = [];

const ALL_ENTRIES: NotebookEntry[] = [
  ...PLANNER_ENTRIES,
  ...CEO_ENTRIES,
  ...BUILDER_ENTRIES,
  ...REVIEWER_ENTRIES,
  ...OPERATOR_ENTRIES,
  ...RESEARCHER_ENTRIES,
];

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
    agent_slug: "planner",
    name: "Planner",
    role: "Planning agent",
    entries: summarizeEntries(PLANNER_ENTRIES),
    total: PLANNER_ENTRIES.length,
    promoted_count: PLANNER_ENTRIES.filter((e) => e.status === "promoted")
      .length,
    last_updated_ts: PLANNER_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "ceo",
    name: "CEO",
    role: "Team lead",
    entries: summarizeEntries(CEO_ENTRIES),
    total: CEO_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: CEO_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "builder",
    name: "Builder",
    role: "Implementation agent",
    entries: summarizeEntries(BUILDER_ENTRIES),
    total: BUILDER_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: BUILDER_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "reviewer",
    name: "Reviewer",
    role: "Review agent",
    entries: summarizeEntries(REVIEWER_ENTRIES),
    total: REVIEWER_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: REVIEWER_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "operator",
    name: "Operator",
    role: "Operations agent",
    entries: summarizeEntries(OPERATOR_ENTRIES),
    total: OPERATOR_ENTRIES.length,
    promoted_count: 0,
    last_updated_ts: OPERATOR_ENTRIES[0]?.last_edited_ts ?? iso(9999),
  },
  {
    agent_slug: "researcher",
    name: "Researcher",
    role: "Research agent",
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

const CHANGES_REQ_COMMENTS: ReviewComment[] = [
  {
    id: "c1",
    author_slug: "builder",
    body_md:
      "Submitting for review. The command checklist is useful, but I may have overfit it to local shell work.",
    ts: iso(60 * 18),
  },
  {
    id: "c2",
    author_slug: "reviewer",
    body_md:
      "Keep the checklist, but make it tool-agnostic: terminal commands, external actions, and UI checks all need verification notes.",
    ts: iso(60 * 12),
  },
];

export const MOCK_REVIEWS: ReviewItem[] = [
  {
    id: "r-handoffs",
    agent_slug: "planner",
    entry_slug: "reliable-handoffs",
    entry_title: "Reliable handoffs",
    proposed_wiki_path: "team/playbooks/reliable-handoffs.md",
    excerpt:
      "Every handoff should include the current goal, touched systems, last verified result, and exact next action.",
    reviewer_slug: "reviewer",
    state: "pending",
    submitted_ts: iso(45),
    updated_ts: iso(45),
    comments: [],
  },
  {
    id: "r-decisions",
    agent_slug: "planner",
    entry_slug: "decision-records-that-age-well",
    entry_title: "Decision records that age well",
    proposed_wiki_path: "team/playbooks/decision-records.md",
    excerpt:
      "Decision, reason, date, owner, and revisit trigger are enough for the next agent to understand why a call was made.",
    reviewer_slug: "reviewer",
    state: "in-review",
    submitted_ts: iso(60 * 5),
    updated_ts: iso(60 * 2),
    comments: [
      {
        id: "r-decisions-c1",
        author_slug: "planner",
        body_md:
          "Submitting because we keep losing the reason behind final calls.",
        ts: iso(60 * 5),
      },
    ],
  },
  {
    id: "r-commands",
    agent_slug: "builder",
    entry_slug: "command-verification-notes",
    entry_title: "Command verification notes",
    proposed_wiki_path: "team/playbooks/verification-notes.md",
    excerpt:
      "Run the narrowest command that proves behavior, capture important output, and say what was not run.",
    reviewer_slug: "reviewer",
    state: "changes-requested",
    submitted_ts: iso(60 * 18),
    updated_ts: iso(60 * 12),
    comments: CHANGES_REQ_COMMENTS,
  },
  {
    id: "r-evidence",
    agent_slug: "planner",
    entry_slug: "evidence-quality-checklist",
    entry_title: "Evidence quality checklist",
    proposed_wiki_path: "team/playbooks/evidence-quality.md",
    excerpt:
      "Before promotion, the source should be named, reproducible, explicit about uncertainty, and clear about what changed.",
    reviewer_slug: "reviewer",
    state: "approved",
    submitted_ts: iso(60 * 36),
    updated_ts: iso(60 * 28),
    comments: [
      {
        id: "r-evidence-c1",
        author_slug: "reviewer",
        body_md: "Approved. This belongs in the shared wiki.",
        ts: iso(60 * 28),
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
