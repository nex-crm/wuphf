import {
  type KeyboardEvent,
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type ApprovalAuditEntry,
  getApprovalAuditByRequest,
} from "../../api/audit";
import {
  type AgentRequest,
  answerRequest,
  getRequests,
} from "../../api/client";
import {
  getInboxItems,
  postInboxCursor,
  type UnifiedInboxResponse,
} from "../../api/lifecycle";
import {
  fetchReviews,
  type ReviewItem,
  updateReviewState,
} from "../../api/notebook";
import type { InboxItem, InboxItemKind } from "../../lib/types/inbox";
import { SEV_ORDER, SEVERITY_TOKENS } from "../../lib/types/lifecycle";
import { useFallbackChannelSlug } from "../../routes/useCurrentRoute";
import { RequestItem } from "./RequestItem";

const IssueDocumentRoute = lazy(() =>
  import("./IssueDocumentRoute").then((m) => ({
    default: m.IssueDocumentRoute,
  })),
);
const DecisionPacketRoute = lazy(() =>
  import("./DecisionPacketRoute").then((m) => ({
    default: m.DecisionPacketRoute,
  })),
);

const ReviewDetail = lazy(() => import("../review/ReviewDetail"));

interface DecisionInboxProps {
  /** Optional initial items — used by tests + screenshot harness. */
  initialItems?: InboxItem[];
  /** Force a state for screenshot capture / E2E. */
  forceState?: "loading" | "empty" | "error";
  /** Override the row-open behavior (tests + harness). */
  onOpenItem?: (item: InboxItem) => void;
}

type InboxFilter =
  | "all"
  | "unread"
  | "decisions"
  | "requests"
  | "reviews"
  | "rejected";

const FILTER_ORDER: readonly InboxFilter[] = [
  "all",
  "unread",
  "decisions",
  "requests",
  "reviews",
  "rejected",
];

const FILTER_LABEL: Record<InboxFilter, string> = {
  all: "All",
  unread: "Unread",
  decisions: "Decisions",
  requests: "Requests",
  reviews: "Reviews",
  rejected: "Rejected",
};

// Inbox decision-state allowlist (Slice 8 fix): blocked tasks are
// INTENTIONALLY excluded. A blocked task by itself is not actionable
// by the human — they don't know what's blocking it or how to fix it.
// When the agent needs HUMAN input to unblock, it calls
// human_interview (which creates a separate "request" Inbox item the
// human CAN answer) — that's the only path that should surface in
// the Inbox. The blocked task itself stays in its state until the
// blocker resolves on the agent's side OR a human reads the linked
// question request. queued_behind_owner is also off-list for the
// same reason — it's a coordination signal, not a human decision.
const DECISION_STATES = new Set([
  "decision",
  "review",
  "changes_requested",
  "rejected",
]);

/**
 * v3 MVP — Inbox actionability invariant.
 *
 * The Inbox surface is a queue of items the operator must act on. If a
 * row cannot be approved / answered / merged / triaged by a human, it
 * does not belong here — it lives on its source surface (Issues, Wiki,
 * an agent's app surface, etc.) until it earns a decision moment.
 *
 * Rules (server may surface more items than the Inbox accepts — the
 * invariant lives on the client because the Inbox is a UX contract):
 *   - Reviews: always actionable (CEO approves / rejects / requests
 *     changes). Approved + archived reviews are excluded.
 *   - Requests: always actionable when blocking (they pause the
 *     conversation). Non-blocking requests are excluded — they ride
 *     along in the conversation.
 *   - Tasks: only states that explicitly need a human call surface
 *     here. Plain "open" / "intake" tasks live on the Issues surface.
 */
function isReviewActionable(item: InboxItem & { kind: "review" }): boolean {
  const state = (item.review.state ?? "").toLowerCase();
  return (
    state === "pending" ||
    state === "in-review" ||
    state === "changes-requested" ||
    state === "in_review" ||
    state === "changes_requested"
  );
}

function isRequestActionable(item: InboxItem & { kind: "request" }): boolean {
  // Requests in WUPHF are blocking by default — keep them all unless
  // the broker explicitly marks one non-blocking.
  return item.request.blocking !== false;
}

