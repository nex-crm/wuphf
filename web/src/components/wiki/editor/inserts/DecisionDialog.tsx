/**
 * Modal form for the decision-block insert.
 *
 * Captures title, rationale, ISO date, and a comma-separated list of
 * alternatives. Defaults the date field to today's local-time ISO date
 * (`YYYY-MM-DD`) so most authors only need to fill out the title and
 * rationale to ship a decision record.
 */

import { useEffect, useRef, useState } from "react";

import { buildDecisionBlock, type DecisionDraft } from "./markdownShapes";

export interface DecisionDialogProps {
  onConfirm: (block: string) => void;
  onCancel: () => void;
}

function todayLocalIsoDate(): string {
  const d = new Date();
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

export function DecisionDialog({
  onConfirm,
  onCancel,
}: DecisionDialogProps): React.ReactElement {
  const [draft, setDraft] = useState<DecisionDraft>({
    title: "",
    rationale: "",
    date: todayLocalIsoDate(),
    alternatives: [],
  });
  const [alternativesText, setAlternativesText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const titleRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    titleRef.current?.focus();
  }, []);

  function handleSubmit(e?: React.FormEvent): void {
    e?.preventDefault();
    if (!draft.title.trim()) {
      setError("Title is required.");
      return;
    }
    if (!draft.rationale.trim()) {
      setError("Rationale is required.");
      return;
    }
    if (!/^\d{4}-\d{2}-\d{2}$/.test(draft.date.trim())) {
      setError("Date must be YYYY-MM-DD.");
      return;
    }
    const alts = alternativesText
      .split(",")
      .map((a) => a.trim())
      .filter((a) => a.length > 0);
    const block = buildDecisionBlock({
      ...draft,
      alternatives: alts,
    });
    onConfirm(block);
  }

  return (
    <div
      className="wk-modal-backdrop"
      data-testid="wk-decision-dialog-backdrop"
      role="dialog"
      aria-modal="true"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onCancel();
        }
      }}
    >
      <form
        className="wk-modal wk-insert-dialog"
        data-testid="wk-decision-dialog"
        onSubmit={handleSubmit}
      >
        <h2>Insert decision block</h2>
        <label htmlFor="wk-decision-title" className="wk-editor-label">
          Title
        </label>
        <input
          id="wk-decision-title"
          ref={titleRef}
          value={draft.title}
          onChange={(e) => setDraft({ ...draft, title: e.target.value })}
          data-testid="wk-decision-title"
        />
        <label htmlFor="wk-decision-date" className="wk-editor-label">
          Date
        </label>
        <input
          id="wk-decision-date"
          type="date"
          value={draft.date}
          onChange={(e) => setDraft({ ...draft, date: e.target.value })}
          data-testid="wk-decision-date"
        />
        <label htmlFor="wk-decision-rationale" className="wk-editor-label">
          Rationale
        </label>
        <textarea
          id="wk-decision-rationale"
          value={draft.rationale}
          rows={4}
          onChange={(e) => setDraft({ ...draft, rationale: e.target.value })}
          data-testid="wk-decision-rationale"
        />
        <label htmlFor="wk-decision-alternatives" className="wk-editor-label">
          Alternatives considered (comma-separated)
        </label>
        <input
          id="wk-decision-alternatives"
          value={alternativesText}
          onChange={(e) => setAlternativesText(e.target.value)}
          placeholder="Option A, Option B"
          data-testid="wk-decision-alternatives"
        />
        {error ? (
          <div
            className="wk-editor-banner wk-editor-banner--error"
            role="alert"
            data-testid="wk-decision-error"
          >
            {error}
          </div>
        ) : null}
        <div className="wk-insert-dialog__actions">
          <button
            type="button"
            data-testid="wk-decision-confirm"
            className="wk-editor-save"
            onClick={() => handleSubmit()}
          >
            Insert decision
          </button>
          <button
            type="button"
            onClick={onCancel}
            className="wk-editor-cancel"
            data-testid="wk-decision-cancel"
          >
            Cancel
          </button>
        </div>
      </form>
    </div>
  );
}
