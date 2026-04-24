import { useEffect, useRef, useState } from "react";

import { type LintFinding, resolveContradiction } from "../../api/wiki";

/**
 * ResolveContradictionModal — modal for resolving a lint contradiction finding.
 *
 * Pattern: mirrors NewArticleModal.tsx (same backdrop + modal CSS, same
 * submit / cancel layout, same error display).
 *
 * The finding's resolve_actions array carries three pre-formatted strings:
 *   [0] "Fact A (id: …): <text>"
 *   [1] "Fact B (id: …): <text>"
 *   [2] "Both"
 *
 * The user picks one of three buttons. On success the modal calls onResolved()
 * so the parent (WikiLint) refreshes the report. Pressing Escape closes
 * without submitting (spec: §5 modal UX).
 */
interface ResolveContradictionModalProps {
  finding: LintFinding;
  findingIdx: number;
  reportDate: string;
  onClose: () => void;
  onResolved: () => void;
}

export default function ResolveContradictionModal({
  finding,
  findingIdx,
  reportDate,
  onClose,
  onResolved,
}: ResolveContradictionModalProps) {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const backdropRef = useRef<HTMLDivElement>(null);

  // Escape key closes without submitting.
  useEffect(() => {
    function onKeyDown(ev: KeyboardEvent) {
      if (ev.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  // Click outside (on backdrop) closes without submitting.
  function handleBackdropClick(ev: React.MouseEvent<HTMLDivElement>) {
    if (ev.target === backdropRef.current) {
      onClose();
    }
  }

  async function handlePick(winner: "A" | "B" | "Both") {
    setError(null);
    setSubmitting(true);
    try {
      await resolveContradiction({
        report_date: reportDate,
        finding_idx: findingIdx,
        finding,
        winner,
      });
      onResolved();
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to resolve contradiction.",
      );
    } finally {
      setSubmitting(false);
    }
  }

  // resolve_actions is always [factAText, factBText, "Both"] for contradictions.
  const factA = finding.resolve_actions?.[0] ?? "Fact A";
  const factB = finding.resolve_actions?.[1] ?? "Fact B";

  return (
    <div
      className="wk-modal-backdrop"
      data-testid="wk-resolve-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-resolve-title"
      ref={backdropRef}
      onClick={handleBackdropClick}
    >
      <div className="wk-modal">
        <h2 id="wk-resolve-title">Resolve contradiction</h2>

        <p className="wk-editor-help">
          Entity: <strong>{finding.entity_slug ?? "(unknown)"}</strong>
          {finding.fact_ids && finding.fact_ids.length > 0 && (
            <> &mdash; facts: {finding.fact_ids.join(", ")}</>
          )}
        </p>

        <div className="wk-resolve-facts" aria-label="Conflicting facts">
          <div className="wk-resolve-fact">
            <span className="wk-resolve-fact-label">Fact A</span>
            <p>{factA}</p>
          </div>
          <div className="wk-resolve-fact">
            <span className="wk-resolve-fact-label">Fact B</span>
            <p>{factB}</p>
          </div>
        </div>

        {error && (
          <div
            className="wk-editor-banner wk-editor-banner--error"
            role="alert"
          >
            {error}
          </div>
        )}

        <p className="wk-editor-help">
          Pick which fact is authoritative. The other will be marked superseded.
          Choose <em>Both</em> to keep both as non-contradictory.
        </p>

        <div className="wk-editor-actions">
          <button
            type="button"
            className="wk-editor-save"
            data-testid="wk-resolve-pick-a"
            onClick={() => handlePick("A")}
            disabled={submitting}
          >
            {submitting ? "Resolving…" : "Fact A"}
          </button>
          <button
            type="button"
            className="wk-editor-save"
            data-testid="wk-resolve-pick-b"
            onClick={() => handlePick("B")}
            disabled={submitting}
          >
            {submitting ? "Resolving…" : "Fact B"}
          </button>
          <button
            type="button"
            className="wk-editor-save"
            data-testid="wk-resolve-pick-both"
            onClick={() => handlePick("Both")}
            disabled={submitting}
          >
            {submitting ? "Resolving…" : "Both"}
          </button>
          <button
            type="button"
            className="wk-editor-cancel"
            data-testid="wk-resolve-cancel"
            onClick={onClose}
            disabled={submitting}
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}
