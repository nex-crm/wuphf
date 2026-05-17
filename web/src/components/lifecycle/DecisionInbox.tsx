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
  type AgentRequest,
  answerRequest,
  getRequests,
} from "../../api/client";
import { getInboxItems, type UnifiedInboxResponse } from "../../api/lifecycle";
import {
  fetchReviews,
  type ReviewItem,
  updateReviewState,
} from "../../api/notebook";
import type { InboxItem, InboxItemKind } from "../../lib/types/inbox";
import {
  SEV_ORDER,
  SEVERITY_TOKENS,
} from "../../lib/types/lifecycle";
import { useFallbackChannelSlug } from "../../routes/useCurrentRoute";
import { RequestItem } from "./RequestItem";

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

  const seed: UnifiedInboxResponse | undefined = initialItems
    ? {
        items: initialItems,
        counts: {
          decisionRequired: 0,
          running: 0,
          blocked: 0,
          approvedToday: 0,
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

  const items = useMemo(() => query.data?.items ?? [], [query.data]);

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
        <ListPane>{renderLoadingRows(showLoadingText)}</ListPane>
        <DetailEmptyPane message="Loading…" />
      </Shell>
    );
  }

  if (query.isError || forceState === "error") {
    return (
      <Shell>
        <ListPane>
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
    return (
      <Shell>
        <ListPane>
          <div className="inbox-empty inbox-empty--inline">
            <h2>Inbox zero.</h2>
            <p>
              Nothing waiting on you. Agents will email you when they need
              something.
            </p>
          </div>
        </ListPane>
        <DetailEmptyPane message="Inbox zero." />
      </Shell>
    );
  }

  return (
    <Shell>
      <ListPane>
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
                    setSelectedKey(itemKey(item));
                    onOpenItem?.(item);
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

function ListPane({ children }: { children: React.ReactNode }) {
  return (
    <aside className="inbox-mail-pane" aria-label="Inbox">
      <header className="inbox-mail-header">
        <h1 className="inbox-mail-title">Inbox</h1>
      </header>
      <div className="inbox-mail-scroll">{children}</div>
    </aside>
  );
}

function MailRow({
  item,
  isSelected,
  tabIndex,
  onSelect,
}: {
  item: InboxItem;
  isSelected: boolean;
  tabIndex: number;
  onSelect: (item: InboxItem) => void;
}) {
  const from = senderForItem(item);
  const subject = item.title;
  const snippet = snippetForItem(item);
  const elapsed = formatElapsed(item.createdAt);
  const kindLabel = kindShortLabel(item.kind);

  return (
    <button
      type="button"
      className="inbox-mail-row"
      data-selected={isSelected ? "true" : "false"}
      data-kind={item.kind}
      tabIndex={tabIndex}
      onClick={() => onSelect(item)}
      onFocus={() => onSelect(item)}
      aria-label={`Open ${kindLabel} from ${from}: ${subject}`}
    >
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
  // Tasks in decision / review / changes_requested / approved state
  // render the full DecisionPacketRoute (3-pane PR-style UI with
  // discussion thread, comment composer, and action sidebar). Other
  // states (intake / ready / running / blocked_on_pr_merge) render a
  // lightweight task summary because they have no packet yet.
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
      return (
        <main
          className="inbox-detail-pane inbox-detail-pane--task"
          aria-label="Decision detail"
        >
          <Suspense
            fallback={
              <div className="inbox-detail-empty">
                <h2>Loading…</h2>
              </div>
            }
          >
            <DecisionPacketRoute taskId={item.taskId} />
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
      return "Waiting on an upstream PR merge before this task can land.";
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
  const requestsQuery = useQuery({
    queryKey: ["requests", channel],
    queryFn: () => getRequests(channel),
    refetchInterval: 5_000,
  });
  const allRequests: AgentRequest[] = requestsQuery.data?.requests ?? [];
  const fullRequest = allRequests.find((r) => r.id === item.requestId);

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
  const isPending =
    !fullRequest.status ||
    fullRequest.status === "open" ||
    fullRequest.status === "pending";

  return (
    <div className="inbox-mail-detail-body inbox-mail-detail-body--embed">
      <RequestItem
        request={fullRequest}
        isPending={isPending}
        onAnswer={(choiceId) => {
          void answerRequest(fullRequest.id, choiceId).then(() => {
            void queryClient.invalidateQueries({ queryKey: ["requests"] });
            void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
          });
        }}
      />
    </div>
  );
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
