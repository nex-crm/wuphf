/**
 * IssueDocument — Phase 4 Issue surface.
 *
 * Extends Phase 3 read-only surface with:
 *  - Approve & Start button (visible only when lifecycleState === "drafting")
 *    Maps to the existing approve lifecycle action (postDecision "approve").
 *    Optimistic "Starting…" state → awaits query invalidation → "running".
 *  - Streaming draft rendering via SSE "issue_draft_section" events.
 *    Sections stream in-order: goal → context → approach → acceptance.
 *    Typing-dot prefix on unwritten sections; removed when all finish.
 *    aria-live="polite" on the spec region for a11y.
 *  - Comment helper line in Drafting state:
 *    "Anyone can comment — execution starts after Approve & Start."
 *  - Action row slot is no longer aria-hidden so the button is reachable
 *    by screen readers.
 *
 * Phase 3 behaviour is fully preserved for non-Drafting states.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { get, sseURL } from "../../api/client";
import {
  postDecision,
  postTaskComment,
  postTaskReject,
} from "../../api/lifecycle";
import {
  createSubIssue,
  getOfficeTasks,
  getSubIssues,
  reassignTask,
  reopenIssue,
  type Task,
  type TaskStatusAction,
  updateTaskStatus,
} from "../../api/tasks";
import { useOfficeMembers } from "../../hooks/useMembers";
import { router } from "../../lib/router";
import {
  messageMarkdownComponents,
  messageRemarkPlugins,
} from "../../lib/messageMarkdown";
import type { LifecycleState } from "../../lib/types/lifecycle";
import {
  Autocomplete,
  type AutocompleteItem,
  applyAutocomplete,
} from "../messages/Autocomplete";
import { PixelAvatar } from "../ui/PixelAvatar";
import { formatIssueTitleForDisplay } from "../../lib/issueTitle";
import { IssueActivityFeed } from "./IssueActivityFeed";
import {
  IssueActivityStream,
  IssueStatusDot,
} from "./IssueActivityStream";
import { LifecycleStatePill } from "./LifecycleStatePill";

// ── Phase 4 constants ──────────────────────────────────────────────────

/**
 * Section keys for streaming draft events.
 * The broker emits SSE "issue_draft_section" events with this shape:
 *   { taskId: string; section: DraftSectionKey; text: string }
 */
export type DraftSectionKey = "goal" | "context" | "approach" | "acceptance";

const DRAFT_SECTION_KEYS: ReadonlyArray<DraftSectionKey> = [
  "goal",
  "context",
  "approach",
  "acceptance",
];

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
  /** Plain-markdown description (task.details from the broker).
   * Linear-style: just the body, no Goal/Context/Approach/Acceptance
   * sections to fill out. */
  description: string;
  lifecycleState: LifecycleState;
  /** Retained for back-compat with stream-handler code; unused by
   * the Linear-style body. New work should write to `description`. */
  spec: IssueSpec;
  comments: IssueComment[];
  channel: string;
  ownerSlug?: string;
  parentIssueId?: string;
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

/** Read a string from an object field or its snake_case alias. */
function strField(
  r: Record<string, unknown>,
  camel: string,
  snake?: string,
): string | undefined {
  const v = r[camel];
  if (typeof v === "string") return v;
  if (snake) {
    const sv = r[snake];
    if (typeof sv === "string") return sv;
  }
  return undefined;
}

/** Normalize spec sub-object from raw broker response. */
function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object"
    ? (value as Record<string, unknown>)
    : undefined;
}

function taskStatusToLifecycleState(task: Task | undefined): LifecycleState {
  if (task?.pipeline_stage === "draft") return "drafting";
  const state = task?.lifecycle_state ?? task?.status;
  switch (state) {
    case "drafting":
    case "intake":
    case "ready":
    case "running":
    case "review":
    case "decision":
    case "blocked_on_pr_merge":
    case "changes_requested":
    case "approved":
    case "rejected":
      return state as LifecycleState;
    case "open":
      return "intake";
    case "in_progress":
      return "running";
    case "done":
      return "approved";
    case "blocked":
      return "blocked_on_pr_merge";
    default:
      return "intake";
  }
}

function normalizeAcceptanceCriteria(value: unknown): string | undefined {
  if (!Array.isArray(value)) return undefined;
  const lines = value
    .map((item) => {
      if (typeof item === "string") return item.trim();
      const row = recordValue(item);
      const statement = row ? strField(row, "statement") : undefined;
      return statement?.trim() ?? "";
    })
    .filter(Boolean)
    .map((statement) => `- ${statement}`);
  return lines.length > 0 ? lines.join("\n") : undefined;
}

function normalizeSpec(
  rawSpec: Record<string, unknown>,
  taskHint?: Task,
): IssueSpec {
  return {
    goal:
      strField(rawSpec, "goal") ??
      strField(rawSpec, "targetOutcome") ??
      taskHint?.details ??
      taskHint?.description ??
      strField(rawSpec, "problem"),
    context: strField(rawSpec, "context") ?? strField(rawSpec, "problem"),
    approach: strField(rawSpec, "approach") ?? strField(rawSpec, "assignment"),
    acceptance:
      strField(rawSpec, "acceptance") ??
      normalizeAcceptanceCriteria(rawSpec.acceptanceCriteria),
  };
}

/** Normalize one comment entry from the raw broker response. */
function normalizeComment(c: unknown, idx: number): IssueComment {
  const comment = (c ?? {}) as Record<string, unknown>;
  const id = strField(comment, "id") ?? `comment-${String(idx)}`;
  const author = strField(comment, "author") ?? "unknown";
  const body = strField(comment, "body") ?? strField(comment, "text") ?? "";
  const appendedAt =
    strField(comment, "appendedAt") ??
    strField(comment, "created_at") ??
    new Date().toISOString();
  const isAgent =
    typeof comment.isAgent === "boolean" ? comment.isAgent : author !== "human";
  return { id, author, isAgent, body, appendedAt };
}

function resolveIssueTaskId(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  taskHint: Task | undefined,
): string {
  return (
    strField(packet, "taskId", "id") ??
    (taskRecord ? strField(taskRecord, "taskId", "id") : undefined) ??
    taskHint?.id ??
    ""
  );
}

function resolveIssueTitle(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  spec: Record<string, unknown>,
  taskHint: Task | undefined,
  taskId: string,
): string {
  const fallbackTitle = taskId || "(untitled)";
  return (
    strField(packet, "title") ??
    (taskRecord ? strField(taskRecord, "title") : undefined) ??
    taskHint?.title ??
    strField(spec, "assignment") ??
    fallbackTitle
  );
}

function resolveIssueLifecycleState(
  packet: Record<string, unknown>,
  taskHint: Task | undefined,
): LifecycleState {
  const rawState = strField(packet, "lifecycleState", "lifecycle_state");
  return rawState
    ? (rawState as LifecycleState)
    : taskStatusToLifecycleState(taskHint);
}

