/**
 * IssueDocument — Phase 3 Issue surface (read-only).
 *
 * Single-column scroll at max-width 720px. Sticky status pill header +
 * sticky button row (empty slot for Phase 4 Approve & Start). Spec
 * sections (Goal/Context/Approach/Acceptance) + comments timeline.
 *
 * After first approval (state "approved" | "running"), spec sections
 * auto-collapse into a 3-line summary card with Expand spec affordance.
 * Collapse/expand is tracked in sessionStorage keyed by taskId so the
 * user's choice survives a same-tab remount.
 *
 * On mount: if ?comment=<id> URL param OR a last-unread-comment data attr
 * is present, the timeline element scrolls to that comment.
 *
 * Phase 3 is READ-ONLY. No Approve & Start, no write endpoints.
 * Phase 4 wires the button row and the drafting gate.
 */

import { useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import { useQuery } from "@tanstack/react-query";

import { get } from "../../api/client";
import {
  messageMarkdownComponents,
  messageRemarkPlugins,
} from "../../lib/messageMarkdown";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { PixelAvatar } from "../ui/PixelAvatar";
import { LifecycleStatePill } from "./LifecycleStatePill";

// ── Types ──────────────────────────────────────────────────────────────

/**
 * Spec sections for the Issue document.
 * Each section is plain markdown text (may be empty / undefined when the
 * issue was just created).
 */
export interface IssueSpec {
  goal?: string;
  context?: string;
  approach?: string;
  acceptance?: string;
}

/**
 * A single comment on the Issue. Used by both human and agent authors.
 * Reuses the FeedbackItem shape from the existing comment infrastructure
 * (broker_inbox_handler.go:229 / lifecycle.ts FeedbackItem), extended
 * with an id for scroll-targeting.
 */
export interface IssueComment {
  id: string;
  author: string;
  /** True when the author is an agent slug (vs. "human"). */
  isAgent: boolean;
  body: string;
  /** RFC3339 / ISO datetime. */
  appendedAt: string;
}

/**
 * Full Issue document payload.
 * Fetched from GET /tasks/<taskId>. Fields mirror the broker's `teamTask`
 * JSON shape (camelCase on the wire from the Go side).
 */
export interface IssueDocument {
  taskId: string;
  title: string;
  lifecycleState: LifecycleState;
  spec: IssueSpec;
  comments: IssueComment[];
  ownerSlug?: string;
  createdAt?: string;
  updatedAt?: string;
}

// ── Helpers ────────────────────────────────────────────────────────────

const COLLAPSED_STATES: ReadonlySet<LifecycleState> = new Set([
  "approved",
  "running",
  "review",
  "decision",
]);

function sessionStorageKey(taskId: string): string {
  return `wuphf:issue-spec-expanded:${taskId}`;
}

function readSpecExpanded(taskId: string, defaultValue: boolean): boolean {
  try {
    const v = sessionStorage.getItem(sessionStorageKey(taskId));
    if (v === "true") return true;
    if (v === "false") return false;
    return defaultValue;
  } catch {
    return defaultValue;
  }
}

function writeSpecExpanded(taskId: string, expanded: boolean): void {
  try {
    sessionStorage.setItem(sessionStorageKey(taskId), String(expanded));
  } catch {
    // private-mode tabs — in-memory state only.
  }
}

function formatTimestamp(iso: string): string {
  try {
    const d = new Date(iso);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 1) return "just now";
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    return d.toLocaleDateString();
  } catch {
    return iso;
  }
}