function isTaskActionable(item: InboxItem & { kind: "task" }): boolean {
  const state = (item.task?.state ?? "").toLowerCase();
  return DECISION_STATES.has(state);
}

function isInboxItemActionable(item: InboxItem): boolean {
  switch (item.kind) {
    case "task":
      return isTaskActionable(item);
    case "request":
      return isRequestActionable(item);
    case "review":
      return isReviewActionable(item);
    default: {
      const _exhaustive: never = item;
      void _exhaustive;
      return false;
    }
  }
}

function itemMatchesFilter(item: InboxItem, filter: InboxFilter): boolean {
  // The actionability invariant is pre-filtered upstream of this call —
  // every item reaching here is already known to need a human decision.
  if (filter === "all") return true;
  if (filter === "unread") return item.isUnread === true;
  if (filter === "requests") return item.kind === "request";
  if (filter === "reviews") return item.kind === "review";
  if (item.kind !== "task") return false;
  const state = item.task?.state ?? "";
  if (filter === "rejected") return state === "rejected";
  // "decisions" bucket — task states that need a human call.
  return DECISION_STATES.has(state);
}

const LOADING_TIMEOUT_MS = 500;

/**
 * Decision Inbox — Outlook-style email inbox. One row per attention
 * item (task / request / review). Click a row → detail in the right
 * pane. No agent grouping — every item is its own thread.
 *
 *   ┌── Item list ───┬── Detail pane ────────────────────────┐
 *   │ ● Mira  14:32  │ From: Mira   Type: decision          │
 *   │   Refactor...  │ Subject: Refactor agent-rail...      │
 *   │   2 critical   │                                       │
 *   │                │ task-2741 · decision · 14:32         │
 *   │   Ada   12:18  │                                       │
 *   │   Promoted...  │ Approve refactor of pill state...    │
 *   │                │                                       │
 *   │   Wren  11:04  │ [Approve] [Request changes] [Defer]   │
 *   │   Done. Han... │                                       │
 *   └────────────────┴───────────────────────────────────────┘
 */
