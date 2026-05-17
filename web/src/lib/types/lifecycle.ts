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
  | "intake"
  | "ready"
  | "running"
  | "review"
  | "decision"
  | "blocked_on_pr_merge"
  | "changes_requested"
  | "approved"
  | "rejected";

/** Severity tier on a reviewer grade. CodeRabbit-shaped. */
export type Severity = "critical" | "major" | "minor" | "nitpick" | "skipped";

/** Filter buckets shown on the Decision Inbox. */
export type InboxFilter =
  | "decision_required"
  | "running"
  | "blocked"
  | "approved";

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
  running: ["intake", "ready", "running", "review"],
  blocked: ["blocked_on_pr_merge", "changes_requested", "rejected"],
  approved: ["approved"],
};