function normalizeIssueComments(
  packet: Record<string, unknown>,
  spec: Record<string, unknown>,
): IssueComment[] {
  const rawComments: unknown[] = Array.isArray(packet.comments)
    ? packet.comments
    : Array.isArray(packet.feedback)
      ? packet.feedback
      : Array.isArray(spec.feedback)
        ? spec.feedback
        : [];
  return rawComments.map(normalizeComment);
}

function resolveAliasedField(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  camel: string,
  snake: string,
): string | undefined {
  return (
    strField(packet, camel, snake) ??
    (taskRecord ? strField(taskRecord, camel, snake) : undefined)
  );
}

function resolveIssueChannel(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  taskHint: Task | undefined,
): string {
  const channel =
    resolveAliasedField(packet, taskRecord, "channel", "channel")?.trim() ||
    taskHint?.channel?.trim();
  if (!channel) {
    throw new Error("issue channel is missing");
  }
  return channel;
}

/** Normalize the raw API response into a clean IssueDocument. */
export function normalizeIssueDocument(
  raw: unknown,
  taskHint?: Task,
): IssueDocument {
  if (!raw || typeof raw !== "object") {
    throw new Error("invalid issue document response");
  }
  const r = raw as Record<string, unknown>;
  const taskRecord = recordValue(r.task);
  const rawSpec = recordValue(r.spec) ?? {};

  // The broker returns tasks with snake_case keys at the top level;
  // /tasks/<id> returns the decision-packet shape. Normalise both
  // forms at the boundary so the document route can render direct
  // links and list-to-detail navigations consistently.
  const taskId = resolveIssueTaskId(r, taskRecord, taskHint);
  const title = resolveIssueTitle(r, taskRecord, rawSpec, taskHint, taskId);
  const lifecycleState = resolveIssueLifecycleState(r, taskHint);
  const spec = normalizeSpec(rawSpec, taskHint);
  const comments = normalizeIssueComments(r, rawSpec);

  // Linear-style description: the broker writes `details` on the task
  // record; legacy clients may still write `description`. Fall back to
  // the spec's `assignment` block when neither is set so existing
  // packet-driven Issues still render something.
  const description =
    (taskRecord
      ? strField(taskRecord, "details", "description")
      : undefined) ??
    taskHint?.description ??
    strField(rawSpec, "assignment") ??
    "";

  const parentIssueId =
    (taskRecord
      ? strField(taskRecord, "parentIssueId", "parent_issue_id")
      : undefined) ?? undefined;

  return {
    taskId,
    title,
    description,
    lifecycleState,
    spec,
    comments,
    channel: resolveIssueChannel(r, taskRecord, taskHint),
    ownerSlug:
      resolveAliasedField(r, taskRecord, "ownerSlug", "owner") ??
      taskHint?.owner,
    parentIssueId,
    createdAt:
      resolveAliasedField(r, taskRecord, "createdAt", "created_at") ??
      taskHint?.created_at,
    updatedAt:
      resolveAliasedField(r, taskRecord, "updatedAt", "updated_at") ??
      taskHint?.updated_at,
  };
}

async function fetchIssueDocument(taskId: string): Promise<IssueDocument> {
  // The broker exposes the full task at /tasks/<id>. IssueDocument is a
  // presentation projection; we re-use the same endpoint as the Decision
  // Packet (which GET /tasks/<id> already serves) and normalise at the
  // boundary.
  const [raw, tasksResponse] = await Promise.all([
    get<unknown>(`/tasks/${encodeURIComponent(taskId)}`),
    getOfficeTasks({ includeDone: true }).catch(() => undefined),
  ]);
  const taskHint = tasksResponse?.tasks.find((task) => task.id === taskId);
  return normalizeIssueDocument(raw, taskHint);
}

// ── Sub-components ─────────────────────────────────────────────────────

interface SpecSectionProps {
  heading: string;
  content: string | undefined;
  /**
   * When true, the section has not started streaming yet.
   * Renders a typing-dot prefix to signal "CEO is writing this".
   * Respects prefers-reduced-motion: dots hidden when reduced-motion active.
   */
  isStreaming?: boolean;
}

