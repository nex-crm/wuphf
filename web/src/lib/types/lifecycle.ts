/**
 * Decision Inbox + Decision Packet — Lane G TS types
 *
 * Mirrors the Go shapes from the multi-agent control loop design
 * (feat/multi-agent-harness, design doc 2026-05-09). The Go side is
 * Lane A/C; until those merge the web routes consume mocked fixtures
 * matching this shape, then will swap to the real `/api/tasks/*`
 * surface without prop changes.
 *
 * String unions (rather than enum) are deliberate — they survive JSON
 * wire serialization unchanged and produce a compile error on typos
 * the same way the Go `LifecycleState`/`Severity` typed-constant pattern
 * does on the broker side.
 */

/** Lifecycle position of a task. Source of truth on the broker (`teamTask.LifecycleState`). */
export type LifecycleState =
  | "drafting"
  | "planning"
  | "intake"
  | "ready"
  | "running"
  | "review"
  | "decision"
  | "blocked_on_pr_merge"
  | "queued_behind_owner"
  | "changes_requested"
  | "approved"
  | "rejected"
  | "archived";

/**
 * Runtime membership for LifecycleState, exhaustive by construction: a
 * `Record<LifecycleState, true>` fails to compile if a union member is
 * missing, so `isLifecycleState` can never silently omit a state (a prior
 * hand-maintained copy had dropped `queued_behind_owner`). Lets wire-shape
 * `string` values — `Task.lifecycle_state`, card payload `lifecycle_state` —
 * be narrowed to the typed union.
 */
const ALL_LIFECYCLE_STATES: Record<LifecycleState, true> = {
  drafting: true,
  planning: true,
  intake: true,
  ready: true,
  running: true,
  review: true,
  decision: true,
  blocked_on_pr_merge: true,
  queued_behind_owner: true,
  changes_requested: true,
  approved: true,
  rejected: true,
  archived: true,
};

export function isLifecycleState(value: unknown): value is LifecycleState {
  return (
    typeof value === "string" && Object.hasOwn(ALL_LIFECYCLE_STATES, value)
  );
}

/**
 * User-facing board stage. The board groups the granular
 * `LifecycleState` values into a smaller set of columns it derives in
 * TypeScript via `stageForState`. The `scheduled` stage is the one
 * exception — it is fed by routines (the scheduler), not by any
 * lifecycle_state, so `stageForState` never returns it.
 */
export type LifecycleStage =
  | "scheduled"
  | "backlog"
  | "in_progress"
  | "blocked"
  | "needs_human"
  | "done"
  | "archive";

/** Left-to-right column order for the task board. */
export const STAGE_ORDER: readonly LifecycleStage[] = [
  "scheduled",
  "backlog",
  "in_progress",
  "blocked",
  "needs_human",
  "done",
  "archive",
];

/** Column header label per stage. */
export const STAGE_LABELS: Record<LifecycleStage, string> = {
  scheduled: "Scheduled Tasks",
  backlog: "Backlog",
  in_progress: "In progress",
  blocked: "Blocked",
  needs_human: "Needs human input",
  done: "Done",
  archive: "Archive",
};

/**
 * Project a granular lifecycle_state onto the board stage it belongs in.
 *
 * Never returns "scheduled" — that column is fed by routines, not
 * lifecycle_state. Any unknown / unmapped value falls back to "backlog"
 * so a newer broker state still lands somewhere readable instead of
 * being dropped.
 */
export function stageForState(s: LifecycleState): LifecycleStage {
  switch (s) {
    case "drafting":
    case "intake":
    case "ready":
      return "backlog";
    case "planning":
    case "running":
    case "review":
    case "changes_requested":
      return "in_progress";
    case "blocked_on_pr_merge":
    case "queued_behind_owner":
      return "blocked";
    case "decision":
      return "needs_human";
    case "approved":
      return "done";
    case "archived":
    case "rejected":
      return "archive";
    default:
      return "backlog";
  }
}

/** Severity tier on a reviewer grade. CodeRabbit-shaped. */
export type Severity = "critical" | "major" | "minor" | "nitpick" | "skipped";