export function DecisionInbox({
  initialItems,
  forceState,
  onOpenItem,
}: DecisionInboxProps) {
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [showLoadingText, setShowLoadingText] = useState(false);
  const [filter, setFilter] = useState<InboxFilter>("all");

  const seed: UnifiedInboxResponse | undefined = initialItems
    ? {
        items: initialItems,
        counts: {
          decisionRequired: 0,
          running: 0,
          blocked: 0,
          approvedToday: 0,
          unread: initialItems.filter((it) => it.isUnread === true).length,
        },
        refreshedAt: new Date().toISOString(),
      }
    : undefined;

  const query = useQuery<UnifiedInboxResponse>({
    queryKey: ["lifecycle", "inbox-items"],
    queryFn: () => getInboxItems("all"),
    initialData: seed,
    enabled: forceState === undefined,
    refetchInterval: 5_000,
    staleTime: 2_000,
  });

  const isLoading =
    forceState === "loading" || (query.isPending && !initialItems);
  useEffect(() => {
    if (!isLoading) {
      setShowLoadingText(false);
      return;
    }
    const t = window.setTimeout(
      () => setShowLoadingText(true),
      LOADING_TIMEOUT_MS,
    );
    return () => window.clearTimeout(t);
  }, [isLoading]);

  // Server may surface every lifecycle row to keep counts honest, but
  // the Inbox UX contract is: only show items the operator can act on.
  // We enforce that invariant here so every downstream pane (counts,
  // list, selection) respects the same definition of "actionable".
  const rawItems = useMemo(() => query.data?.items ?? [], [query.data]);
  const allItems = useMemo(
    () => rawItems.filter(isInboxItemActionable),
    [rawItems],
  );
  const items = useMemo(
    () => allItems.filter((it) => itemMatchesFilter(it, filter)),
    [allItems, filter],
  );
  const filterCounts = useMemo(() => {
    const counts: Record<InboxFilter, number> = {
      all: allItems.length,
      unread: 0,
      decisions: 0,
      requests: 0,
      reviews: 0,
      rejected: 0,
    };
    for (const it of allItems) {
      for (const f of FILTER_ORDER) {
        if (f === "all") continue;
        if (itemMatchesFilter(it, f)) counts[f] += 1;
      }
    }
    return counts;
  }, [allItems]);

  // Auto-select the top item on first mount + when selection gets
  // stale (e.g. approved item vanishes from the list).
  useEffect(() => {
    if (selectedKey === null && items.length > 0) {
      setSelectedKey(itemKey(items[0]));
    } else if (
      selectedKey !== null &&
      !items.some((it) => itemKey(it) === selectedKey) &&
      items.length > 0
    ) {
      setSelectedKey(itemKey(items[0]));
    }
  }, [items, selectedKey]);

  const listRef = useRef<HTMLUListElement | null>(null);

  const handleListKey = useCallback(
    (e: KeyboardEvent<HTMLUListElement>) => {
      if (items.length === 0) return;
      const idx = items.findIndex((it) => itemKey(it) === selectedKey);
      if (e.key === "ArrowDown") {
        e.preventDefault();
        const next = idx < 0 ? 0 : Math.min(idx + 1, items.length - 1);
        setSelectedKey(itemKey(items[next]));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        const next = idx <= 0 ? 0 : idx - 1;
        setSelectedKey(itemKey(items[next]));
      }
    },
    [items, selectedKey],
  );

  useEffect(() => {
    function global(e: globalThis.KeyboardEvent) {
      if (isShortcutBlockedFromTarget(e)) return;
      if (e.key === "Escape" && hasBackHistory()) {
        window.history.back();
      }
    }
    window.addEventListener("keydown", global);
    return () => window.removeEventListener("keydown", global);
  }, []);

  const selectedItem =
    selectedKey === null
      ? null
      : (items.find((it) => itemKey(it) === selectedKey) ?? null);

  if (isLoading) {
    return (
      <Shell>
        <ListPane filter={filter} counts={filterCounts} onFilter={setFilter}>
          {renderLoadingRows(showLoadingText)}
        </ListPane>
        <DetailEmptyPane message="Loading…" />
      </Shell>
    );
  }

  if (query.isError || forceState === "error") {
    return (
      <Shell>
        <ListPane filter={filter} counts={filterCounts} onFilter={setFilter}>
          <div className="inbox-error-banner" role="alert">
            <span className="banner-dot" aria-hidden="true" />
            <div className="body">Can't reach the broker.</div>
            <button
              type="button"
              className="retry"
              onClick={() => query.refetch()}
            >
              Retry
            </button>
          </div>
        </ListPane>
        <DetailEmptyPane message="Pick a thread on the left." />
      </Shell>
    );
  }

  if (items.length === 0 || forceState === "empty") {
    const noItemsAtAll = allItems.length === 0 || forceState === "empty";
    const headline = noItemsAtAll ? "Inbox zero." : "Nothing in this filter.";
    const body = noItemsAtAll
      ? "No decisions waiting on you. Agents are running; items show up here only when they need a human call."
      : "Switch to All to see other inbox items.";
    return (
      <Shell>
        <ListPane filter={filter} counts={filterCounts} onFilter={setFilter}>
          <div className="inbox-empty inbox-empty--inline">
            <h2>{headline}</h2>
            <p>{body}</p>
          </div>
        </ListPane>
        <DetailEmptyPane message={headline} />
      </Shell>
    );
  }

  return (
    <Shell>
      <ListPane filter={filter} counts={filterCounts} onFilter={setFilter}>
        <ul
          className="inbox-mail-list"
          aria-label="Inbox"
          ref={listRef}
          onKeyDown={handleListKey}
          style={{ listStyle: "none", margin: 0, padding: 0 }}
        >
          {items.map((item, idx) => {
            const key = itemKey(item);
            return (
              <li key={key}>
                <MailRow
                  item={item}
                  isSelected={key === selectedKey}
                  tabIndex={
                    key === selectedKey || (selectedKey === null && idx === 0)
                      ? 0
                      : -1
                  }
                  onSelect={(item) => {
                    // Focus-driven: track selection only. No side
                    // effects — arrow-key nav must not silently mark
                    // every passed-over row as read.
                    setSelectedKey(itemKey(item));
                  }}
                  onOpen={(item) => {
                    // User-initiated open (click / Enter / Space). Mark
                    // as read here so the cursor only advances on
                    // genuine intent, not keyboard hover.
                    setSelectedKey(itemKey(item));
                    onOpenItem?.(item);
                    if (item.isUnread === true) {
                      void postInboxCursor();
                    }
                  }}
                />
              </li>
            );
          })}
        </ul>
      </ListPane>
      {selectedItem ? (
        <DetailPane item={selectedItem} />
      ) : (
        <DetailEmptyPane message="Pick a thread on the left." />
      )}
    </Shell>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="inbox-shell inbox-shell--mail" data-testid="decision-inbox">
      {children}
    </div>
  );
}