/** Normalize the raw API response into a clean IssueDocument. */
function normalizeIssueDocument(raw: unknown): IssueDocument {
  if (!raw || typeof raw !== "object") {
    throw new Error("invalid issue document response");
  }
  const r = raw as Record<string, unknown>;

  // The broker returns tasks with snake_case keys at the top level;
  // spec sub-fields and comments use camelCase (Go json tags on the
  // Task struct). Normalise both forms at the boundary.
  const taskId =
    (typeof r.taskId === "string" ? r.taskId : undefined) ??
    (typeof r.id === "string" ? r.id : "");
  const title =
    (typeof r.title === "string" ? r.title : undefined) ?? "(untitled)";
  const lifecycleState: LifecycleState =
    typeof r.lifecycleState === "string"
      ? (r.lifecycleState as LifecycleState)
      : typeof r.lifecycle_state === "string"
        ? (r.lifecycle_state as LifecycleState)
        : "intake";

  const rawSpec =
    r.spec && typeof r.spec === "object"
      ? (r.spec as Record<string, unknown>)
      : {};

  const spec: IssueSpec = {
    goal: typeof rawSpec.goal === "string" ? rawSpec.goal : undefined,
    context: typeof rawSpec.context === "string" ? rawSpec.context : undefined,
    approach:
      typeof rawSpec.approach === "string" ? rawSpec.approach : undefined,
    acceptance:
      typeof rawSpec.acceptance === "string"
        ? rawSpec.acceptance
        : typeof rawSpec.targetOutcome === "string"
          ? rawSpec.targetOutcome
          : undefined,
  };

  const rawComments = Array.isArray(r.comments)
    ? r.comments
    : Array.isArray(r.feedback)
      ? r.feedback
      : [];

  const comments: IssueComment[] = rawComments.map(
    (c: unknown, idx: number): IssueComment => {
      const comment = (c ?? {}) as Record<string, unknown>;
      const id =
        typeof comment.id === "string" ? comment.id : `comment-${String(idx)}`;
      const author =
        typeof comment.author === "string" ? comment.author : "unknown";
      const body =
        typeof comment.body === "string"
          ? comment.body
          : typeof comment.text === "string"
            ? comment.text
            : "";
      const appendedAt =
        typeof comment.appendedAt === "string"
          ? comment.appendedAt
          : typeof comment.created_at === "string"
            ? comment.created_at
            : new Date().toISOString();
      const isAgent =
        typeof comment.isAgent === "boolean"
          ? comment.isAgent
          : author !== "human";
      return { id, author, isAgent, body, appendedAt };
    },
  );

  return {
    taskId,
    title,
    lifecycleState,
    spec,
    comments,
    ownerSlug:
      typeof r.ownerSlug === "string"
        ? r.ownerSlug
        : typeof r.owner === "string"
          ? r.owner
          : undefined,
    createdAt:
      typeof r.createdAt === "string"
        ? r.createdAt
        : typeof r.created_at === "string"
          ? r.created_at
          : undefined,
    updatedAt:
      typeof r.updatedAt === "string"
        ? r.updatedAt
        : typeof r.updated_at === "string"
          ? r.updated_at
          : undefined,
  };
}

async function fetchIssueDocument(taskId: string): Promise<IssueDocument> {
  // The broker exposes the full task at /tasks/<id>. IssueDocument is a
  // presentation projection; we re-use the same endpoint as the Decision
  // Packet (which GET /tasks/<id> already serves) and normalise at the
  // boundary.
  const raw = await get<unknown>(`/tasks/${encodeURIComponent(taskId)}`);
  return normalizeIssueDocument(raw);
}

// ── Sub-components ─────────────────────────────────────────────────────

interface SpecSectionProps {
  heading: string;
  content: string | undefined;
}

function SpecSection({ heading, content }: SpecSectionProps) {
  const body = content?.trim() || "—";
  const isEmpty = !content?.trim();
  return (
    <section className="issue-spec-section" aria-labelledby={`spec-${heading}`}>
      <h3 id={`spec-${heading}`} className="issue-spec-heading">
        {heading}
      </h3>
      {isEmpty ? (
        <p className="issue-spec-empty" aria-label="No content yet">
          —
        </p>
      ) : (
        <div className="issue-spec-body">
          <ReactMarkdown
            remarkPlugins={messageRemarkPlugins}
            components={messageMarkdownComponents}
          >
            {body}
          </ReactMarkdown>
        </div>
      )}
    </section>
  );
}