/** Filter buckets shown on the Decision Inbox. */
export type InboxFilter =
  | "decision_required"
  | "running"
  | "blocked"
  | "approved"
  | "unread";

/**
 * One acceptance-criterion checkbox. Toggled by the owner agent during
 * `running`; humans never toggle these directly.
 */
export interface ACItem {
  statement: string;
  done: boolean;
}

/**
 * Feedback appended on `changes_requested` re-entry. Each entry is one
 * round-trip of human-to-agent redirection.
 */
export interface FeedbackItem {
  appendedAt: string; // RFC3339
  author: string; // human owner slug
  body: string;
}

/** Spec produced by the intake agent and refined during the run. */
export interface Spec {
  problem: string;
  targetOutcome: string;
  acceptanceCriteria: ACItem[];
  assignment: string;
  constraints: string[];
  autoAssign: string;
  feedback: FeedbackItem[];
}

/** One item in the "what worked" list within a session report. */
export interface Win {
  delta: string; // e.g. "+126 LOC", "−287 LOC"
  description: string;
}

/** One item in the "what didn't work" dead-ends list.
 *
 * Wire shape mirrors the Go broker's `team.DeadEnd` struct verbatim:
 * `tried` is the path the agent attempted, `reason` is why it was
 * abandoned. The Decision Packet view renders these in the "What I
 * tried that didn't work (dead ends)" panel.
 */
export interface DeadEnd {
  tried: string;
  reason: string;
}

/** Owner agent's self-authored session report. */
export interface SessionReport {
  highlights: string;
  topWins: Win[];
  deadEnds: DeadEnd[];
  metadata: Record<string, string>;
}

/** Per-file diff summary for the Decision Packet.
 *
 * Wire shape mirrors the Go broker's `team.DiffSummary` struct
 * verbatim. `status` and `renamedFrom` are omitempty on the wire so
 * existing Lane E rows that omit them decode cleanly. `isNew` from
 * earlier iterations is derived as `status === "added"` at render time
 * rather than persisted as a separate boolean.
 */
export interface DiffSummary {
  path: string;
  status?: string;
  additions: number;
  deletions: number;
  renamedFrom?: string;
}

/** One reviewer's grade on the artifact. */
export interface ReviewerGrade {
  reviewerSlug: string;
  severity: Severity;
  suggestion: string;
  reasoning: string;
  filePath?: string;
  line?: number;
  submittedAt: string; // RFC3339
}

/** Typed dependency context for the Decision Packet sidebar. */
export interface Dependencies {
  parentTaskId: string;
  blockedOn: string[]; // task IDs or PR identifiers
}

/**
 * Lifecycle-error banner kinds. Used by the Decision Packet view to
 * render the right banner without parsing free-text errors.
 */
export type PacketBannerKind =
  | "reviewer_timeout"
  | "persistence_error"
  | "missing_packet";

export interface PacketBanner {
  kind: PacketBannerKind;
  message: string;
  /** Optional reviewer slug when kind === "reviewer_timeout". */
  reviewerSlug?: string;
  /** Optional elapsed time string for timeout banners. */
  elapsed?: string;
}

/** Minimal sub-issue / parent-link surface for the left context column. */
export interface SubIssue {
  taskId: string;
  title: string;
  state: LifecycleState;
}

/** Reviewer-set summary shown in the left column. */
export interface ReviewerSummary {
  slug: string;
  isHuman: boolean;
  hasGraded: boolean;
}

/**
 * One row of the Decision Inbox. Aggregates the fields the row needs
 * without forcing a full-packet fetch — reduces the inbox-list payload
 * substantially and matches the indexed-lookup shape on the broker.
 */
export interface InboxRow {
  taskId: string;
  title: string;
  assignment: string;
  state: LifecycleState;
  /** Aggregate severity counts for the chip on the row. */
  severityCounts: Record<Severity, number>;
  /** ISO datetime — when the task last changed state. */
  lastChangedAt: string;
  /** Convenience pre-formatted elapsed string ("8m", "2h") for the row meta. */
  elapsed: string;
  /** True when elapsed should render in red (decision sitting >5m). */
  isUrgent: boolean;
  /**
   * Typed-blocker task IDs from teamTask.BlockedOn. Empty for tasks
   * that are not blocked; populated for blocked_on_pr_merge and any
   * other state the broker carries a dependency on.
   */
  blockedOn?: string[];
  /**
   * Free-text reason the broker attached to the task. For blocked
   * tasks this is the actual "why" (agent timeout, manual block).
   * Truncated by the broker for inbox payload size.
   */
  details?: string;
  /** RFC3339 timestamp of the task's last mutation. */
  updatedAt?: string;
}