function SpecSection({ heading, content, isStreaming }: SpecSectionProps) {
  const body = content?.trim() || "—";
  const isEmpty = !content?.trim();
  return (
    <section className="issue-spec-section" aria-labelledby={`spec-${heading}`}>
      <h3 id={`spec-${heading}`} className="issue-spec-heading">
        {heading}
        {isStreaming ? (
          <span
            className="typing-dots"
            aria-label="CEO is writing this section"
            role="status"
          >
            <span aria-hidden="true">…</span>
          </span>
        ) : null}
      </h3>
      {isEmpty ? (
        <p className="issue-spec-empty">—</p>
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
    .filter((s): s is string => Boolean(s))
    .slice(0, 3)
    .map((s) => s.trim().split("\n")[0] ?? "");

  return (
    <section
      className="issue-spec-summary"
      aria-label="Spec summary (collapsed)"
    >
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
    </section>
  );
}

interface CommentItemProps {
  comment: IssueComment;
}

function CommentItem({ comment }: CommentItemProps) {
  const label = comment.isAgent ? `Agent ${comment.author}` : "Human";
  // [SUGGESTION] prefix → specialist scope proposal (Slice 7). Highlight
  // the card so CEO scans them quickly + strip the marker from the
  // visible body since it duplicates the label.
  const trimmed = comment.body.trimStart();
  const isSuggestion = /^\[SUGGESTION\]/i.test(trimmed);
  const body = isSuggestion
    ? trimmed.replace(/^\[SUGGESTION\]\s*/i, "")
    : comment.body;
  return (
    <article
      id={`comment-${comment.id}`}
      className={
        "issue-comment" +
        (isSuggestion ? " issue-comment--suggestion" : "")
      }
      aria-label={`Comment by ${comment.author}`}
    >
      <div className="issue-comment-meta">
        <PixelAvatar
          slug={comment.author}
          size={24}
          className="issue-comment-avatar"
        />
        <span className="issue-comment-author" title={label}>
          {comment.author}
        </span>
        {isSuggestion ? (
          <span
            className="issue-comment-suggestion-badge"
            title="Specialist suggestion — CEO decides"
          >
            Suggestion
          </span>
        ) : null}
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
          {body}
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
      role="status"
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

// ── Phase 4 sub-components ─────────────────────────────────────────────

/**
 * Approve & Start button. Visible only during `drafting` state.
 *
 * On click: POSTs to existing approve endpoint (postDecision "approve"),
 * transitions optimistically to "Starting…", then refetches the task.
 * On error: inline error banner appears, button re-enables.
 *
 * A11y: aria-label, focus-visible outline, Enter/Space activatable via
 * the native <button> element.
 */
interface ApproveAndStartButtonProps {
  taskId: string;
  onApproved: () => void;
}

function ApproveAndStartButton({
  taskId,
  onApproved,
}: ApproveAndStartButtonProps) {
  const [approveError, setApproveError] = useState<string | null>(null);

  const approveMutation = useMutation({
    mutationFn: () => postDecision(taskId, "approve"),
    onSuccess: () => {
      setApproveError(null);
      onApproved();
    },
    onError: (err: unknown) => {
      const message =
        err instanceof Error ? err.message : "Failed to approve issue.";
      setApproveError(message);
    },
  });

  const { isPending } = approveMutation;

  return (
    <div
      className="issue-approve-and-start"
      data-testid="approve-and-start-wrapper"
    >
      {approveError ? (
        <div
          className="issue-approve-error"
          role="alert"
          data-testid="approve-and-start-error"
        >
          {approveError}
        </div>
      ) : null}
      <button
        type="button"
        className="btn btn-primary issue-approve-btn"
        disabled={isPending}
        onClick={() => approveMutation.mutate()}
        aria-label="Approve and start execution"
        data-testid="approve-and-start"
      >
        {isPending ? "Starting…" : "Approve & Start"}
      </button>
    </div>
  );
}

/**
 * Close Issue button. Visible in any non-terminal lifecycle state so
 * the human can shelve work that's no longer relevant (CEO went down
 * the wrong path, scope changed, user moved on). Posts a Reject via
 * the existing /tasks endpoint — reject is terminal; downstream blocks
 * stay blocked, packet records the reason, channel gets a "task closed"
 * broadcast via postTaskCancelNotificationsLocked.
 *
 * Two-step gesture: first click reveals a reason textarea + confirm.
 * Without that gate, a stray click on a hover-target permanently
 * closes the Issue with no chance to undo.
 */
interface CloseIssueButtonProps {
  taskId: string;
  onClosed: () => void;
}

function CloseIssueButton({ taskId, onClosed }: CloseIssueButtonProps) {
  const [confirming, setConfirming] = useState(false);
  const [reason, setReason] = useState("");
  const [closeError, setCloseError] = useState<string | null>(null);

  const closeMutation = useMutation({
    mutationFn: (r: string) => postTaskReject(taskId, r),
    onSuccess: () => {
      setCloseError(null);
      setConfirming(false);
      setReason("");
      onClosed();
    },
    onError: (err: unknown) => {
      setCloseError(
        err instanceof Error ? err.message : "Failed to close issue.",
      );
    },
  });

  const trimmed = reason.trim();
  const canSubmit = trimmed.length > 0 && !closeMutation.isPending;

  if (!confirming) {
    return (
      <button
        type="button"
        className="btn btn-ghost issue-close-btn"
        onClick={() => setConfirming(true)}
        aria-label="Close this issue (terminal)"
        data-testid="close-issue"
      >
        Close issue
      </button>
    );
  }

  return (
    <div
      className="issue-close-confirm"
      data-testid="close-issue-confirm"
      role="group"
      aria-label="Confirm close issue"
    >
      <label className="issue-close-confirm-label" htmlFor="close-reason">
        Reason for closing (required)
      </label>
      <textarea
        id="close-reason"
        className="issue-close-confirm-input"
        value={reason}
        onChange={(e) => {
          setReason(e.target.value);
          if (closeError) setCloseError(null);
        }}
        placeholder="e.g. Scope changed, no longer needed, duplicate of …"
        rows={2}
        disabled={closeMutation.isPending}
        data-testid="close-issue-reason"
      />
      {closeError ? (
        <p
          className="issue-close-confirm-error"
          role="alert"
          data-testid="close-issue-error"
        >
          {closeError}
        </p>
      ) : null}
      <div className="issue-close-confirm-actions">
        <button
          type="button"
          className="btn btn-ghost"
          onClick={() => {
            setConfirming(false);
            setReason("");
            setCloseError(null);
          }}
          disabled={closeMutation.isPending}
        >
          Cancel
        </button>
        <button
          type="button"
          className="btn btn-danger"
          disabled={!canSubmit}
          onClick={() => closeMutation.mutate(trimmed)}
          data-testid="close-issue-confirm"
        >
          {closeMutation.isPending ? "Closing…" : "Close issue"}
        </button>
      </div>
    </div>
  );
}

// ── Streaming draft hook ────────────────────────────────────────────────

/**
 * Accumulated draft text per section, updated via SSE.
 * null means the section hasn't started streaming yet.
 */
type DraftAccumulator = Record<DraftSectionKey, string | null>;

function emptyAccumulator(): DraftAccumulator {
  return { goal: null, context: null, approach: null, acceptance: null };
}

/**
 * Parse a raw SSE event data string into a typed draft section update.
 * Returns null if the event is malformed or not for the given taskId.
 */
function parseDraftSectionEvent(
  raw: string,
  taskId: string,
): { section: DraftSectionKey; text: string } | null {
  let payload: unknown;
  try {
    payload = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (
    typeof p.taskId !== "string" ||
    p.taskId !== taskId ||
    typeof p.section !== "string" ||
    typeof p.text !== "string"
  ) {
    return null;
  }
  const key = p.section as DraftSectionKey;
  if (!DRAFT_SECTION_KEYS.includes(key)) return null;
  return { section: key, text: p.text };
}

/**
 * Subscribes to the broker SSE stream and listens for
 * "issue_draft_section" events for this taskId.
 *
 * Event payload expected: { taskId: string; section: DraftSectionKey; text: string }
 *
 * On unmount, the SSE connection is closed.
 */
function useDraftStream(taskId: string, enabled: boolean): DraftAccumulator {
  const [draft, setDraft] = useState<DraftAccumulator>(emptyAccumulator);

  useEffect(() => {
    if (!enabled) return;

    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES) return;

    const source = new ES(sseURL("/events"));

    source.addEventListener("issue_draft_section", (event) => {
      if (!("data" in event) || typeof event.data !== "string") return;
      const parsed = parseDraftSectionEvent(event.data, taskId);
      if (!parsed) return;
      const { section, text } = parsed;
      setDraft((prev) => ({
        ...prev,
        [section]: (prev[section] ?? "") + text,
      }));
    });

    return () => {
      source.close();
    };
  }, [taskId, enabled]);

  return draft;
}

// ── Comments timeline sub-component ───────────────────────────────────

interface CommentsTimelineProps {
  taskId: string;
  channel: string;
  comments: IssueComment[];
  isDrafting: boolean;
  timelineRef: React.RefObject<HTMLDivElement | null>;
  onCommentPosted: () => void;
}

function CommentsTimeline({
  taskId,
  channel,
  comments,
  isDrafting,
  timelineRef,
  onCommentPosted,
}: CommentsTimelineProps) {
  const [commentBody, setCommentBody] = useState("");
  const [commentError, setCommentError] = useState<string | null>(null);
  const [caret, setCaret] = useState(0);
  const [acItems, setAcItems] = useState<AutocompleteItem[]>([]);
  const [acIdx, setAcIdx] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const trimmedComment = commentBody.trim();

  const commentMutation = useMutation({
    mutationFn: (body: string) => postTaskComment(taskId, channel, body),
    onSuccess: () => {
      setCommentBody("");
      setCaret(0);
      setCommentError(null);
      onCommentPosted();
    },
    onError: (err: unknown) => {
      setCommentError(
        err instanceof Error ? err.message : "Could not post comment.",
      );
    },
  });

  const pickAutocomplete = useCallback(
    (item: AutocompleteItem) => {
      const next = applyAutocomplete(commentBody, caret, item);
      setCommentBody(next.text);
      requestAnimationFrame(() => {
        const el = textareaRef.current;
        if (!el) return;
        el.focus();
        el.setSelectionRange(next.caret, next.caret);
        setCaret(next.caret);
      });
    },
    [commentBody, caret],
  );

  const handleAcItems = useCallback((items: AutocompleteItem[]) => {
    setAcItems(items);
    setAcIdx((prev) => (prev >= items.length ? 0 : prev));
  }, []);

  function submitComment(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!trimmedComment || commentMutation.isPending) return;
    setCommentError(null);
    commentMutation.mutate(trimmedComment);
  }

  function handleCommentKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Autocomplete keyboard nav runs first so the textarea doesn't
    // swallow Enter when the panel is open. Same pattern as Composer.
    if (acItems.length > 0) {
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setAcIdx((prev) => (prev + 1) % acItems.length);
          return;
        case "ArrowUp":
          event.preventDefault();
          setAcIdx((prev) => (prev - 1 + acItems.length) % acItems.length);
          return;
        case "Tab":
        case "Enter": {
          event.preventDefault();
          const pick = acItems[acIdx];
          if (pick) pickAutocomplete(pick);
          return;
        }
        case "Escape":
          event.preventDefault();
          setAcItems([]);
          return;
      }
    }
    // Cmd/Ctrl+Enter submits when autocomplete is not active.
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      if (trimmedComment && !commentMutation.isPending) {
        commentMutation.mutate(trimmedComment);
      }
    }
  }

  return (
    <section
      className="issue-doc-comments"
      aria-label="Timeline"
      aria-live="polite"
      ref={timelineRef}
    >
      <h3 className="issue-comments-heading">Timeline</h3>
      {comments.length === 0 ? (
        <div
          className="issue-comments-empty"
          data-testid="issue-comments-empty"
          role="note"
        >
          <p className="issue-comments-empty-title">
            {isDrafting
              ? "CEO will start asking questions here."
              : "Nothing on the timeline yet."}
          </p>
          <p className="issue-comments-empty-hint">
            {isDrafting
              ? "Answer inline to refine the spec. Each turn from the CEO, the team, and you lands here as a card."
              : "Status changes, reviewer comments, and human↔CEO replies will appear here as they happen."}
          </p>
        </div>
      ) : (
        <ol
          className="issue-comments-list"
          data-testid="issue-comments-list"
          aria-label="Timeline entries (chronological)"
        >
          {comments.map((c) => (
            <li key={c.id} className="issue-comments-list-item">
              <CommentItem comment={c} />
            </li>
          ))}
        </ol>
      )}
      {/*
       * Drafting-state comment helper. Lets reviewers know they can
       * comment before execution starts. Server-side gating is the
       * source of truth; this is a UX affordance only.
       */}
      {isDrafting ? (
        <p
          className="issue-comments-drafting-helper"
          data-testid="drafting-comment-helper"
        >
          Anyone can comment — execution starts after Approve &amp; Start.
        </p>
      ) : null}
      <form
        className="issue-comment-form"
        onSubmit={submitComment}
        data-testid="issue-comment-form"
      >
        <label className="issue-comment-form-label" htmlFor="issue-comment">
          Add a comment
        </label>
        <div className="issue-comment-input-wrap">
          <Autocomplete
            value={commentBody}
            caret={caret}
            selectedIdx={acIdx}
            onItems={handleAcItems}
            onPick={pickAutocomplete}
          />
          <textarea
            id="issue-comment"
            ref={textareaRef}
            className="issue-comment-input"
            value={commentBody}
            onChange={(event) => {
              setCommentBody(event.target.value);
              setCaret(event.target.selectionStart ?? event.target.value.length);
              if (commentError) setCommentError(null);
            }}
            onSelect={(event) => {
              const target = event.currentTarget;
              setCaret(target.selectionStart ?? target.value.length);
            }}
            onKeyUp={(event) => {
              const target = event.currentTarget;
              setCaret(target.selectionStart ?? target.value.length);
            }}
            onClick={(event) => {
              const target = event.currentTarget;
              setCaret(target.selectionStart ?? target.value.length);
            }}
            onKeyDown={handleCommentKeyDown}
            placeholder="Ask a question, clarify scope, or leave review notes. Type @ to mention."
            rows={4}
            disabled={commentMutation.isPending}
            data-testid="issue-comment-input"
          />
        </div>
        {commentError ? (
          <p
            className="issue-comment-error"
            role="alert"
            data-testid="issue-comment-error"
          >
            {commentError}
          </p>
        ) : null}
        <button
          type="submit"
          className="issue-comment-submit"
          disabled={!trimmedComment || commentMutation.isPending}
          data-testid="issue-comment-submit"
        >
          {commentMutation.isPending ? "Posting…" : "Comment"}
        </button>
      </form>
    </section>
  );
}

// ── Spec body sub-component ───────────────────────────────────────────

interface SpecBodyProps {
  spec: IssueSpec;
  mergedSpec: IssueSpec;
  shouldAutoCollapse: boolean;
  specExpanded: boolean;
  isDrafting: boolean;
  isSectionStreaming: (key: DraftSectionKey) => boolean;
  onExpand: () => void;
  onCollapse: () => void;
}

function SpecBody({
  spec,
  mergedSpec,
  shouldAutoCollapse,
  specExpanded,
  isDrafting,
  isSectionStreaming,
  onExpand,
  onCollapse,
}: SpecBodyProps) {
  if (shouldAutoCollapse && !specExpanded) {
    return <SpecSummaryCard spec={spec} onExpand={onExpand} />;
  }
  return (
    <section
      className="issue-doc-spec"
      aria-label="Issue specification"
      aria-live={isDrafting ? "polite" : undefined}
    >
      {shouldAutoCollapse ? (
        <button
          type="button"
          className="issue-spec-collapse-btn"
          onClick={onCollapse}
          aria-label="Collapse spec sections"
        >
          Collapse spec
        </button>
      ) : null}
      <SpecSection
        heading="Goal"
        content={mergedSpec.goal}
        isStreaming={isSectionStreaming("goal")}
      />
      <SpecSection
        heading="Context"
        content={mergedSpec.context}
        isStreaming={isSectionStreaming("context")}
      />
      <SpecSection
        heading="Approach"
        content={mergedSpec.approach}
        isStreaming={isSectionStreaming("approach")}
      />
      <SpecSection
        heading="Acceptance"
        content={mergedSpec.acceptance}
        isStreaming={isSectionStreaming("acceptance")}
      />
    </section>
  );
}

// ── Spec streaming helpers ─────────────────────────────────────────────

/**
 * Merge streamed draft sections over the server-fetched spec.
 * A non-null streamed value replaces the server-fetched value for that
 * section so the UI shows live text before the full fetch returns.
 */
function mergeSpec(
  isDrafting: boolean,
  accumulated: DraftAccumulator,
  serverSpec: IssueSpec,
): IssueSpec {
  if (!isDrafting) return serverSpec;
  return {
    goal: accumulated.goal ?? serverSpec.goal,
    context: accumulated.context ?? serverSpec.context,
    approach: accumulated.approach ?? serverSpec.approach,
    acceptance: accumulated.acceptance ?? serverSpec.acceptance,
  };
}

/**
 * Return a predicate that answers "should this section show a typing-dot?".
 *
 * A section shows the dot when:
 * 1. The issue is in Drafting state.
 * 2. Streaming has started (at least one section has received text).
 * 3. The section itself has NOT yet received any streamed text.
 */
function buildSectionStreamingCheck(
  isDrafting: boolean,
  streamingStarted: boolean,
  accumulated: DraftAccumulator,
): (key: DraftSectionKey) => boolean {
  return (key: DraftSectionKey) =>
    isDrafting && streamingStarted && accumulated[key] === null;
}

// ── Main component ─────────────────────────────────────────────────────

interface IssueDocumentProps {
  taskId: string;
  /** Skip fetch and render with these data directly. Used by tests + screenshots. */
  initialDocument?: IssueDocument;
  /**
   * Inject a mock draft accumulator for tests (streaming draft section).
   * In production this is driven by useDraftStream.
   */
  testDraftAccumulator?: DraftAccumulator;
}

/**
 * IssueDocument renders a single Issue, extended in Phase 4 with:
 *  - Approve & Start button (Drafting state only)
 *  - Streaming draft section rendering via SSE
 *  - Comment helper line in Drafting state
 *
 * Props:
 *   taskId — the task ID to fetch. Drives the query key.
 *   initialDocument — if provided, skips fetch; used in tests.
 *   testDraftAccumulator — inject mock draft state for streaming tests.
 *
 * The component manages spec-collapsed state in sessionStorage so
 * returning to an already-approved issue restores the user's choice.
 */
export function IssueDocument({
  taskId,
  initialDocument,
  testDraftAccumulator,
}: IssueDocumentProps) {
  const queryClient = useQueryClient();

  const query = useQuery<IssueDocument>({
    queryKey: ["issue", taskId],
    queryFn: () => fetchIssueDocument(taskId),
    initialData: initialDocument,
    staleTime: 5_000,
    enabled: !initialDocument,
  });

  // Determine whether spec sections should auto-collapse.
  const doc = query.data;
  const isDrafting = doc?.lifecycleState === "drafting";
  const shouldAutoCollapse = doc
    ? COLLAPSED_STATES.has(doc.lifecycleState)
    : false;
  const defaultExpanded = !shouldAutoCollapse;
  const hasDoc = Boolean(doc);

  const [specExpanded, setSpecExpanded] = useState<boolean>(() =>
    readSpecExpanded(taskId, defaultExpanded),
  );

  // When the document loads for the first time and auto-collapse is
  // active, apply the stored preference or default to collapsed.
  useEffect(() => {
    if (!hasDoc) return;
    const stored = readSpecExpanded(taskId, defaultExpanded);
    setSpecExpanded(stored);
  }, [taskId, defaultExpanded, hasDoc]);

  // Persist spec expand/collapse on change.
  function toggleSpec() {
    setSpecExpanded((prev) => {
      const next = !prev;
      writeSpecExpanded(taskId, next);
      return next;
    });
  }

  // ── Streaming draft: subscribe when in Drafting state ─────────────────
  // testDraftAccumulator overrides the SSE-driven state for unit tests.
  const sseAccumulator = useDraftStream(
    taskId,
    isDrafting && !testDraftAccumulator,
  );
  const draftAccumulator = testDraftAccumulator ?? sseAccumulator;
  const mergedSpec = mergeSpec(isDrafting, draftAccumulator, doc?.spec ?? {});
  const streamingStarted = DRAFT_SECTION_KEYS.some(
    (k) => draftAccumulator[k] !== null,
  );
  const isSectionStreaming = buildSectionStreamingCheck(
    isDrafting,
    streamingStarted,
    draftAccumulator,
  );

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
      {/* Sticky header: status pill + title + owner */}
      <header className="issue-doc-header issue-doc-header--sticky">
        {doc.parentIssueId ? (
          <ParentIssueBreadcrumb parentIssueId={doc.parentIssueId} />
        ) : null}
        <div className="issue-doc-header-row">
          <LifecycleStatePill state={doc.lifecycleState} />
          <h2 className="issue-doc-title">{formatIssueTitleForDisplay(doc.title)}</h2>
        </div>
        <div className="issue-doc-meta-row">
          <OwnerPicker
            taskId={taskId}
            channel={doc.channel}
            currentOwner={doc.ownerSlug}
            onChanged={() => {
              void queryClient.invalidateQueries({ queryKey: ["issue", taskId] });
              void queryClient.invalidateQueries({ queryKey: ["issues"] });
              void queryClient.invalidateQueries({
                queryKey: ["issue-children"],
              });
            }}
          />
        </div>
        {/*
         * Phase 4 button row. Contains Approve & Start when in Drafting.
         * Other lifecycle states use the existing Inbox PR-style loop
         * (PacketActionSidebar / DecisionPacketRoute) which is mounted
         * by the parent route — this component does not duplicate those.
         */}
        <div
          className="issue-doc-button-row"
          data-testid="issue-doc-button-row"
        >
          <IssueActionToolbar
            taskId={taskId}
            channel={doc.channel}
            lifecycleState={doc.lifecycleState}
            onAfterAction={() => {
              void queryClient.invalidateQueries({
                queryKey: ["issue", taskId],
              });
              void queryClient.invalidateQueries({ queryKey: ["issues"] });
              void queryClient.invalidateQueries({
                queryKey: ["lifecycle"],
              });
              void queryClient.invalidateQueries({
                queryKey: ["lifecycle", "inbox-items"],
              });
            }}
          />
        </div>
      </header>

      {/* Body: Linear-style — description, sub-issues, comments.
       *  Goal/Context/Approach/Acceptance spec sections were removed
       *  in favor of a single rich description, matching the Linear
       *  product surface the team optimized for. The SpecBody
       *  component is preserved in this file for tests + legacy code
       *  paths but is no longer mounted. */}
      <div className="issue-doc-body">
        <IssueDescription description={doc.description} isDrafting={isDrafting} />

        {/* Live activity stream — surfaces what the owning agent is doing
         *  right now via the SSE-fed agentActivitySnapshots. Stays in the
         *  header zone (always visible) so the human sees the heartbeat
         *  no matter which tab is active. */}
        <IssueActivityStream
          ownerSlug={doc.ownerSlug}
          lifecycleState={doc.lifecycleState}
        />

        <IssueDetailTabs
          taskId={taskId}
          channel={doc.channel}
          comments={doc.comments}
          isDrafting={isDrafting}
          showSubIssues={!doc.parentIssueId}
          timelineRef={timelineRef}
          onCommentPosted={() => {
            void queryClient.invalidateQueries({ queryKey: ["issue", taskId] });
            void queryClient.invalidateQueries({ queryKey: ["issues"] });
            void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
          }}
        />
      </div>
    </div>
  );
}

