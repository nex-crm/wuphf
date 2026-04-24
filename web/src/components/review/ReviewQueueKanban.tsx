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

/** `/reviews` 5-column Kanban + detail drawer. UI-only in Lane E. */

const STATE_ORDER: ReviewState[] = [
  "pending",
  "in-review",
  "changes-requested",
  "approved",
  "archived",
];
const STATE_TITLE: Record<ReviewState, string> = {
  pending: "Pending",
  "in-review": "In review",
  "changes-requested": "Changes requested",
  approved: "Approved",
  archived: "Archived",
};

export default function ReviewQueueKanban() {
  const [reviews, setReviews] = useState<ReviewItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [_refreshTick, setRefreshTick] = useState(0);

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
  }, []);

  useEffect(() => {
    const unsub = subscribeNotebookEvents((ev) => {
      if (ev.type === "review:state_change") {
        setRefreshTick((n) => n + 1);
      }
    });
    return unsub;
  }, []);

  const grouped = useMemo(() => {
    const out: Record<ReviewState, ReviewItem[]> = {
      pending: [],
      "in-review": [],
      "changes-requested": [],
      approved: [],
      archived: [],
    };
    for (const r of reviews) {
      if (out[r.state]) out[r.state].push(r);
    }
    return out;
  }, [reviews]);

  const active = activeId
    ? (reviews.find((r) => r.id === activeId) ?? null)
    : null;

  const handleStateChange = async (id: string, nextState: ReviewState) => {
    // Optimistic update.
    setReviews((prev) =>
      prev.map((r) => (r.id === id ? { ...r, state: nextState } : r)),
    );
    try {
      await updateReviewState(id, nextState);
    } catch {
      // Rollback on failure.
      setRefreshTick((n) => n + 1);
    }
  };

  const totalCounts = `${reviews.length} reviews · ${grouped.pending.length + grouped["in-review"].length + grouped["changes-requested"].length} open · ${grouped.approved.length} recently approved`;

  return (
    <div className="notebook-surface" data-testid="review-queue-surface">
      <a href="#nb-review-main" className="nb-skip-link">
        Skip to review queue
      </a>
      <main
        id="nb-review-main"
        className="nb-review-queue"
        aria-label="Review queue"
      >
        <header className="nb-review-queue-header">
          <h1 className="nb-review-queue-title">Reviews</h1>
          <div className="nb-review-queue-meta">{totalCounts}</div>
        </header>
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
          <div className="nb-review-columns" role="list">
            {STATE_ORDER.map((state) => (
              <ReviewColumn
                key={state}
                title={STATE_TITLE[state]}
                items={grouped[state]}
                activeId={activeId}
                onOpenCard={(id) => setActiveId(id)}
              />
            ))}
          </div>
        )}
      </main>
      {active && (
        <ReviewDetail
          review={active}
          onClose={() => setActiveId(null)}
          onApprove={(id) => {
            void handleStateChange(id, "approved");
            setActiveId(null);
          }}
          onRequestChanges={(id) => {
            void handleStateChange(id, "changes-requested");
            setActiveId(null);
          }}
        />
      )}
    </div>
  );
}