/**
 * Inbox sidebar group counts. The default landing filter is
 * `decision_required` (exception-only) per the design doc.
 */
export interface InboxCounts {
  decisionRequired: number;
  running: number;
  blocked: number;
  approvedToday: number;
  /**
   * Number of items whose latest activity post-dates the caller's
   * InboxCursor.LastSeenAt. Drives the "Unread" sidebar pill and the
   * top-bar attention badge.
   */
  unread: number;
}

/** Top-level inbox payload returned by the (mocked, soon-real) API. */
export interface InboxPayload {
  rows: InboxRow[];
  counts: InboxCounts;
  /** ISO datetime — last successful refresh, used by the error banner. */
  refreshedAt: string;
}

/**
 * Full Decision Packet for the `/task/:id` view.
 *
 * Field order intentionally matches the visual reading flow on the
 * center column: meta → spec → AC → session report → diff → grades.
 */
export interface DecisionPacket {
  taskId: string;
  /**
   * Slug of the task's own channel (channel-per-task model). Not part of the
   * persisted packet — the GET /tasks/{id} response carries it on the `task`
   * snapshot, and the API layer lifts it onto the packet so the discussion
   * empty state can link to the conversation.
   */
  channel?: string;
  title: string;
  lifecycleState: LifecycleState;
  ownerSlug: string;
  worktreePath: string;
  createdAt: string;
  updatedAt: string;
  spec: Spec;
  sessionReport: SessionReport;
  changedFiles: DiffSummary[];
  reviewerGrades: ReviewerGrade[];
  dependencies: Dependencies;
  /** Sub-issues children of this task. */
  subIssues: SubIssue[];
  /** Set of agent + human reviewers, for the left column summary. */
  reviewers: ReviewerSummary[];
  /** Banners surfaced at the top of the packet (timeout, persistence, etc.). */
  banners: PacketBanner[];
  /** True when the packet was rebuilt from in-memory state (corrupted JSON). */
  regeneratedFromMemory: boolean;
}

/** State pill -> token names. Used by both Inbox row + Packet meta. */
export const STATE_PILL_TOKENS: Record<
  LifecycleState,
  { bg: string; text: string; label: string }
> = {
  /**
   * drafting: pre-Intake mode where agents can comment but not dispatch.
   * Uses brand-accent tokens (--accent-bg / --accent) to signal
   * "needs human attention" — distinct from intake/ready (--bg-row-active)
   * which use a neutral palette.
   * Design review decision 2026-05-17: locked.
   */
  drafting: {
    bg: "var(--accent-bg)",
    text: "var(--accent)",
    label: "drafting",
  },
  /**
   * planning: Plan mode (Phase 5) — the owner is writing a plan for human
   * approval before executing. Uses the accent tokens like drafting because it
   * is also pre-execution and awaits a human Approve & Start.
   */
  planning: {
    bg: "var(--accent-bg)",
    text: "var(--accent)",
    label: "planning",
  },
  intake: {
    bg: "var(--bg-row-active)",
    text: "var(--text-secondary)",
    label: "intake",
  },
  ready: {
    bg: "var(--bg-row-active)",
    text: "var(--text-secondary)",
    label: "ready",
  },
  running: {
    bg: "var(--cyan-200)",
    text: "var(--cyan-500)",
    label: "running",
  },
  review: {
    bg: "var(--cyan-200)",
    text: "var(--cyan-500)",
    label: "review",
  },
  decision: {
    bg: "var(--success-200)",
    text: "var(--success-500)",
    label: "decision",
  },
  blocked_on_pr_merge: {
    bg: "var(--warning-200)",
    text: "var(--warning-500)",
    label: "blocked",
  },
  queued_behind_owner: {
    bg: "var(--warning-200)",
    text: "var(--warning-500)",
    label: "queued",
  },
  changes_requested: {
    bg: "var(--bg-row-active)",
    text: "var(--warning-500)",
    label: "changes requested",
  },
  approved: {
    bg: "var(--bg-row-active)",
    text: "var(--text-tertiary)",
    label: "approved",
  },
  rejected: {
    bg: "var(--danger-200, var(--warning-200))",
    text: "var(--danger-500, var(--warning-500))",
    label: "rejected",
  },
  /**
   * archived: terminal, muted. Lands in the board's Archive column
   * alongside rejected. Styled neutral (no accent / no alarm color) to
   * read as "filed away, no longer in flight".
   */
  archived: {
    bg: "var(--bg-row-active)",
    text: "var(--text-tertiary)",
    label: "archived",
  },
};

