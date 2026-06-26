import { useCallback, useEffect, useState } from "react";

import {
  fetchAgentEntries,
  type NotebookAgentSummary,
  type NotebookEntry,
  type ReviewItem,
  type ReviewState,
  updateReviewState,
} from "../../api/notebook";
import AuthorShelfSidebar from "./AuthorShelfSidebar";
import NotebookEntryView from "./NotebookEntry";

/**
 * `/notebooks/{agent-slug}[/{entry-slug}]` — two-column view. Left: dated
 * log of this agent's entries. Right: the selected entry rendered in full
 * (defaults to most recent).
 *
 * When the selected entry has an actionable review (pending / in-review /
 * changes-requested), the entry's inline review thread doubles as the
 * review surface: Approve and Request changes act in place, so a reviewer
 * who clicked through from the Reviews queue never has to leave the
 * notebook item they're judging.
 */

const ACTIONABLE_REVIEW_STATES: ReadonlySet<ReviewState> = new Set([
  "pending",
  "in-review",
  "changes-requested",
]);

interface AgentNotebookViewProps {
  agentSlug: string;
  entrySlug?: string | null;
  onNavigateCatalog: () => void;
  onSelectEntry: (entrySlug: string | null) => void;
  onNavigateWiki?: (wikiPath: string) => void;
}

export default function AgentNotebookView({
  agentSlug,
  entrySlug,
  onNavigateCatalog,
  onSelectEntry,
  onNavigateWiki,
}: AgentNotebookViewProps) {
  const [agent, setAgent] = useState<NotebookAgentSummary | null>(null);
  const [entries, setEntries] = useState<NotebookEntry[]>([]);
  const [reviews, setReviews] = useState<ReviewItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reviewActionError, setReviewActionError] = useState<string | null>(
    null,
  );

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchAgentEntries(agentSlug)
      .then((res) => {
        setAgent(res.agent);
        setEntries(res.entries);
        setReviews(res.reviews);
      })
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Failed to load notebook",
        );
      })
      .finally(() => setLoading(false));
  }, [agentSlug]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchAgentEntries(agentSlug)
      .then((res) => {
        if (cancelled) return;
        setAgent(res.agent);
        setEntries(res.entries);
        setReviews(res.reviews);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof Error ? err.message : "Failed to load notebook",
        );
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [agentSlug]);

  const handleReviewAction = useCallback(
    async (reviewId: string, state: ReviewState, rationale?: string) => {
      setReviewActionError(null);
      try {
        await updateReviewState(reviewId, state, { rationale });
        load();
      } catch (err: unknown) {
        // Never fail silently — the live v3 run watched a frozen review
        // surface and concluded every control was dead ([18:14:39]).
        setReviewActionError(
          err instanceof Error ? err.message : "Review action failed.",
        );
      }
    },
    [load],
  );

  if (loading) {
    return (
      <div className="nb-layout">
        <div className="nb-shelf">
          <span className="nb-skeleton" />
          <span className="nb-skeleton" />
        </div>
        <div className="nb-article" aria-busy="true">
          <div className="nb-loading">Loading notebook…</div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="nb-article">
        <p className="nb-error" role="alert">
          Error: {error}
        </p>
        <button type="button" className="nb-retry-btn" onClick={load}>
          Retry
        </button>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="nb-article">
        <p className="nb-error">Agent not found: {agentSlug}</p>
        <button
          type="button"
          className="nb-retry-btn"
          onClick={onNavigateCatalog}
        >
          Back to bookshelf
        </button>
      </div>
    );
  }

  // Pick the entry to render: explicit slug → matching entry; else first.
  const activeEntry: NotebookEntry | null = entrySlug
    ? (entries.find((e) => e.entry_slug === entrySlug) ?? null)
    : (entries[0] ?? null);

  // The entry's live review, when there is one a reviewer can still act on.
  const activeReview: ReviewItem | null = activeEntry
    ? (reviews.find(
        (review) =>
          review.entry_slug === activeEntry.entry_slug &&
          ACTIONABLE_REVIEW_STATES.has(review.state),
      ) ?? null)
    : null;

  return (
    <div className="nb-layout">
      <AuthorShelfSidebar
        agent={agent}
        entries={entries.map((e) => ({
          entry_slug: e.entry_slug,
          title: e.title,
          last_edited_ts: e.last_edited_ts,
          status: e.status,
        }))}
        currentEntrySlug={activeEntry?.entry_slug ?? null}
        onSelect={(slug) => onSelectEntry(slug)}
      />
      {activeEntry ? (
        <NotebookEntryView
          entry={activeEntry}
          reviewComments={activeReview?.comments}
          reviewState={activeReview?.state}
          reviewActionError={reviewActionError}
          onApproveReview={
            activeReview
              ? () => {
                  void handleReviewAction(activeReview.id, "approved");
                }
              : undefined
          }
          onRequestChangesReview={
            activeReview
              ? (rationale) => {
                  void handleReviewAction(
                    activeReview.id,
                    "changes-requested",
                    rationale,
                  );
                }
              : undefined
          }
          onNavigateCatalog={onNavigateCatalog}
          onNavigateAgent={() => onSelectEntry(null)}
          onNavigateWiki={onNavigateWiki}
        />
      ) : (
        <div className="nb-empty-prompt">
          <p>No entries yet — {agent.name} has not written anything.</p>
        </div>
      )}
    </div>
  );
}