function SpecSummaryCard({
  spec,
  onExpand,
}: {
  spec: IssueSpec;
  onExpand: () => void;
}) {
  // Produce a 3-line plaintext summary from the spec sections.
  const lines = [spec.goal, spec.context, spec.approach, spec.acceptance]
    .filter(Boolean)
    .slice(0, 3)
    .map((s) => s!.trim().split("\n")[0] ?? "");

  return (
    <div className="issue-spec-summary" aria-label="Spec summary (collapsed)">
      <div className="issue-spec-summary-lines" aria-hidden="true">
        {lines.length > 0 ? (
          lines.map((line, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: static slice, index is stable here.
            <p key={i} className="issue-spec-summary-line">
              {line}
            </p>
          ))
        ) : (
          <p className="issue-spec-summary-line issue-spec-empty">
            No spec content yet.
          </p>
        )}
      </div>
      <button
        type="button"
        className="issue-spec-expand-btn"
        onClick={onExpand}
        aria-label="Expand spec sections"
      >
        Expand spec
      </button>
    </div>
  );
}

interface CommentItemProps {
  comment: IssueComment;
}

function CommentItem({ comment }: CommentItemProps) {
  const label = comment.isAgent ? `Agent ${comment.author}` : "Human";
  return (
    <article
      id={`comment-${comment.id}`}
      className="issue-comment"
      aria-label={`Comment by ${comment.author}`}
    >
      <div className="issue-comment-meta">
        <PixelAvatar
          slug={comment.author}
          size={24}
          className="issue-comment-avatar"
        />
        <span className="issue-comment-author" aria-label={`Author: ${label}`}>
          {comment.author}
        </span>
        <time
          className="issue-comment-time"
          dateTime={comment.appendedAt}
          title={comment.appendedAt}
        >
          {formatTimestamp(comment.appendedAt)}
        </time>
      </div>
      <div className="issue-comment-body">
        <ReactMarkdown
          remarkPlugins={messageRemarkPlugins}
          components={messageMarkdownComponents}
        >
          {comment.body}
        </ReactMarkdown>
      </div>
    </article>
  );
}

// ── Loading + error states ─────────────────────────────────────────────

function IssueDocumentSkeleton() {
  return (
    <div
      className="issue-document issue-document--loading"
      data-testid="issue-document-loading"
      aria-busy="true"
      aria-label="Loading issue"
    >
      <div className="issue-doc-header issue-doc-header--sticky">
        <div className="issue-doc-skeleton issue-doc-skeleton--pill" />
        <div className="issue-doc-skeleton issue-doc-skeleton--title" />
      </div>
      <div className="issue-doc-body">
        {[0, 1, 2, 3].map((i) => (
          <div
            key={i}
            className="issue-doc-skeleton issue-doc-skeleton--block"
            style={{ width: `${70 + (i % 2) * 15}%` }}
          />
        ))}
      </div>
    </div>
  );
}