// ── Issue detail tabs ────────────────────────────────────────────────

type IssueDetailTab = "activity" | "comments" | "sub-issues";

interface IssueDetailTabsProps {
  taskId: string;
  channel: string;
  comments: IssueComment[];
  isDrafting: boolean;
  showSubIssues: boolean;
  timelineRef: React.RefObject<HTMLDivElement | null>;
  onCommentPosted: () => void;
}

/**
 * Linear/Paperclip-shaped tab strip below the description: Activity (the
 * default), Comments, Sub-Issues. The activity feed is the "what's
 * happened" view; Comments is the discussion thread; Sub-Issues hosts the
 * breakdown. Sub-Issues tab is hidden for sub-issues themselves (no
 * sub-sub-issues), matching the prior single-flat-page rule.
 */
function IssueDetailTabs({
  taskId,
  channel,
  comments,
  isDrafting,
  showSubIssues,
  timelineRef,
  onCommentPosted,
}: IssueDetailTabsProps) {
  const [tab, setTab] = useState<IssueDetailTab>("activity");
  const commentCount = comments.length;

  return (
    <div className="issue-doc-tabs">
      <div className="issue-doc-tabs-strip" role="tablist" aria-label="Issue detail">
        <TabButton
          active={tab === "activity"}
          onClick={() => setTab("activity")}
          label="Activity"
        />
        <TabButton
          active={tab === "comments"}
          onClick={() => setTab("comments")}
          label="Comments"
          count={commentCount}
        />
        {showSubIssues ? (
          <TabButton
            active={tab === "sub-issues"}
            onClick={() => setTab("sub-issues")}
            label="Sub-issues"
          />
        ) : null}
      </div>

      <div className="issue-doc-tabs-panel" role="tabpanel">
        {tab === "activity" ? <IssueActivityFeed taskId={taskId} /> : null}
        {tab === "comments" ? (
          <CommentsTimeline
            taskId={taskId}
            channel={channel}
            comments={comments}
            isDrafting={isDrafting}
            timelineRef={timelineRef}
            onCommentPosted={onCommentPosted}
          />
        ) : null}
        {tab === "sub-issues" && showSubIssues ? (
          <SubIssuesList taskId={taskId} channel={channel} />
        ) : null}
      </div>
    </div>
  );
}