function ListPane({
  children,
  filter,
  counts,
  onFilter,
}: {
  children: React.ReactNode;
  filter?: InboxFilter;
  counts?: Record<InboxFilter, number>;
  onFilter?: (next: InboxFilter) => void;
}) {
  return (
    <aside className="inbox-mail-pane" aria-label="Inbox">
      <header className="inbox-mail-header">
        <h1 className="inbox-mail-title">Inbox</h1>
      </header>
      {filter && counts && onFilter ? (
        // Toggle-button group, not ARIA tabs. The chips don't gate a
        // separate tabpanel — they re-filter the inbox list directly —
        // so role="tab"/"tablist" without aria-controls + arrow-key
        // navigation would be a half-implemented pattern. aria-pressed
        // gives screen readers the right "toggled" semantics.
        <div
          className="inbox-filter-bar"
          role="group"
          aria-label="Inbox filter"
          data-testid="inbox-filter-bar"
        >
          {FILTER_ORDER.map((f) => (
            <button
              key={f}
              type="button"
              className="inbox-filter-chip"
              aria-pressed={f === filter}
              onClick={() => onFilter(f)}
              data-testid={`inbox-filter-${f}`}
            >
              {FILTER_LABEL[f]}
              <span className="inbox-filter-chip-count">{counts[f]}</span>
            </button>
          ))}
        </div>
      ) : null}
      <div className="inbox-mail-scroll">{children}</div>
    </aside>
  );
}

function MailRow({
  item,
  isSelected,
  tabIndex,
  onSelect,
  onOpen,
}: {
  item: InboxItem;
  isSelected: boolean;
  tabIndex: number;
  onSelect: (item: InboxItem) => void;
  onOpen: (item: InboxItem) => void;
}) {
  const from = senderForItem(item);
  const subject = item.title;
  const snippet = snippetForItem(item);
  const elapsed = formatElapsed(item.createdAt);
  const kindLabel = kindShortLabel(item.kind);

  const isUnread = item.isUnread === true;
  return (
    <button
      type="button"
      className="inbox-mail-row"
      data-selected={isSelected ? "true" : "false"}
      data-kind={item.kind}
      data-unread={isUnread ? "true" : "false"}
      tabIndex={tabIndex}
      onClick={() => onOpen(item)}
      onFocus={() => onSelect(item)}
      aria-label={`${isUnread ? "Unread " : ""}Open ${kindLabel} from ${from}: ${subject}`}
    >
      <span
        className="inbox-mail-row-unread-dot"
        aria-hidden="true"
        data-visible={isUnread ? "true" : "false"}
      />
      <span className="inbox-mail-row-rail" aria-hidden="true" />
      <span className="inbox-mail-row-main">
        <span className="inbox-mail-row-line">
          <span className="inbox-mail-row-from">{from}</span>
          <time className="inbox-mail-row-time">{elapsed}</time>
        </span>
        <span className="inbox-mail-row-subject">
          {subject || "(no subject)"}
        </span>
        <span className="inbox-mail-row-snippet">
          <span className="inbox-mail-row-kind">{kindLabel}</span>
          {snippet ? " · " : ""}
          {snippet}
        </span>
      </span>
    </button>
  );
}

