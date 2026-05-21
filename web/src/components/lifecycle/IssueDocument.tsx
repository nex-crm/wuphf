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

import { useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { get, sseURL } from "../../api/client";
import { postDecision, postTaskComment } from "../../api/lifecycle";
import { getOfficeTasks, type Task } from "../../api/tasks";
import {
  messageMarkdownComponents,
  messageRemarkPlugins,
} from "../../lib/messageMarkdown";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { PixelAvatar } from "../ui/PixelAvatar";
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
  lifecycleState: LifecycleState;
  spec: IssueSpec;
  comments: IssueComment[];
  channel: string;
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
  return (
    resolveAliasedField(packet, taskRecord, "channel", "channel")?.trim() ||
    taskHint?.channel?.trim() ||
    "general"
  );
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

  return {
    taskId,
    title,
    lifecycleState,
    spec,
    comments,
    channel: resolveIssueChannel(r, taskRecord, taskHint),
    ownerSlug:
      resolveAliasedField(r, taskRecord, "ownerSlug", "owner") ??
      taskHint?.owner,
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
        <span className="issue-comment-author" title={label}>
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
  const trimmedComment = commentBody.trim();

  const commentMutation = useMutation({
    mutationFn: (body: string) => postTaskComment(taskId, channel, body),
    onSuccess: () => {
      setCommentBody("");
      setCommentError(null);
      onCommentPosted();
    },
    onError: (err: unknown) => {
      setCommentError(
        err instanceof Error ? err.message : "Could not post comment.",
      );
    },
  });

  function submitComment(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!trimmedComment || commentMutation.isPending) return;
    setCommentError(null);
    commentMutation.mutate(trimmedComment);
  }

  return (
    <section
      className="issue-doc-comments"
      aria-label="Comments"
      aria-live="polite"
      ref={timelineRef}
    >
      <h3 className="issue-comments-heading">Comments</h3>
      {comments.length === 0 ? (
        <p className="issue-comments-empty" data-testid="issue-comments-empty">
          No comments yet.
        </p>
      ) : (
        <div className="issue-comments-list" data-testid="issue-comments-list">
          {comments.map((c) => (
            <CommentItem key={c.id} comment={c} />
          ))}
        </div>
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
        <textarea
          id="issue-comment"
          className="issue-comment-input"
          value={commentBody}
          onChange={(event) => {
            setCommentBody(event.target.value);
            if (commentError) setCommentError(null);
          }}
          placeholder="Ask a question, clarify scope, or leave review notes."
          rows={4}
          disabled={commentMutation.isPending}
          data-testid="issue-comment-input"
        />
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
      {/* Sticky header: status pill + title */}
      <header className="issue-doc-header issue-doc-header--sticky">
        <div className="issue-doc-header-row">
          <LifecycleStatePill state={doc.lifecycleState} />
          <h2 className="issue-doc-title">{doc.title}</h2>
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
          {isDrafting ? (
            <ApproveAndStartButton
              taskId={taskId}
              onApproved={() => {
                // Invalidate the issue query so the status pill updates
                // to "running" once the broker confirms the transition.
                void queryClient.invalidateQueries({
                  queryKey: ["issue", taskId],
                });
              }}
            />
          ) : null}
        </div>
      </header>

      {/* Body: spec sections + comments timeline */}
      <div className="issue-doc-body">
        {/* Spec sections — aria-live for streaming draft announcements */}
        <SpecBody
          spec={doc.spec}
          mergedSpec={mergedSpec}
          shouldAutoCollapse={shouldAutoCollapse}
          specExpanded={specExpanded}
          isDrafting={isDrafting}
          isSectionStreaming={isSectionStreaming}
          onExpand={toggleSpec}
          onCollapse={toggleSpec}
        />

        {/* Comments timeline */}
        <CommentsTimeline
          taskId={taskId}
          channel={doc.channel}
          comments={doc.comments}
          isDrafting={isDrafting}
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
