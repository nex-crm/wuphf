/**
 * Decision Packet + Inbox mock fixtures.
 *
 * Lanes A/B/C/D/E (broker-side state machine, intake agent, packet
 * persistence, reviewer routing, indexed lifecycle lookup) are still in
 * flight. Until those merge, the web routes consume the deterministic
 * fixtures here so the UI can render fully populated states + state
 * coverage during build.
 *
 * After Lane A merges, `getInboxPayload` and `getDecisionPacket` swap
 * to the real `/api/tasks` endpoints — they already return the same
 * shape (`InboxPayload`, `DecisionPacket`) the routes consume.
 */

import type {
  DecisionPacket,
  InboxPayload,
  InboxRow,
  ReviewerGrade,
  Severity,
} from "../types/lifecycle";

const ZERO_COUNTS: Record<Severity, number> = {
  critical: 0,
  major: 0,
  minor: 0,
  nitpick: 0,
  skipped: 0,
};

function counts(
  partial: Partial<Record<Severity, number>>,
): Record<Severity, number> {
  return { ...ZERO_COUNTS, ...partial };
}

const NOW = "2026-05-09T22:00:00Z";

const ROW_REFACTOR_AGENT_RAIL: InboxRow = {
  taskId: "task-2741",
  title: "Refactor agent-rail event pill state machine",
  assignment:
    "Owner agent finished. Merge or request changes — owner committed broker_actor.go + 3 tests.",
  state: "decision",
  severityCounts: counts({ critical: 1, major: 2, minor: 4 }),
  lastChangedAt: "2026-05-09T21:52:00Z",
  elapsed: "8m",
  isUrgent: true,
};

const ROW_NOTEBOOK_REVIEW: InboxRow = {
  taskId: "task-2742",
  title: "Wiki article: notebook auto-writer review queue",
  assignment:
    "All reviewers green. Auto-merge eligible if you opt in — currently human-required.",
  state: "decision",
  severityCounts: counts({ minor: 3 }),
  lastChangedAt: "2026-05-09T21:58:00Z",
  elapsed: "2m",
  isUrgent: false,
};

const ROW_SEVERITY_TOKEN: InboxRow = {
  taskId: "task-2743",
  title: "Add severity tier styling to badge component",
  assignment:
    "Owner abandoned mid-run. Re-spawn or kill the task — 1 dead-end logged.",
  state: "blocked_on_pr_merge",
  severityCounts: counts({ major: 1 }),
  lastChangedAt: "2026-05-09T21:18:00Z",
  elapsed: "42m",
  isUrgent: false,
};

const ROW_RUNNING_INTAKE: InboxRow = {
  taskId: "task-2744",
  title: "Telegram dispatcher session ack on send",
  assignment: "Owner agent still working. ACK roundtrip in progress.",
  state: "running",
  severityCounts: counts({}),
  lastChangedAt: "2026-05-09T21:45:00Z",
  elapsed: "15m",
  isUrgent: false,
};

const ROW_RUNNING_REVIEW: InboxRow = {
  taskId: "task-2745",
  title: "Replace recall-loop temperature scheduling",
  assignment: "Reviewers grading. 1 of 3 submitted.",
  state: "review",
  severityCounts: counts({ nitpick: 1 }),
  lastChangedAt: "2026-05-09T21:50:00Z",
  elapsed: "10m",
  isUrgent: false,
};

const ROW_MERGED: InboxRow = {
  taskId: "task-2730",
  title: "Wiki freshness scoring uses neighbor recency",
  assignment: "Merged 32m ago.",
  state: "merged",
  severityCounts: counts({ minor: 1, nitpick: 2 }),
  lastChangedAt: "2026-05-09T21:28:00Z",
  elapsed: "32m",
  isUrgent: false,
};

const ROW_CHANGES_REQUESTED: InboxRow = {
  taskId: "task-2746",
  title: "Skill compile dedup gate raises threshold to 0.82",
  assignment: "Sent back yesterday. Owner agent is re-resuming.",
  state: "changes_requested",
  severityCounts: counts({ minor: 2 }),
  lastChangedAt: "2026-05-08T18:00:00Z",
  elapsed: "1d",
  isUrgent: false,
};

export const POPULATED_INBOX: InboxPayload = {
  rows: [
    ROW_REFACTOR_AGENT_RAIL,
    ROW_NOTEBOOK_REVIEW,
    ROW_SEVERITY_TOKEN,
    ROW_RUNNING_INTAKE,
    ROW_RUNNING_REVIEW,
    ROW_CHANGES_REQUESTED,
    ROW_MERGED,
  ],
  counts: {
    decisionRequired: 2,
    running: 7,
    blocked: 2,
    mergedToday: 11,
  },
  refreshedAt: NOW,
};