function DetailPane({ item }: { item: InboxItem }) {
  // Tasks render the Linear-style Issue document so the Inbox detail
  // pane matches the standalone /issues/$id view. The legacy
  // DecisionPacketRoute (file diffs / reviewer grades / spec sections)
  // was code-review-shaped and showed irrelevant blocks for non-code
  // Issues. The simpler IssueDocument surface (description + sub-issues
  // + comments) shows the actual context plus the same Approve / Close
  // / Reopen actions. Kept the legacy import below for direct routes
  // that may still want it.
  if (item.kind === "task") {
    const state = item.task?.state ?? "";
    const showFullPacket =
      state === "decision" ||
      state === "review" ||
      state === "changes_requested" ||
      state === "approved" ||
      state === "rejected" ||
      state === "blocked_on_pr_merge";
    if (showFullPacket) {
      void DecisionPacketRoute;
      return (
        <main
          className="inbox-detail-pane inbox-detail-pane--task"
          aria-label="Issue detail"
        >
          <Suspense
            fallback={
              <div className="inbox-detail-empty">
                <h2>Loading…</h2>
              </div>
            }
          >
            <IssueDocumentRoute issueId={item.taskId} />
          </Suspense>
        </main>
      );
    }
    // Non-decision-state tasks: TaskInfoBody is a light summary
    // with no title of its own, so keep DetailHeader.
    return (
      <main
        className="inbox-detail-pane inbox-detail-pane--mail"
        aria-label="Task detail"
      >
        <DetailHeader item={item} />
        <TaskInfoBody item={item} />
      </main>
    );
  }

  // Request + review embed components own their own title + meta,
  // so the inbox-level DetailHeader would duplicate them. Skip it
  // for those kinds; only render it for non-decision task states
  // (TaskInfoBody has no title of its own).
  return (
    <main
      className="inbox-detail-pane inbox-detail-pane--mail"
      aria-label="Thread detail"
    >
      {item.kind === "request" ? (
        <RequestBody item={item} />
      ) : (
        <ReviewBody item={item} />
      )}
    </main>
  );
}

function TaskInfoBody({
  item,
}: {
  item: Extract<InboxItem, { kind: "task" }>;
}) {
  const state = item.task?.state ?? "—";
  const sev = item.task?.severityCounts;
  const total = sev ? sev.critical + sev.major + sev.minor + sev.nitpick : 0;
  const stateLine = stateExplainer(state);
  return (
    <div className="inbox-mail-detail-body">
      <p className="inbox-mail-state-explainer">{stateLine}</p>
      <dl className="inbox-mail-detail-fields">
        <dt>Task ID</dt>
        <dd>
          <code>{item.taskId}</code>
        </dd>
        <dt>State</dt>
        <dd>
          <span className="inbox-action-card-pill">{state}</span>
        </dd>
        {item.task?.assignment ? (
          <>
            <dt>Assignment</dt>
            <dd>{item.task.assignment}</dd>
          </>
        ) : null}
        {total > 0 && sev ? (
          <>
            <dt>Reviewer grades</dt>
            <dd>
              <span className="inbox-action-card-severity">
                {sev.critical ? (
                  <span className="inbox-severity-dot inbox-severity-dot--critical">
                    {sev.critical} critical
                  </span>
                ) : null}
                {sev.major ? (
                  <span className="inbox-severity-dot inbox-severity-dot--major">
                    {sev.major} major
                  </span>
                ) : null}
                {sev.minor ? (
                  <span className="inbox-severity-dot inbox-severity-dot--minor">
                    {sev.minor} minor
                  </span>
                ) : null}
                {sev.nitpick ? (
                  <span className="inbox-severity-dot inbox-severity-dot--nitpick">
                    {sev.nitpick} nitpick
                  </span>
                ) : null}
              </span>
            </dd>
          </>
        ) : null}
      </dl>
    </div>
  );
}