/**
 * Severity tier color tokens. Read by SeverityGradeCard + InboxRow chip.
 *
 * Yellow AA decision (option b): we add `--yellow-aa-200/--yellow-aa-500`
 * tokens in `multi-agent-harness.css` (verified ≥4.5:1 against
 * `--bg-card` in both nex + nex-dark) instead of using `--warning-*` for
 * minor (which collapses with `major`'s orange) or the failing
 * `--yellow-500/--yellow-200` pair. See lifecycle.css for the values.
 */
export const SEVERITY_TOKENS: Record<
  Severity,
  {
    border: string;
    pillBg: string;
    pillText: string;
    bg: string;
    label: string;
    /** CSS modifier class for the dot chip (e.g. "crit", "maj"). */
    cssClass: string;
  }
> = {
  critical: {
    border: "var(--error-500)",
    pillBg: "var(--error-500)",
    pillText: "#ffffff",
    bg: "var(--error-200)",
    label: "critical",
    cssClass: "crit",
  },
  major: {
    border: "var(--warning-500)",
    pillBg: "var(--warning-500)",
    pillText: "#ffffff",
    bg: "var(--bg-card)",
    label: "major",
    cssClass: "maj",
  },
  minor: {
    border: "var(--yellow-aa-500)",
    pillBg: "var(--yellow-aa-500)",
    pillText: "var(--yellow-aa-on-pill)",
    bg: "var(--bg-card)",
    label: "minor",
    cssClass: "min",
  },
  nitpick: {
    border: "var(--cyan-500)",
    pillBg: "var(--cyan-500)",
    pillText: "#ffffff",
    bg: "var(--bg-card)",
    label: "nitpick",
    cssClass: "nit",
  },
  skipped: {
    border: "var(--text-tertiary)",
    pillBg: "var(--bg-row-active)",
    pillText: "var(--text-secondary)",
    bg: "var(--bg-card)",
    label: "skipped",
    cssClass: "skp",
  },
};

/** Display order for non-skipped severity tiers in summary chips. */
export const SEV_ORDER: ReadonlyArray<Exclude<Severity, "skipped">> = [
  "critical",
  "major",
  "minor",
  "nitpick",
];

export const INBOX_FILTERS: ReadonlyArray<{
  id: InboxFilter;
  label: string;
  countKey: keyof InboxCounts;
}> = [
  {
    id: "decision_required",
    label: "Needs decision",
    countKey: "decisionRequired",
  },
  { id: "running", label: "Running", countKey: "running" },
  { id: "blocked", label: "Blocked", countKey: "blocked" },
  { id: "approved", label: "Approved", countKey: "approvedToday" },
];

/** Map filter -> states that belong in that bucket. */
export const FILTER_TO_STATES: Record<
  InboxFilter,
  ReadonlyArray<LifecycleState>
> = {
  decision_required: ["decision"],
  // drafting is surfaced in the issues route, not the inbox; include it in
  // running so inbox rows with this state still land somewhere readable.
  running: ["drafting", "planning", "intake", "ready", "running", "review"],
  blocked: [
    "blocked_on_pr_merge",
    "queued_behind_owner",
    "changes_requested",
    "rejected",
  ],
  approved: ["approved"],
  // Unread is post-filtered against the actor's cursor on the server,
  // not against a fixed state set. Leave the state list empty so any
  // call site that walks states for "unread" produces no false matches.
  unread: [],
};
