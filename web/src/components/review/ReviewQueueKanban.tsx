import { useEffect, useMemo, useState } from "react";

import {
  fetchReviews,
  type ReviewItem,
  type ReviewState,
  subscribeNotebookEvents,
  updateReviewState,
} from "../../api/notebook";
import ReviewColumn from "./ReviewColumn";
import ReviewDetail from "./ReviewDetail";
import "../../styles/notebook.css";

/** `/reviews` 5-column Kanban + detail drawer backed by the broker review log. */

type ReviewColumnState = Extract<
  ReviewState,
  "pending" | "in-review" | "changes-requested" | "approved" | "archived"
>;

const STATE_ORDER: ReviewColumnState[] = [
  "pending",
  "in-review",
  "changes-requested",
  "approved",
  "archived",
];
const STATE_TITLE: Record<ReviewColumnState, string> = {
  pending: "Pending",
  "in-review": "In review",
  "changes-requested": "Changes requested",
  approved: "Approved",
  archived: "Archived",
};

function reviewColumnState(state: ReviewState): ReviewColumnState {
  if (state === "rejected" || state === "expired") return "archived";
  return state;
}

export default function ReviewQueueKanban() {
  const [reviews, setReviews] = useState<ReviewItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchReviews()
      .then((r) => {
        if (!cancelled) setReviews(r);
      })
      .catch((err: unknown) => {
        if (!cancelled)
          setError(err instanceof Error ? err.message : "Failed to load");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [refreshTick]);

  useEffect(() => {
    const unsub = subscribeNotebookEvents((ev) => {
      if (ev.type === "review:state_change") {
        setRefreshTick((n) => n + 1);
      }
    });
    return unsub;
  }, []);

  const grouped = useMemo(() => {
    const out: Record<ReviewColumnState, ReviewItem[]> = {
      pending: [],
      "in-review": [],
      "changes-requested": [],
      approved: [],
      archived: [],
    };
    for (const r of reviews) out[reviewColumnState(r.state)].push(r);
    return out;
  }, [reviews]);

  const active = activeId
    ? (reviews.find((r) => r.id === activeId) ?? null)
    : null;

  const handleStateChange = async (
    id: string,
    nextState: ReviewState,
    rationale?: string,
  ): Promise<boolean> => {
    setActionError(null);
    // Optimistic update.
    setReviews((prev) =>
      prev.map((r) => (r.id === id ? { ...r, state: nextState } : r)),
    );
    try {
      await updateReviewState(id, nextState, { rationale });
      return true;
    } catch (err: unknown) {
      // Rollback AND say so. The live v3 run returned 400/409/500 across
      // the whole queue with zero feedback — the human watched a frozen
      // board and concluded every review control was dead ([18:14:39]).
      setActionError(
        err instanceof Error ? err.message : "Review action failed.",
      );
      setRefreshTick((n) => n + 1);
      return false;
    }
  };

  const totalCounts = `${reviews.length} reviews · ${grouped.pending.length + grouped["in-review"].length + grouped["changes-requested"].length} open · ${grouped.approved.length} recently approved`;

  return (
    <div className="notebook-surface" data-testid="review-queue-surface">
      {/* Skip link uses programmatic focus instead of href="#nb-review-main"
       * because the app runs under hash-history routing — an anchor hash would
       * clobber /#/reviews and navigate the user off the surface. */}
      <button
        type="button"
        className="nb-skip-link"
        onClick={() => {
          const main = document.getElementById("nb-review-main");
          if (main instanceof HTMLElement) {
            main.focus();
            main.scrollIntoView({ block: "start" });
          }
        }}
      >
        Skip to review queue
      </button>
      <main
        id="nb-review-main"
        className="nb-review-queue"
        aria-label="Review queue"
        tabIndex={-1}
      >
        <header className="nb-review-queue-header">
          <h1 className="nb-review-queue-title">Reviews</h1>
          <div className="nb-review-queue-meta">{totalCounts}</div>
        </header>
        {actionError ? (
          <p
            className="nb-error"
            role="alert"
            data-testid="nb-review-board-action-error"
          >
            Could not update the review: {actionError}
          </p>
        ) : null}
        {loading ? (
          <div className="nb-loading" aria-busy="true">
            Loading reviews…
          </div>
        ) : error ? (
          <>
            <p className="nb-error" role="alert">
              Error: {error}
            </p>
            <button
              type="button"
              className="nb-retry-btn"
              onClick={() => setRefreshTick((n) => n + 1)}
            >
              Retry
            </button>
          </>
        ) : (
          <ul className="nb-review-columns">
            {STATE_ORDER.map((state) => (
              <ReviewColumn
                key={state}
                title={STATE_TITLE[state]}
                items={grouped[state]}
                activeId={activeId}
                onOpenCard={(id) => setActiveId(id)}
              />
            ))}
          </ul>
        )}
      </main>
      {active ? (
        <ReviewDetail
          review={active}
          onClose={() => setActiveId(null)}
          actionError={actionError}
          onApprove={(id) => {
            void handleStateChange(id, "approved").then((ok) => {
              if (ok) setActiveId(null);
            });
          }}
          onRequestChanges={(id, rationale) => {
            void handleStateChange(id, "changes-requested", rationale).then(
              (ok) => {
                if (ok) setActiveId(null);
              },
            );
          }}
        />
      ) : null}
    </div>
  );
}