function stateExplainer(state: string): string {
  switch (state) {
    case "intake":
      return "The intake agent is gathering the spec.";
    case "ready":
      return "Spec confirmed. The owner agent will start shortly.";
    case "running":
      return "The owner agent is working. Decision details surface once it transitions to review.";
    case "review":
      return "Reviewers are grading the work. Decision details surface once enough grades land.";
    case "blocked_on_pr_merge":
      return "The owner agent is paused. Resume to retry, or reject to drop the task.";
    case "changes_requested":
      return "You asked for changes. The owner agent is iterating.";
    case "approved":
      return "This task was approved. No further action needed.";
    case "rejected":
      return "This task was rejected. The work will not land. Downstream tasks stay blocked.";
    default:
      return "Decision details aren't available for this task yet.";
  }
}

function DetailHeader({ item }: { item: InboxItem }) {
  const from = senderForItem(item);
  return (
    <header className="inbox-mail-detail-header">
      <h1 className="inbox-mail-detail-subject">
        {item.title || "(no subject)"}
      </h1>
      <div className="inbox-mail-detail-meta">
        <span className="inbox-mail-detail-from">
          <strong>{from}</strong>
        </span>
        <span className="inbox-mail-detail-kind" data-kind={item.kind}>
          {kindShortLabel(item.kind)}
        </span>
        <time className="inbox-mail-detail-time">
          {formatFullTimestamp(item.createdAt)}
        </time>
      </div>
    </header>
  );
}

function RequestBody({
  item,
}: {
  item: Extract<InboxItem, { kind: "request" }>;
}) {
  const queryClient = useQueryClient();
  const channel = useFallbackChannelSlug();
  const [answerError, setAnswerError] = useState<string | null>(null);
  const requestsQuery = useQuery({
    queryKey: ["requests", channel],
    queryFn: () => getRequests(channel),
    refetchInterval: 5_000,
  });
  const allRequests: AgentRequest[] = requestsQuery.data?.requests ?? [];
  const fullRequest = allRequests.find((r) => r.id === item.requestId);
  const isPending =
    !!fullRequest &&
    (!fullRequest.status ||
      fullRequest.status === "open" ||
      fullRequest.status === "pending");

  // Audit trail loads only for answered requests. Hook MUST run on every
  // render (even when fullRequest is null) to keep React's hook order
  // stable across re-renders — React error #310 was firing because the
  // early returns below were skipping this useQuery on the first render
  // and then calling it after the request loaded. We pass a stable
  // sentinel key when there's no request yet and gate execution on
  // `enabled`.
  const auditTargetId = fullRequest?.id ?? "__none__";
  const auditQuery = useQuery({
    queryKey: ["approval-audit", "by-request", auditTargetId],
    queryFn: () => getApprovalAuditByRequest(auditTargetId),
    enabled: !!fullRequest && !isPending,
    refetchInterval: !!fullRequest && !isPending ? 5_000 : false,
  });
  const auditEntries: ApprovalAuditEntry[] = auditQuery.data ?? [];

  if (!fullRequest && requestsQuery.isPending) {
    return (
      <div className="inbox-mail-detail-body">
        <p className="inbox-mail-section-empty">Loading request…</p>
      </div>
    );
  }
  if (!fullRequest) {
    return (
      <div className="inbox-mail-detail-body">
        <p className="inbox-mail-section-empty">
          This request has been answered or is no longer active.
        </p>
      </div>
    );
  }

  const handleAnswer = async (choiceId: string, customText?: string) => {
    setAnswerError(null);
    try {
      await answerRequest(fullRequest.id, choiceId, customText);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["requests"] }),
        queryClient.invalidateQueries({ queryKey: ["lifecycle"] }),
      ]);
    } catch (err) {
      setAnswerError(
        err instanceof Error ? err.message : "Could not answer request.",
      );
    }
  };

  return (
    <div className="inbox-mail-detail-body inbox-mail-detail-body--embed">
      {answerError ? (
        <div className="inbox-error-banner" role="alert">
          <span className="banner-dot" aria-hidden="true" />
          <div className="body">{answerError}</div>
        </div>
      ) : null}
      <RequestItem
        request={fullRequest}
        isPending={isPending}
        onAnswer={(choiceId, customText) =>
          void handleAnswer(choiceId, customText)
        }
      />
      {!isPending && auditEntries.length > 0 ? (
        <ApprovalAuditTrail entries={auditEntries} />
      ) : null}
    </div>
  );
}

