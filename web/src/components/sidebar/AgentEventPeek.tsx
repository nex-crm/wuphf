import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { formatRelative } from "../../lib/relativeTime";
import type { StoredActivitySnapshot } from "../../stores/app";

export interface AgentEventPeekProps {
  slug: string;
  agentName: string;
  agentRole?: string;
  open: boolean;
  current: StoredActivitySnapshot | undefined;
  history: StoredActivitySnapshot[];
  anchorRef: React.RefObject<HTMLElement | null>;
  onClose: () => void;
  onOpenWorkspace: () => void;
}

interface Position {
  top: number;
  left: number;
}

const PEEK_WIDTH = 320;
const PEEK_OFFSET = 8;

function computePosition(anchor: HTMLElement): Position {
  const { top, right, left: rectLeft } = anchor.getBoundingClientRect();
  const rightEdge = right + PEEK_OFFSET + PEEK_WIDTH;
  // Flip to left if the peek would extend beyond the right viewport edge.
  const left =
    rightEdge > window.innerWidth
      ? rectLeft - PEEK_OFFSET - PEEK_WIDTH
      : right + PEEK_OFFSET;
  return { top, left };
}

function kindLabel(kind: StoredActivitySnapshot["kind"]): string {
  if (kind === "milestone") return "milestone";
  if (kind === "stuck") return "stuck";
  return "routine";
}

export function AgentEventPeek({
  slug,
  agentName,
  agentRole,
  open,
  current,
  history,
  anchorRef,
  onClose,
  onOpenWorkspace,
}: AgentEventPeekProps) {
  const [now, setNow] = useState<number>(() => Date.now());
  const [pos, setPos] = useState<Position>({ top: 0, left: 0 });
  const dialogRef = useRef<HTMLDivElement | null>(null);

  // Own 1Hz tick — the peek is mount/unmount so it owns its own clock.
  useEffect(() => {
    if (!open) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [open]);

  // Position from anchor whenever open flips true or on scroll/resize.
  useEffect(() => {
    if (!open) return;

    function reposition() {
      if (anchorRef.current) {
        setPos(computePosition(anchorRef.current));
      }
    }

    reposition();
    window.addEventListener("resize", reposition);
    window.addEventListener("scroll", reposition, true);
    return () => {
      window.removeEventListener("resize", reposition);
      window.removeEventListener("scroll", reposition, true);
    };
  }, [open, anchorRef]);

  // Focus the dialog when it opens.
  useEffect(() => {
    if (open && dialogRef.current) {
      dialogRef.current.focus();
    }
  }, [open]);

  // Keyboard: Escape → close, Enter → open workspace.
  useEffect(() => {
    if (!open) return;

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      } else if (e.key === "Enter") {
        onOpenWorkspace();
      }
    }

    const el = dialogRef.current;
    el?.addEventListener("keydown", onKeyDown);
    return () => el?.removeEventListener("keydown", onKeyDown);
  }, [open, onClose, onOpenWorkspace]);

  // Outside mousedown → close.
  useEffect(() => {
    if (!open) return;

    function onMouseDown(e: MouseEvent) {
      const target = e.target as Node;
      if (dialogRef.current?.contains(target)) return;
      if (anchorRef.current?.contains(target)) return;
      onClose();
    }

    document.addEventListener("mousedown", onMouseDown);
    return () => document.removeEventListener("mousedown", onMouseDown);
  }, [open, anchorRef, onClose]);

  if (!open) return null;

  const isStuck = current?.kind === "stuck";
  const currentLabel = current ? kindLabel(current.kind) : null;
  const showDetailBlock =
    !!current &&
    typeof current.detail === "string" &&
    current.detail.length > 0 &&
    current.detail !== current.activity;

  // Recent list: if stuck, prepend the current as a "BLOCKED:" pin,
  // then show up to 6 history entries. Both branches require a current
  // snapshot, so the list collapses on the no-activity empty state.
  const recentEntries: Array<{
    entry: StoredActivitySnapshot;
    prefix?: string;
  }> = [];
  if (isStuck && current) {
    recentEntries.push({ entry: current, prefix: "BLOCKED:" });
  }
  for (const h of history) {
    if (recentEntries.length >= 6) break;
    recentEntries.push({ entry: h });
  }
  const showRecentBlock = recentEntries.length > 0;
  const showEmptyState = !current;

  const avatarLetter = agentName.charAt(0).toUpperCase();

  return createPortal(
    <div
      ref={dialogRef}
      role="dialog"
      aria-labelledby={`peek-name-${slug}`}
      aria-describedby={showDetailBlock ? `peek-current-${slug}` : undefined}
      className="sidebar-agent-peek"
      data-stuck={isStuck ? "true" : undefined}
      tabIndex={-1}
      style={{ top: pos.top, left: pos.left }}
    >
      {/* Header */}
      <div className="sidebar-agent-peek-header">
        <div className="sidebar-agent-peek-avatar" aria-hidden="true">
          {avatarLetter}
        </div>
        <div className="sidebar-agent-peek-identity">
          <span id={`peek-name-${slug}`} className="sidebar-agent-peek-name">
            {agentName}
          </span>
          {!!agentRole && (
            <span className="sidebar-agent-peek-role">{agentRole}</span>
          )}
        </div>
        {isStuck && (
          <span className="sidebar-agent-peek-blocked-chip">BLOCKED</span>
        )}
      </div>

      {/* State chip + relative time — only when we have a snapshot */}
      {current && currentLabel ? (
        <div className="sidebar-agent-peek-state-row">
          <span
            className="sidebar-agent-peek-state-chip"
            data-kind={currentLabel}
          >
            {currentLabel}
          </span>
          <span className="sidebar-agent-peek-time">
            {formatRelative(current.receivedAtMs, now)}
          </span>
        </div>
      ) : null}

      {/* Current thought */}
      {showDetailBlock && current ? (
        <div id={`peek-current-${slug}`} className="sidebar-agent-peek-detail">
          {current.detail}
        </div>
      ) : null}

      {/* Empty state — no SSE event has arrived for this agent yet */}
      {showEmptyState ? (
        <div className="sidebar-agent-peek-empty" data-testid="peek-empty">
          No activity yet. This agent has not streamed an event since the
          office opened.
        </div>
      ) : null}

      {/* Recent list */}
      {showRecentBlock && (
        <div className="sidebar-agent-peek-recent-section">
          <div className="sidebar-agent-peek-recent-header">RECENT</div>
          <ul className="sidebar-agent-peek-recent" aria-label="Recent events">
            {recentEntries.map(({ entry, prefix }) => (
              <li
                key={`${entry.receivedAtMs}-${prefix ?? ""}`}
                className="sidebar-agent-peek-recent-item"
              >
                <span
                  className="sidebar-agent-peek-dot"
                  data-kind={kindLabel(entry.kind)}
                  aria-hidden="true"
                />
                <span className="sidebar-agent-peek-recent-text">
                  {prefix ? `${prefix} ` : ""}
                  {entry.activity ?? entry.detail ?? ""}
                </span>
                <span className="sidebar-agent-peek-recent-time">
                  {formatRelative(entry.receivedAtMs, now)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Footer — NOT a button; Enter key drives the action */}
      <div className="sidebar-agent-peek-footer">
        <span>&#9166; Open workspace</span>
      </div>
    </div>,
    document.body,
  );
}