interface TabButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  count?: number;
}

function TabButton({ active, onClick, label, count }: TabButtonProps) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      className={`issue-doc-tab${active ? " issue-doc-tab--active" : ""}`}
      onClick={onClick}
    >
      <span>{label}</span>
      {typeof count === "number" && count > 0 ? (
        <span className="issue-doc-tab-count">{count}</span>
      ) : null}
    </button>
  );
}

// ── Linear-style description + sub-issues ─────────────────────────────

interface IssueDescriptionProps {
  description: string;
  isDrafting: boolean;
}

function IssueDescription({ description, isDrafting }: IssueDescriptionProps) {
  const body = description.trim();
  if (!body) {
    return (
      <section
        className="issue-doc-description issue-doc-description--empty"
        aria-label="Description"
      >
        <p className="issue-doc-description-empty-line">
          {isDrafting
            ? "No description yet. Add one in chat — the CEO will fill this out as the spec firms up."
            : "No description."}
        </p>
      </section>
    );
  }
  return (
    <section
      className="issue-doc-description"
      aria-label="Description"
    >
      <div className="issue-doc-description-body">
        <ReactMarkdown
          remarkPlugins={messageRemarkPlugins}
          components={messageMarkdownComponents}
        >
          {body}
        </ReactMarkdown>
      </div>
    </section>
  );
}