interface ApprovalAuditTrailProps {
  entries: ApprovalAuditEntry[];
}

function ApprovalAuditTrail({ entries }: ApprovalAuditTrailProps) {
  // Sort entries by created_at so the timeline reads chronologically. The
  // broker writes append-only but doesn't guarantee order on the wire.
  const sorted = [...entries].sort((a, b) =>
    (a.created_at ?? "").localeCompare(b.created_at ?? ""),
  );

  // Lead row: "Approved by you at <time>". Sourced from the first entry's
  // answered_at, which the broker records on the success and rejection
  // paths. Falls back to the entry's requested_at otherwise so the row is
  // never blank.
  const first = sorted[0];
  const answeredAt = first?.answered_at ?? first?.requested_at ?? "";

  return (
    <section className="inbox-trail" aria-label="Approval trail">
      <header className="inbox-trail-header">TRAIL</header>
      <ol className="inbox-trail-list">
        {answeredAt ? (
          <li className="inbox-trail-row">
            <span className="inbox-trail-actor">
              {leadVerbForOutcome(first?.outcome)} by you
            </span>
            <span className="inbox-trail-time">{formatTrailTime(answeredAt)}</span>
          </li>
        ) : null}
        {sorted.map((entry, idx) => (
          <ApprovalAuditTrailRow
            key={`${entry.approval_request_id}-${entry.outcome ?? ""}-${idx}`}
            entry={entry}
          />
        ))}
      </ol>
    </section>
  );
}

interface ApprovalAuditTrailRowProps {
  entry: ApprovalAuditEntry;
}

function ApprovalAuditTrailRow({ entry }: ApprovalAuditTrailRowProps) {
  const actor = entry.actor ? `@${entry.actor}` : "agent";
  const verb = trailVerbForOutcome(entry.outcome);
  const when = entry.executed_at ?? entry.created_at;
  const summary = entry.outcome_summary?.trim() ?? "";
  return (
    <li className="inbox-trail-row">
      <span className="inbox-trail-actor">
        → {actor} {verb}
      </span>
      {summary ? (
        <span className="inbox-trail-outcome"> — {summary}</span>
      ) : null}
      <span className="inbox-trail-time">{formatTrailTime(when)}</span>
    </li>
  );
}

// Lead-row verb (capitalized, for "<Verb> by you"). Mirrors
// trailVerbForOutcome but in the past-tense "I did this" voice rather
// than the agent's "→ @actor did this" voice.
function leadVerbForOutcome(outcome: string | undefined): string {
  switch (outcome) {
    case "rejected":
      return "Rejected";
    case "cancelled":
      return "Cancelled";
    case "timed_out":
      return "Timed out";
    case "executed_ok":
    case "executed_failed":
      return "Answered";
    default:
      return "Answered";
  }
}

function trailVerbForOutcome(outcome: string | undefined): string {
  switch (outcome) {
    case "executed_ok":
      return "executed";
    case "executed_failed":
      return "tried to execute (failed)";
    case "rejected":
      return "marked rejected";
    case "timed_out":
      return "timed out";
    case "cancelled":
      return "cancelled";
    default:
      return outcome ?? "updated";
  }
}

function formatTrailTime(value: string | undefined): string {
  if (!value) return "";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "";
  return parsed.toLocaleTimeString([], {
    hour: "numeric",
    minute: "2-digit",
  });
}