function IssueDocumentError({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div
      className="issue-document issue-document--error"
      data-testid="issue-document-error"
    >
      <div className="issue-doc-error-card" role="alert">
        <strong>Could not load issue</strong>
        <p>{message}</p>
        <button type="button" className="issue-doc-retry-btn" onClick={onRetry}>
          Retry
        </button>
      </div>
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────

interface IssueDocumentProps {
  taskId: string;
  /** Skip fetch and render with these data directly. Used by tests + screenshots. */
  initialDocument?: IssueDocument;
}

/**
 * IssueDocument renders a single Issue in the Phase 3 read-only view.
 *
 * Props:
 *   taskId — the task ID to fetch. Drives the query key.
 *   initialDocument — if provided, skipped fetch; used in tests.
 *
 * The component manages spec-collapsed state in sessionStorage so
 * returning to an already-approved issue restores the user's choice.
 */
export function IssueDocument({ taskId, initialDocument }: IssueDocumentProps) {
  const query = useQuery<IssueDocument>({
    queryKey: ["issue", taskId],
    queryFn: () => fetchIssueDocument(taskId),
    initialData: initialDocument,
    staleTime: 5_000,
    enabled: !initialDocument,
  });

  // Determine whether spec sections should auto-collapse.
  const doc = query.data;
  const shouldAutoCollapse = doc
    ? COLLAPSED_STATES.has(doc.lifecycleState)
    : false;
  const defaultExpanded = !shouldAutoCollapse;

  const [specExpanded, setSpecExpanded] = useState<boolean>(() =>
    readSpecExpanded(taskId, defaultExpanded),
  );

  // When the document loads for the first time and auto-collapse is
  // active, apply the stored preference or default to collapsed.
  useEffect(() => {
    if (!doc) return;
    const stored = readSpecExpanded(taskId, defaultExpanded);
    setSpecExpanded(stored);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId, shouldAutoCollapse]);

  // Persist spec expand/collapse on change.
  function toggleSpec() {
    setSpecExpanded((prev) => {
      const next = !prev;
      writeSpecExpanded(taskId, next);
      return next;
    });
  }

  // Scroll to last-unread comment on mount (URL param or data attr).
  const timelineRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!doc || doc.comments.length === 0) return;
    const params = new URLSearchParams(window.location.search);
    const commentId = params.get("comment");
    if (commentId) {
      const el = document.getElementById(`comment-${commentId}`);
      el?.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  }, [doc]);

  if (query.isPending && !initialDocument) {
    return <IssueDocumentSkeleton />;
  }

  if (query.isError && !doc) {
    return (
      <IssueDocumentError
        message={
          query.error instanceof Error
            ? query.error.message
            : "Network or broker error."
        }
        onRetry={() => void query.refetch()}
      />
    );
  }

  if (!doc) {
    return <IssueDocumentSkeleton />;
  }

  return (
    <div
      className="issue-document"
      data-testid="issue-document"
      data-task-id={taskId}
      data-lifecycle-state={doc.lifecycleState}
    >
      {/* Sticky header: status pill + title */}
      <header
        className="issue-doc-header issue-doc-header--sticky"
        aria-label="Issue header"
      >
        <div className="issue-doc-header-row">
          <LifecycleStatePill state={doc.lifecycleState} />
          <h2 className="issue-doc-title">{doc.title}</h2>
        </div>
        {/*
         * Phase 4 button row slot.
         * Empty div with known class name so Phase 4 can query-select or
         * pass a `buttons` prop without touching the layout structure.
         * aria-hidden so screen readers don't announce an empty region.
         */}
        <div
          className="issue-doc-button-row"
          aria-hidden="true"
          data-testid="issue-doc-button-row"
        />
      </header>

      {/* Body: spec sections + comments timeline */}
      <div className="issue-doc-body">
        {/* Spec sections */}
        {shouldAutoCollapse && !specExpanded ? (
          <SpecSummaryCard spec={doc.spec} onExpand={toggleSpec} />
        ) : (
          <section className="issue-doc-spec" aria-label="Issue specification">
            {shouldAutoCollapse && (
              <button
                type="button"
                className="issue-spec-collapse-btn"
                onClick={toggleSpec}
                aria-label="Collapse spec sections"
              >
                Collapse spec
              </button>
            )}
            <SpecSection heading="Goal" content={doc.spec.goal} />
            <SpecSection heading="Context" content={doc.spec.context} />
            <SpecSection heading="Approach" content={doc.spec.approach} />
            <SpecSection heading="Acceptance" content={doc.spec.acceptance} />
          </section>
        )}

        {/* Comments timeline */}
        <section
          className="issue-doc-comments"
          aria-label="Comments"
          aria-live="polite"
          ref={timelineRef}
        >
          <h3 className="issue-comments-heading">Comments</h3>
          {doc.comments.length === 0 ? (
            <p
              className="issue-comments-empty"
              data-testid="issue-comments-empty"
            >
              No comments yet.
            </p>
          ) : (
            <div
              className="issue-comments-list"
              data-testid="issue-comments-list"
            >
              {doc.comments.map((c) => (
                <CommentItem key={c.id} comment={c} />
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