export const EMPTY_INBOX: InboxPayload = {
  rows: [],
  counts: {
    decisionRequired: 0,
    running: 7,
    blocked: 2,
    mergedToday: 11,
  },
  refreshedAt: NOW,
};

const GRADE_TESS_CRIT: ReviewerGrade = {
  reviewerSlug: "tess",
  severity: "critical",
  suggestion:
    "Watermark assumes monotonic event order — SSE replay can deliver out of order",
  reasoning:
    "If a stale event arrives after a fresher one, the state machine flips backward. Reproducible on broker restart with replay window. Suggest: track max(watermark) and discard older arrivals.",
  filePath: "internal/team/broker_actor.go",
  line: 182,
  submittedAt: "2026-05-09T21:58:00Z",
};

const GRADE_AVA_MAJOR: ReviewerGrade = {
  reviewerSlug: "ava",
  severity: "major",
  suggestion:
    "Stuck-state should survive a refactor like this — needs an explicit assertion",
  reasoning:
    'No test asserts that data-stuck="true" renders the border under reduced-motion. Easy regression risk.',
  filePath: "internal/team/broker_actor_test.go",
  submittedAt: "2026-05-09T21:57:00Z",
};

const GRADE_TESS_MAJOR_2: ReviewerGrade = {
  reviewerSlug: "tess",
  severity: "major",
  suggestion:
    "Halo duration token used as constant — not read from CSS variable",
  reasoning:
    "Hardcoded 600ms in JS. Should read from --bubble-halo-duration at runtime. Otherwise theme overrides break.",
  filePath: "internal/team/broker_actor.go",
  line: 41,
  submittedAt: "2026-05-09T21:58:00Z",
};

const GRADE_SAM_SKIPPED: ReviewerGrade = {
  reviewerSlug: "sam",
  severity: "skipped",
  suggestion: "Reviewer timed out",
  reasoning: "Process did not submit grade within 10-minute window.",
  submittedAt: "2026-05-09T21:52:00Z",
};

export const POPULATED_PACKET: DecisionPacket = {
  taskId: "task-2741",
  title: "Refactor agent-rail event pill state machine",
  lifecycleState: "decision",
  ownerSlug: "tess",
  worktreePath: ".worktrees/refactor-agent-pill-2741",
  createdAt: "2026-05-09T19:48:00Z",
  updatedAt: "2026-05-09T21:58:00Z",
  spec: {
    problem:
      "Replace the timer-driven state machine in broker_actor.go with one driven by SSE-event watermarks so the dim/idle transitions match what the user sees.",
    targetOutcome:
      "Agent-rail pill state derives from arrival watermarks rather than wall-clock timers, eliminating the three known race conditions.",
    acceptanceCriteria: [
      {
        statement: "Halo decay matches DESIGN.md token --bubble-halo-duration",
        done: true,
      },
      { statement: "Reduced-motion path snaps state instantly", done: true },
      {
        statement: "No agent rail row reflow during state change",
        done: true,
      },
      {
        statement: "Stuck-state border survives reduced-motion",
        done: true,
      },
      {
        statement: "Test for halo/holding/dim/idle/stuck transitions",
        done: true,
      },
      { statement: "E2E test for the full state lifecycle", done: false },
    ],
    assignment:
      "Owner agent committed the refactor. 1 critical, 2 major, 4 minor grades. Merge if you accept the critical, or send back with feedback.",
    constraints: ["Local-first only", "No new dependencies"],
    autoAssign: "tess",
    feedback: [],
  },
  sessionReport: {
    highlights:
      "SSE-driven watermarks now own the state machine. Eliminates 3 race conditions caught by the new test suite. Refactor surface is +412/−287 across 4 files; one new test file added.",
    topWins: [
      {
        delta: "−287 LOC",
        description:
          "Removed timer-based state machine in favor of event watermarks",
      },
      { delta: "+126 LOC", description: "New stateFromWatermark() resolver" },
      {
        delta: "+183 LOC",
        description: "5 unit tests for halo/holding/dim/idle/stuck transitions",
      },
    ],
    deadEnds: [
      {
        tried: "Single-watermark model",
        reason:
          "Couldn't express the holding-but-arrived case",
      },
      {
        tried: "Debouncing event arrivals at 100ms",
        reason: "Broke the new-event halo trigger",
      },
    ],
    metadata: {
      runtime: "2h12m",
      model: "claude-opus-4-7",
      tool_calls: "8",
    },
  },
  changedFiles: [
    {
      path: "internal/team/broker_actor.go",
      status: "modified",
      additions: 126,
      deletions: 287,
    },
    {
      path: "internal/team/broker_actor_test.go",
      status: "added",
      additions: 183,
      deletions: 0,
    },
    {
      path: "internal/team/broker_types.go",
      status: "modified",
      additions: 18,
      deletions: 0,
    },
    {
      path: "web/src/styles/agents.css",
      status: "modified",
      additions: 85,
      deletions: 0,
    },
  ],
  reviewerGrades: [
    GRADE_TESS_CRIT,
    GRADE_AVA_MAJOR,
    GRADE_TESS_MAJOR_2,
    GRADE_SAM_SKIPPED,
  ],
  dependencies: {
    parentTaskId: "",
    blockedOn: ["PR #791"],
  },
  subIssues: [
    {
      taskId: "task-2701",
      title: "Add severity color tokens",
      state: "merged",
    },
    { taskId: "task-2715", title: "Update DESIGN.md", state: "running" },
  ],
  reviewers: [
    { slug: "tess", isHuman: false, hasGraded: true },
    { slug: "ava", isHuman: false, hasGraded: true },
    { slug: "nazz", isHuman: true, hasGraded: false },
    { slug: "sam", isHuman: false, hasGraded: false },
  ],
  banners: [
    {
      kind: "reviewer_timeout",
      message:
        "Slot filled with skipped placeholder. Merge anyway, or rerequest with `wuphf task review task-2741 --rerequest sam`.",
      reviewerSlug: "sam",
      elapsed: "10m",
    },
  ],
  regeneratedFromMemory: false,
};