function ReviewBody({
  item,
}: {
  item: Extract<InboxItem, { kind: "review" }>;
}) {
  const queryClient = useQueryClient();
  const reviewsQuery = useQuery<ReviewItem[]>({
    queryKey: ["reviews"],
    queryFn: fetchReviews,
    refetchInterval: 15_000,
  });
  const review = (reviewsQuery.data ?? []).find((r) => r.id === item.reviewId);

  if (!review && reviewsQuery.isPending) {
    return (
      <div className="inbox-mail-detail-body">
        <p className="inbox-mail-section-empty">Loading review…</p>
      </div>
    );
  }
  if (!review) {
    return (
      <div className="inbox-mail-detail-body">
        <p className="inbox-mail-section-empty">
          This review is no longer active.
        </p>
      </div>
    );
  }

  return (
    <div className="inbox-mail-detail-body inbox-mail-detail-body--embed notebook-surface">
      <Suspense
        fallback={<p className="inbox-mail-section-empty">Loading review…</p>}
      >
        <ReviewDetail
          review={review}
          onClose={() => {
            /* No drawer close from inbox embed — selection lives in
               the inbox state. */
          }}
          onApprove={(id) => {
            void updateReviewState(id, "approved").then(() => {
              void queryClient.invalidateQueries({ queryKey: ["reviews"] });
              void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
            });
          }}
          onRequestChanges={(id) => {
            void updateReviewState(id, "changes-requested").then(() => {
              void queryClient.invalidateQueries({ queryKey: ["reviews"] });
              void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
            });
          }}
        />
      </Suspense>
    </div>
  );
}

function DetailEmptyPane({ message }: { message: string }) {
  return (
    <main className="inbox-detail-pane inbox-detail-pane--empty">
      <div className="inbox-detail-empty">
        <h2>Inbox</h2>
        <p>{message}</p>
      </div>
    </main>
  );
}

function renderLoadingRows(showText: boolean) {
  return (
    <>
      <div className="inbox-mail-list" aria-busy="true" aria-live="polite">
        {[0, 1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="inbox-mail-skeleton">
            <div className="inbox-skeleton-bar medium" />
            <div className="inbox-skeleton-bar short" />
          </div>
        ))}
      </div>
      {showText ? (
        <div className="inbox-loading-text" role="status">
          Loading inbox…
        </div>
      ) : null}
    </>
  );
}

function itemKey(item: InboxItem): string {
  switch (item.kind) {
    case "task":
      return `task:${item.taskId}`;
    case "request":
      return `request:${item.requestId}`;
    case "review":
      return `review:${item.reviewId}`;
  }
}

function senderForItem(item: InboxItem): string {
  // Prefer the explicit agent slug attribution from the broker.
  if (item.agentSlug && item.agentSlug !== "system") {
    return capitalizeName(item.agentSlug);
  }
  if (item.kind === "request")
    return capitalizeName(item.request.from || "owner");
  if (item.kind === "review") return capitalizeName(item.review.sourceSlug);
  return "Office";
}

function capitalizeName(slug: string): string {
  if (!slug) return slug;
  return slug
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((part) => part[0].toUpperCase() + part.slice(1))
    .join(" ");
}

function snippetForItem(item: InboxItem): string {
  switch (item.kind) {
    case "task":
      return item.task?.assignment ?? "";
    case "request":
      return item.request.question || "";
    case "review":
      return `${item.review.sourceSlug} → ${item.review.targetPath}`;
  }
}

function kindShortLabel(kind: InboxItemKind): string {
  switch (kind) {
    case "task":
      return "decision";
    case "request":
      return "request";
    case "review":
      return "review";
  }
}

function formatElapsed(iso?: string): string {
  if (!iso) return "";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  const diff = Date.now() - t.getTime();
  const m = Math.floor(diff / 60_000);
  if (m < 1) return "now";
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.floor(h / 24);
  if (d < 7) return `${d}d`;
  return t.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function formatFullTimestamp(iso?: string): string {
  if (!iso) return "";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  return t.toLocaleString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function isShortcutBlockedFromTarget(e: globalThis.KeyboardEvent): boolean {
  if (e.metaKey || e.ctrlKey || e.altKey) return true;
  const target = e.target as HTMLElement | null;
  if (!target) return false;
  return (
    target.tagName === "INPUT" ||
    target.tagName === "TEXTAREA" ||
    target.isContentEditable
  );
}

function hasBackHistory(): boolean {
  return typeof window !== "undefined" && window.history.length > 1;
}