interface SubIssuesListProps {
  taskId: string;
  channel: string;
}

function SubIssuesList({ taskId, channel }: SubIssuesListProps) {
  const queryClient = useQueryClient();
  const [isAdding, setIsAdding] = useState(false);
  const [draftTitle, setDraftTitle] = useState("");
  const [draftOwner, setDraftOwner] = useState("");
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const { data: members = [] } = useOfficeMembers();

  const childQuery = useQuery({
    queryKey: ["issue-children", taskId],
    queryFn: () => getSubIssues(taskId),
    staleTime: 5_000,
  });

  const addMutation = useMutation({
    mutationFn: (input: { title: string; owner: string }) =>
      createSubIssue({
        parentIssueId: taskId,
        title: input.title,
        channel,
        owner: input.owner || undefined,
      }),
    onSuccess: () => {
      setDraftTitle("");
      setDraftOwner("");
      setIsAdding(false);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["issue-children", taskId] });
      void queryClient.invalidateQueries({ queryKey: ["issues"] });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not add sub-issue.");
    },
  });

  useEffect(() => {
    if (isAdding) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [isAdding]);

  const children = childQuery.data?.tasks ?? [];
  // Sub-issues + Issues should share owner-pick UX. Exclude `human` and
  // self-loop entries that aren't real agent slugs.
  const assignableAgents = members.filter(
    (m) => m.slug && m.slug !== "human" && m.slug !== "you",
  );

  function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const title = draftTitle.trim();
    if (!title || addMutation.isPending) return;
    addMutation.mutate({ title, owner: draftOwner.trim() });
  }

  function openSub(childId: string) {
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: childId },
    });
  }

  return (
    <section
      className="issue-doc-sub-issues"
      aria-label="Sub-issues"
      data-testid="issue-sub-issues"
    >
      <header className="issue-doc-sub-issues-header">
        <h3 className="issue-doc-sub-issues-heading">
          Sub-issues
          {children.length > 0 ? (
            <span className="issue-doc-sub-issues-count">
              {" "}
              · {children.length}
            </span>
          ) : null}
        </h3>
        {!isAdding ? (
          <button
            type="button"
            className="issue-doc-sub-issues-add"
            onClick={() => setIsAdding(true)}
            data-testid="add-sub-issue-button"
          >
            + Add sub-issue
          </button>
        ) : null}
      </header>

      {children.length === 0 && !isAdding ? (
        <p className="issue-doc-sub-issues-empty">
          No sub-issues. Break this down with the + button above.
        </p>
      ) : null}

      {children.length > 0 ? (
        <ul className="issue-doc-sub-issues-list">
          {children.map((child) => {
            const lifecycle = (child.lifecycle_state ||
              child.status ||
              "drafting") as LifecycleState;
            return (
              <li key={child.id} className="issue-doc-sub-issue-row">
                <button
                  type="button"
                  className="issue-doc-sub-issue-link"
                  onClick={() => openSub(child.id)}
                  data-testid="sub-issue-link"
                  data-task-id={child.id}
                >
                  <IssueStatusDot lifecycleState={lifecycle} />
                  <span className="issue-doc-sub-issue-id">{child.id}</span>
                  <span className="issue-doc-sub-issue-title">
                    {formatIssueTitleForDisplay(child.title) || "(untitled)"}
                  </span>
                  <span className="issue-doc-sub-issue-state">
                    {child.lifecycle_state || child.status}
                  </span>
                  {child.owner ? (
                    <span className="issue-doc-sub-issue-owner">
                      @{child.owner}
                    </span>
                  ) : null}
                </button>
              </li>
            );
          })}
        </ul>
      ) : null}

      {isAdding ? (
        <form
          className="issue-doc-sub-issues-form"
          onSubmit={handleSubmit}
          data-testid="add-sub-issue-form"
        >
          <input
            ref={inputRef}
            className="issue-doc-sub-issues-input"
            value={draftTitle}
            onChange={(event) => {
              setDraftTitle(event.target.value);
              if (error) setError(null);
            }}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                setIsAdding(false);
                setDraftTitle("");
                setDraftOwner("");
              }
            }}
            placeholder="Sub-issue title (Enter to add, Esc to cancel)"
            disabled={addMutation.isPending}
            data-testid="sub-issue-title-input"
          />
          <label className="issue-doc-sub-issues-owner-label">
            Owner
            <select
              className="issue-doc-sub-issues-owner-select"
              value={draftOwner}
              onChange={(event) => setDraftOwner(event.target.value)}
              disabled={addMutation.isPending}
              data-testid="sub-issue-owner-select"
            >
              <option value="">— unassigned —</option>
              {assignableAgents.map((agent) => (
                <option key={agent.slug} value={agent.slug}>
                  @{agent.slug}
                  {agent.name && agent.name !== agent.slug
                    ? ` (${agent.name})`
                    : ""}
                </option>
              ))}
            </select>
          </label>
          <div className="issue-doc-sub-issues-form-actions">
            <button
              type="submit"
              className="issue-doc-sub-issues-submit"
              disabled={!draftTitle.trim() || addMutation.isPending}
            >
              {addMutation.isPending ? "Adding…" : "Add"}
            </button>
            <button
              type="button"
              className="issue-doc-sub-issues-cancel"
              onClick={() => {
                setIsAdding(false);
                setDraftTitle("");
                setDraftOwner("");
                setError(null);
              }}
              disabled={addMutation.isPending}
            >
              Cancel
            </button>
          </div>
          {error ? (
            <p className="issue-doc-sub-issues-error" role="alert">
              {error}
            </p>
          ) : null}
        </form>
      ) : null}
    </section>
  );
}

