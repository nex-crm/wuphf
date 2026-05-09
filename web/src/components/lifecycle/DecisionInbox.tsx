import {
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";

import { getInboxPayload } from "../../api/lifecycle";
import { POPULATED_INBOX } from "../../lib/mocks/decisionPackets";
import {
  FILTER_TO_STATES,
  INBOX_FILTERS,
  type InboxFilter,
  type InboxPayload,
  type InboxRow as InboxRowType,
} from "../../lib/types/lifecycle";
import { InboxRow } from "./InboxRow";

interface DecisionInboxProps {
  /** Optional initial payload — used by tests + screenshot harness to bypass the query. */
  initialPayload?: InboxPayload;
  /** Optional initial filter override. Defaults to `decision_required`. */
  initialFilter?: InboxFilter;
  /** Force a state for screenshot capture / E2E. */
  forceState?: "loading" | "empty" | "error" | "partial" | "populated";
  /** Navigate to /task/:id on row activation. Defaults to TanStack Router. */
  onOpenTask?: (taskId: string) => void;
}

const DEFAULT_FILTER: InboxFilter = "decision_required";
const LOADING_TIMEOUT_MS = 500;

/**
 * Decision Inbox — `/inbox` route container. Owns:
 *  - Fetch loop (mocked until Lane A merges; SSE follow-up wired post-merge).
 *  - Filter tabs + sidebar group counts.
 *  - All 5 interaction states (loading / empty / error / partial / populated).
 *  - Keyboard nav (↑/↓ row select, Enter open, / focus filter, Esc back).
 *
 * The component is route-agnostic: navigation callback is injected so
 * tests + the screenshot harness can stub it without spinning up the
 * router.
 */
export function DecisionInbox({
  initialPayload,
  initialFilter = DEFAULT_FILTER,
  forceState,
  onOpenTask,
}: DecisionInboxProps) {
  const [filter, setFilter] = useState<InboxFilter>(initialFilter);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [showLoadingText, setShowLoadingText] = useState(false);
  const filterRef = useRef<HTMLButtonElement | null>(null);

  const query = useQuery<InboxPayload>({
    queryKey: ["lifecycle", "inbox"],
    queryFn: getInboxPayload,
    initialData: initialPayload,
    enabled: forceState === undefined,
    refetchInterval: 5_000, // TODO(post-lane-b): swap to SSE push.
    staleTime: 2_000,
  });

  // Skeleton for <500ms, then "Loading inbox..." text fallback per the
  // design doc. Effect re-fires on each fresh fetch so the timing
  // applies the same way the user sees it.
  const isLoading =
    forceState === "loading" || (query.isPending && !initialPayload);
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

  const payload = query.data ?? POPULATED_INBOX;
  const filteredRows = useMemo(
    () => filterRows(payload.rows, filter),
    [payload.rows, filter],
  );
  const { counts } = payload;

  // Default-filter swap target for the "partial" empty state. If we're
  // already on the default and got zero matches, treat this as empty.
  const fallbackFilter: InboxFilter =
    filter === DEFAULT_FILTER ? "running" : DEFAULT_FILTER;
  const fallbackCount =
    counts[
      INBOX_FILTERS.find((f) => f.id === fallbackFilter)?.countKey ??
        "decisionRequired"
    ];

  const handleOpen = useCallback(
    (taskId: string) => {
      if (onOpenTask) {
        onOpenTask(taskId);
        return;
      }
      if (typeof window !== "undefined") {
        window.location.hash = `#/task/${taskId}`;
      }
    },
    [onOpenTask],
  );

  // Keyboard navigation on the row list.
  const handleListKey = useCallback(
    (e: KeyboardEvent<HTMLUListElement>) => {
      if (filteredRows.length === 0) return;
      const idx = filteredRows.findIndex((r) => r.taskId === selectedTaskId);
      if (e.key === "ArrowDown") {
        e.preventDefault();
        const next = idx < 0 ? 0 : Math.min(idx + 1, filteredRows.length - 1);
        setSelectedTaskId(filteredRows[next].taskId);
        focusRow(filteredRows[next].taskId);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        const next = idx <= 0 ? 0 : idx - 1;
        setSelectedTaskId(filteredRows[next].taskId);
        focusRow(filteredRows[next].taskId);
      } else if (e.key === "Enter" && idx >= 0) {
        e.preventDefault();
        handleOpen(filteredRows[idx].taskId);
      }
    },
    [filteredRows, selectedTaskId, handleOpen],
  );

  // Global keys: `/` focuses the filter tabs, Esc returns to dashboard.
  useEffect(() => {
    function global(e: globalThis.KeyboardEvent) {
      if (isShortcutBlockedFromTarget(e)) return;
      if (e.key === "/") {
        e.preventDefault();
        filterRef.current?.focus();
      } else if (e.key === "Escape" && hasBackHistory()) {
        // Match the spec — Esc returns to the dashboard. The router-level
        // back-nav is owned by the route layer; here we no-op when no
        // history entry exists (tests / standalone harness).
        window.history.back();
      }
    }
    window.addEventListener("keydown", global);
    return () => window.removeEventListener("keydown", global);
  }, []);

  if (forceState === "loading" || isLoading) {
    return (
      <InboxFrame counts={counts}>{renderLoading(showLoadingText)}</InboxFrame>
    );
  }

  if (forceState === "error" || query.isError) {
    return (
      <InboxFrame counts={counts}>
        <InboxHeader />
        <InboxErrorBanner
          lastSuccess={payload.refreshedAt}
          onRetry={() => query.refetch()}
        />
        <Filters
          filter={filter}
          setFilter={setFilter}
          counts={counts}
          filterRef={filterRef}
        />
        <div className="inbox-stale-overlay">
          <RowList
            rows={filteredRows}
            selectedTaskId={selectedTaskId}
            onOpen={handleOpen}
            onSelect={setSelectedTaskId}
            handleListKey={handleListKey}
          />
        </div>
      </InboxFrame>
    );
  }

  if (forceState === "partial") {
    return (
      <InboxFrame counts={counts}>
        <InboxHeader />
        <Filters
          filter={filter}
          setFilter={setFilter}
          counts={counts}
          filterRef={filterRef}
        />
        <InboxPartial
          currentFilter={filter}
          fallbackFilter={fallbackFilter}
          fallbackCount={fallbackCount}
          onSwap={() => setFilter(fallbackFilter)}
        />
        <InboxFooter />
      </InboxFrame>
    );
  }

  if (
    forceState === "empty" ||
    (filter === DEFAULT_FILTER &&
      filteredRows.length === 0 &&
      counts.decisionRequired === 0)
  ) {
    return (
      <InboxFrame counts={counts}>
        <InboxHeader />
        <Filters
          filter={filter}
          setFilter={setFilter}
          counts={counts}
          filterRef={filterRef}
        />
        <InboxEmpty counts={counts} />
        <InboxFooter />
      </InboxFrame>
    );
  }

  if (filteredRows.length === 0) {
    return (
      <InboxFrame counts={counts}>
        <InboxHeader />
        <Filters
          filter={filter}
          setFilter={setFilter}
          counts={counts}
          filterRef={filterRef}
        />
        <InboxPartial
          currentFilter={filter}
          fallbackFilter={fallbackFilter}
          fallbackCount={fallbackCount}
          onSwap={() => setFilter(fallbackFilter)}
        />
        <InboxFooter />
      </InboxFrame>
    );
  }

  return (
    <InboxFrame counts={counts}>
      <InboxHeader rowCount={filteredRows.length} filter={filter} />
      <Filters
        filter={filter}
        setFilter={setFilter}
        counts={counts}
        filterRef={filterRef}
      />
      <RowList
        rows={filteredRows}
        selectedTaskId={selectedTaskId}
        onOpen={handleOpen}
        onSelect={setSelectedTaskId}
        handleListKey={handleListKey}
      />
      <InboxFooter />
    </InboxFrame>
  );
}

function filterRows(
  rows: ReadonlyArray<InboxRowType>,
  filter: InboxFilter,
): InboxRowType[] {
  const allowed = new Set(FILTER_TO_STATES[filter]);
  return rows.filter((r) => allowed.has(r.state));
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

function focusRow(taskId: string) {
  const el = document.querySelector<HTMLButtonElement>(
    `.inbox-row[data-task-id="${taskId}"]`,
  );
  el?.focus();
}

function InboxFrame({
  children,
  counts,
}: {
  children: React.ReactNode;
  counts: InboxPayload["counts"];
}) {
  return (
    <div className="inbox-shell" data-testid="decision-inbox">
      <aside className="inbox-sidebar" aria-label="Inbox sections">
        <h2>Inbox</h2>
        <nav>
          <a href="#/inbox" className="active">
            Needs decision
            <span
              className={`inbox-count ${counts.decisionRequired > 0 ? "urgent" : ""}`}
            >
              {counts.decisionRequired}
            </span>
          </a>
          <a href="#/inbox?filter=running">
            Running <span className="inbox-count">{counts.running}</span>
          </a>
          <a href="#/inbox?filter=blocked">
            Blocked <span className="inbox-count">{counts.blocked}</span>
          </a>
          <a href="#/inbox?filter=merged">
            Merged today{" "}
            <span className="inbox-count">{counts.mergedToday}</span>
          </a>
        </nav>
      </aside>
      <main className="inbox-main">{children}</main>
    </div>
  );
}

function InboxHeader({
  rowCount,
  filter,
}: {
  rowCount?: number;
  filter?: InboxFilter;
}) {
  const title =
    rowCount !== undefined && filter === "decision_required"
      ? `${rowCount} task${rowCount === 1 ? "" : "s"} need${rowCount === 1 ? "s" : ""} your decision`
      : "Decision Inbox";
  return (
    <>
      <div className="inbox-crumb">/inbox</div>
      <h1 className="inbox-title">{title}</h1>
      <p className="inbox-subtitle">
        Most recent first. Press <span className="kbd">Enter</span> to open the
        top one, <span className="kbd">/</span> to filter.
      </p>
    </>
  );
}

function Filters({
  filter,
  setFilter,
  counts,
  filterRef,
}: {
  filter: InboxFilter;
  setFilter: (f: InboxFilter) => void;
  counts: InboxPayload["counts"];
  filterRef: React.RefObject<HTMLButtonElement | null>;
}) {
  return (
    <div className="inbox-filters" role="tablist" aria-label="Filter tasks">
      {INBOX_FILTERS.map((f, idx) => (
        <button
          key={f.id}
          ref={idx === 0 ? filterRef : null}
          type="button"
          role="tab"
          aria-selected={filter === f.id}
          className={`inbox-filter${filter === f.id ? " active" : ""}`}
          onClick={() => setFilter(f.id)}
        >
          {f.label}
          <span className="num">{counts[f.countKey]}</span>
        </button>
      ))}
    </div>
  );
}

function RowList({
  rows,
  selectedTaskId,
  onOpen,
  onSelect,
  handleListKey,
}: {
  rows: InboxRowType[];
  selectedTaskId: string | null;
  onOpen: (id: string) => void;
  onSelect: (id: string) => void;
  handleListKey: (e: KeyboardEvent<HTMLUListElement>) => void;
}) {
  return (
    <ul
      className="inbox-list"
      aria-label="Tasks"
      onKeyDown={handleListKey}
      style={{ listStyle: "none", margin: 0, padding: 0 }}
    >
      {rows.map((row) => (
        <li key={row.taskId}>
          <InboxRow
            row={row}
            isSelected={row.taskId === selectedTaskId}
            onOpen={onOpen}
            onSelect={onSelect}
          />
        </li>
      ))}
    </ul>
  );
}

function InboxFooter() {
  return (
    <div className="inbox-footer">
      <span>
        <span className="kbd">↑↓</span> navigate ·{" "}
        <span className="kbd">Enter</span> open · <span className="kbd">m</span>{" "}
        merge · <span className="kbd">r</span> request changes
      </span>
      <span>Auto-refresh: 5s</span>
    </div>
  );
}

function renderLoading(showText: boolean) {
  return (
    <>
      <InboxHeader />
      <div className="inbox-list" aria-busy="true" aria-live="polite">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="inbox-skeleton-row">
            <div>
              <div className="inbox-skeleton-bar medium" />
              <div className="inbox-skeleton-bar short" />
            </div>
            <div className="inbox-skeleton-bar short" />
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

function InboxEmpty({ counts }: { counts: InboxPayload["counts"] }) {
  return (
    <div className="inbox-empty">
      <h2>
        Nothing waiting on you. {counts.running} agent
        {counts.running === 1 ? "" : "s"} {counts.running === 1 ? "is" : "are"}{" "}
        running background tasks.
      </h2>
      <p>
        Press <span className="kbd">n</span> to start a new task, or kick one
        off from the CLI with <code>wuphf task start</code>.
      </p>
      <button type="button" className="btn-primary">
        Start a new task
      </button>
    </div>
  );
}

function InboxErrorBanner({
  lastSuccess,
  onRetry,
}: {
  lastSuccess: string;
  onRetry: () => void;
}) {
  const fmt = formatLastSuccess(lastSuccess);
  return (
    <div className="inbox-error-banner" role="alert">
      <span className="banner-dot" aria-hidden="true" />
      <div className="body">
        Can't reach the broker. SSE stream offline. Last successful refresh:{" "}
        {fmt}. Showing cached state below.
      </div>
      <button type="button" className="retry" onClick={onRetry}>
        Retry
      </button>
    </div>
  );
}

function InboxPartial({
  currentFilter,
  fallbackFilter,
  fallbackCount,
  onSwap,
}: {
  currentFilter: InboxFilter;
  fallbackFilter: InboxFilter;
  fallbackCount: number;
  onSwap: () => void;
}) {
  return (
    <div className="inbox-partial" role="status">
      <p>
        No tasks in <strong>{labelFor(currentFilter)}</strong>. Switch to{" "}
        <strong>{labelFor(fallbackFilter)}</strong> to see {fallbackCount} task
        {fallbackCount === 1 ? "" : "s"}.
      </p>
      <button type="button" className="swap" onClick={onSwap}>
        Switch to {labelFor(fallbackFilter)}
      </button>
    </div>
  );
}

function labelFor(filter: InboxFilter): string {
  return INBOX_FILTERS.find((f) => f.id === filter)?.label ?? filter;
}

function formatLastSuccess(iso: string): string {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "—";
  return t.toLocaleTimeString();
}