const PACKETS_BY_ID: Record<string, DecisionPacket> = {
  [POPULATED_PACKET.taskId]: POPULATED_PACKET,
  "task-2742": {
    ...POPULATED_PACKET,
    taskId: "task-2742",
    title: "Wiki article: notebook auto-writer review queue",
    lifecycleState: "decision",
    spec: {
      ...POPULATED_PACKET.spec,
      acceptanceCriteria: POPULATED_PACKET.spec.acceptanceCriteria.map(
        (ac) => ({ ...ac, done: true }),
      ),
      assignment:
        "All reviewers green. Auto-merge eligible if you opt in — currently human-required.",
    },
    reviewerGrades: [
      {
        reviewerSlug: "ava",
        severity: "minor",
        suggestion: "Wiki link uses absolute path; prefer wikilink form",
        reasoning:
          "[[Notebook auto-writer]] would render the same and survive reorgs.",
        filePath: "wiki/decisions/notebook-auto-writer.md",
        submittedAt: "2026-05-09T21:55:00Z",
      },
      {
        reviewerSlug: "tess",
        severity: "minor",
        suggestion: "Add a 'see also' to the source notebook",
        reasoning:
          "Helps the next reader trace where the wiki article came from.",
        filePath: "wiki/decisions/notebook-auto-writer.md",
        submittedAt: "2026-05-09T21:56:00Z",
      },
      {
        reviewerSlug: "nazz",
        severity: "minor",
        suggestion: "Add a date-stamp to the lede",
        reasoning: "Easier to scan when triaging older promotions.",
        submittedAt: "2026-05-09T21:57:00Z",
      },
    ],
    banners: [],
  },
};

/**
 * Mocked broker GET /api/tasks/inbox response.
 *
 * TODO(post-lane-a): wire to real /api/tasks endpoint. Lane A
 * (LifecycleState + indexed lookup) lands the indexed-list query.
 */
export async function getInboxPayloadMock(): Promise<InboxPayload> {
  return Promise.resolve(POPULATED_INBOX);
}

/**
 * Mocked broker GET /api/tasks/:id response.
 *
 * TODO(post-lane-c): wire to real /api/tasks/:id endpoint. Lane C
 * (Decision Packet model + persistence) lands the
 * `~/.wuphf/tasks/<id>/decision_packet.json` source.
 */
export async function getDecisionPacketMock(
  taskId: string,
): Promise<DecisionPacket> {
  const packet = PACKETS_BY_ID[taskId];
  if (packet) return Promise.resolve(packet);
  // Default to the canonical populated packet so deep-link demos still
  // render a coherent task view; the route layer will swap for real
  // broker fetch + 404 handling once Lane C lands.
  // structuredClone so nested arrays/objects don't share references with
  // POPULATED_PACKET — prevents mutation-based contamination across deep-
  // link demo invocations.
  return Promise.resolve({ ...structuredClone(POPULATED_PACKET), taskId });
}