// ── Owner picker (Issue header) ──────────────────────────────────────

interface OwnerPickerProps {
  taskId: string;
  channel: string;
  currentOwner: string | undefined;
  onChanged: () => void;
}

function OwnerPicker({
  taskId,
  channel,
  currentOwner,
  onChanged,
}: OwnerPickerProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { data: members = [] } = useOfficeMembers();
  const assignableAgents = members.filter(
    (m) => m.slug && m.slug !== "human" && m.slug !== "you",
  );

  const reassignMutation = useMutation({
    mutationFn: (newOwner: string) => reassignTask(taskId, newOwner, channel),
    onSuccess: () => {
      setIsEditing(false);
      setError(null);
      onChanged();
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not reassign.");
    },
  });

  if (!isEditing) {
    return (
      <button
        type="button"
        className="issue-doc-owner-pill"
        onClick={() => setIsEditing(true)}
        data-testid="issue-owner-pill"
        aria-label="Change owner"
      >
        <span className="issue-doc-owner-pill-label">Owner</span>
        <span className="issue-doc-owner-pill-value">
          {currentOwner ? `@${currentOwner}` : "unassigned"}
        </span>
        <span className="issue-doc-owner-pill-edit" aria-hidden="true">
          ✎
        </span>
      </button>
    );
  }

  return (
    <div className="issue-doc-owner-editor">
      <label className="issue-doc-owner-editor-label" htmlFor="owner-select">
        Owner
      </label>
      <select
        id="owner-select"
        className="issue-doc-owner-editor-select"
        defaultValue={currentOwner ?? ""}
        onChange={(event) => {
          const next = event.target.value;
          if (next === (currentOwner ?? "")) {
            setIsEditing(false);
            return;
          }
          reassignMutation.mutate(next);
        }}
        disabled={reassignMutation.isPending}
        autoFocus
        data-testid="issue-owner-select"
      >
        <option value="">— unassigned —</option>
        {assignableAgents.map((agent) => (
          <option key={agent.slug} value={agent.slug}>
            @{agent.slug}
            {agent.name && agent.name !== agent.slug
              ? ` (${agent.name})`
              : ""}
          </option>
        ))}
      </select>
      <button
        type="button"
        className="issue-doc-owner-editor-cancel"
        onClick={() => {
          setIsEditing(false);
          setError(null);
        }}
        disabled={reassignMutation.isPending}
      >
        Cancel
      </button>
      {error ? (
        <span className="issue-doc-owner-editor-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

// ── Parent issue breadcrumb (shown on sub-issues) ────────────────────

interface ParentIssueBreadcrumbProps {
  parentIssueId: string;
}

function ParentIssueBreadcrumb({ parentIssueId }: ParentIssueBreadcrumbProps) {
  // Fetch the parent's title so the breadcrumb shows it inline. The
  // /tasks list is already cached for the kanban; we read the same
  // cache key (["issues","list"]) for a free hit when available, and
  // fall back to the task id when the parent is filtered out (e.g.
  // it lives in a channel the viewer can't see).
  const tasksQuery = useQuery({
    queryKey: ["issues", "list"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    staleTime: 5_000,
  });
  const parent = tasksQuery.data?.tasks.find((t) => t.id === parentIssueId);
  const label = parent?.title ?? parentIssueId;

  function openParent() {
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: parentIssueId },
    });
  }

  return (
    <button
      type="button"
      className="issue-doc-parent-breadcrumb"
      onClick={openParent}
      data-testid="issue-parent-breadcrumb"
      aria-label={`Open parent issue ${parentIssueId}`}
    >
      <span className="issue-doc-parent-breadcrumb-icon" aria-hidden="true">
        ↑
      </span>
      <span className="issue-doc-parent-breadcrumb-label">Parent</span>
      <span className="issue-doc-parent-breadcrumb-id">{parentIssueId}</span>
      <span className="issue-doc-parent-breadcrumb-title">{label}</span>
    </button>
  );
}

// ── Reopen button (rejected / approved Issues) ───────────────────────

interface ReopenIssueButtonProps {
  taskId: string;
  channel: string;
  onReopened: () => void;
}

function ReopenIssueButton({
  taskId,
  channel,
  onReopened,
}: ReopenIssueButtonProps) {
  const [error, setError] = useState<string | null>(null);

  const reopenMutation = useMutation({
    mutationFn: () => reopenIssue(taskId, channel),
    onSuccess: () => {
      setError(null);
      onReopened();
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not reopen.");
    },
  });

  return (
    <div className="issue-doc-reopen">
      <button
        type="button"
        className="issue-doc-reopen-button"
        onClick={() => reopenMutation.mutate()}
        disabled={reopenMutation.isPending}
        data-testid="reopen-issue-button"
      >
        {reopenMutation.isPending ? "Reopening…" : "Reopen issue"}
      </button>
      {error ? (
        <span className="issue-doc-reopen-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

// ── State-aware Issue action toolbar ─────────────────────────────────

interface IssueActionToolbarProps {
  taskId: string;
  channel: string;
  lifecycleState: LifecycleState;
  onAfterAction: () => void;
}

interface ActionDef {
  /** Broker action verb (matches TaskStatusAction). */
  action: TaskStatusAction;
  /** Button label the human sees. */
  label: string;
  /** Visual variant: primary (positive next step), danger (terminal/scary), neutral (everything else). */
  variant: "primary" | "danger" | "neutral";
  /** When true, prompt the human for a 1-line reason before firing. */
  requiresReason?: boolean;
  /** Placeholder for the reason prompt. */
  reasonHint?: string;
}

/** Returns the action set valid for the current lifecycle state. The
 *  set is intentionally small — only show actions that have a clear
 *  resolution path for the human. Pure presentation logic; the broker
 *  is the source of truth for what's actually allowed (the hybrid
 *  CEO/owner gate in Slice 7). */
function actionsForState(state: LifecycleState): ActionDef[] {
  switch (state) {
    case "drafting":
      // Approve & Start has its own button (special verb postDecision).
      // From drafting we offer Cancel as the close path.
      return [
        {
          action: "cancel",
          label: "Cancel issue",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "intake":
    case "ready":
      return [
        {
          action: "review",
          label: "Mark ready for review",
          variant: "primary",
        },
        {
          action: "block",
          label: "Block",
          variant: "neutral",
          requiresReason: true,
          reasonHint: "What's blocking this?",
        },
        {
          action: "cancel",
          label: "Cancel",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "running":
    case "changes_requested":
      return [
        {
          action: "submit_for_review",
          label: "Submit for review",
          variant: "primary",
        },
        {
          action: "complete",
          label: "Mark done",
          variant: "primary",
        },
        {
          action: "block",
          label: "Block",
          variant: "neutral",
          requiresReason: true,
          reasonHint: "What's blocking this?",
        },
        {
          action: "cancel",
          label: "Cancel",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "blocked_on_pr_merge":
      // Blocked tasks are agent-internal blockers. The human only
      // sees this surface on the Issue detail page (the Inbox
      // intentionally hides blocked items per the product model:
      // human input goes through human_interview requests instead).
      // We still offer a manual unblock for the rare case where the
      // human knows they fixed the underlying issue out-of-band
      // (paid a bill, reconnected an account) — no reason required
      // since they probably can't articulate the agent's blocker.
      return [
        {
          action: "resume",
          label: "Force unblock",
          variant: "neutral",
        },
        {
          action: "cancel",
          label: "Cancel",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "review":
    case "decision":
      return [
        {
          action: "approve",
          label: "Approve",
          variant: "primary",
        },
        {
          action: "request_changes",
          label: "Request changes",
          variant: "neutral",
          requiresReason: true,
          reasonHint: "What needs to change?",
        },
      ];
    case "approved":
    case "rejected":
      // Terminal — actions handled by the Reopen button (separate
      // component) and read-only otherwise.
      return [];
    default:
      return [];
  }
}

function IssueActionToolbar({
  taskId,
  channel,
  lifecycleState,
  onAfterAction,
}: IssueActionToolbarProps) {
  const [pendingReason, setPendingReason] = useState<{
    action: ActionDef;
    reason: string;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);

  const isDrafting = lifecycleState === "drafting";
  const isTerminal =
    lifecycleState === "approved" || lifecycleState === "rejected";

  const statusMutation = useMutation({
    mutationFn: (input: { action: TaskStatusAction; reason?: string }) =>
      updateTaskStatus(taskId, input.action, channel, "human", {
        overrideReason: input.reason,
      }),
    onSuccess: () => {
      setPendingReason(null);
      setError(null);
      onAfterAction();
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Action failed.");
    },
  });

  const actions = actionsForState(lifecycleState);

  function fire(action: ActionDef) {
    setError(null);
    if (action.requiresReason) {
      setPendingReason({ action, reason: "" });
      return;
    }
    statusMutation.mutate({ action: action.action });
  }

  function submitReason() {
    if (!pendingReason) return;
    const reason = pendingReason.reason.trim();
    if (!reason) {
      setError("Reason is required for this action.");
      return;
    }
    statusMutation.mutate({
      action: pendingReason.action.action,
      reason,
    });
  }

  return (
    <div className="issue-action-toolbar">
      {/* Approve & Start (drafting only) — uses postDecision via the
        * existing button so the special drafting→running transition
        * keeps its current behavior (Slice 1). */}
      {isDrafting ? (
        <ApproveAndStartButton taskId={taskId} onApproved={onAfterAction} />
      ) : null}

      {actions.map((act) => (
        <button
          key={act.action}
          type="button"
          className={`issue-action-button issue-action-button--${act.variant}`}
          onClick={() => fire(act)}
          disabled={statusMutation.isPending}
          data-testid={`action-${act.action}`}
        >
          {act.label}
        </button>
      ))}

      {/* Reopen for terminal states — separate component because it
        * uses the dedicated reopen endpoint. */}
      {isTerminal ? (
        <ReopenIssueButton
          taskId={taskId}
          channel={channel}
          onReopened={onAfterAction}
        />
      ) : null}

      {/* Inline reason prompt for actions that need a 1-line context. */}
      {pendingReason ? (
        <div className="issue-action-reason-row">
          <input
            type="text"
            className="issue-action-reason-input"
            value={pendingReason.reason}
            placeholder={pendingReason.action.reasonHint ?? "Reason"}
            onChange={(event) =>
              setPendingReason({
                action: pendingReason.action,
                reason: event.target.value,
              })
            }
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submitReason();
              } else if (event.key === "Escape") {
                event.preventDefault();
                setPendingReason(null);
              }
            }}
            autoFocus
            data-testid="action-reason-input"
          />
          <button
            type="button"
            className="issue-action-button issue-action-button--primary"
            onClick={submitReason}
            disabled={statusMutation.isPending}
          >
            {statusMutation.isPending
              ? "…"
              : pendingReason.action.label}
          </button>
          <button
            type="button"
            className="issue-action-button issue-action-button--neutral"
            onClick={() => setPendingReason(null)}
            disabled={statusMutation.isPending}
          >
            Cancel
          </button>
        </div>
      ) : null}

      {error ? (
        <span className="issue-action-error" role="alert">
          {error}
        </span>
      ) : null}

      {/* CloseIssueButton is intentionally NOT shown here — its old
        * meaning ("reject") is now subsumed into Cancel for non-terminal
        * states and is irrelevant for terminal ones. The void below
        * silences the unused-import warning. */}
      {false ? (
        <CloseIssueButton taskId={taskId} onClosed={onAfterAction} />
      ) : null}
    </div>
  );
}
