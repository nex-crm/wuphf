/**
 * CeoChecklist — list of checkboxes + submit chip.
 * CeoTeamTrim is an alias of this component with team-specific chrome.
 *
 * Used for: team trim (blueprint path).
 *
 * Keyboard model per spec:
 *   Tab    → item
 *   Space  → toggle checkbox
 *   Enter  → Submit button
 *
 * All item labels treated as plain text (never innerHTML).
 */

import { useRef, useState } from "react";

import type {
  CardStage,
  CeoChecklistPayload,
  CeoTeamTrimPayload,
} from "../../onboarding/types";

interface CeoChecklistProps {
  payload: CeoChecklistPayload | CeoTeamTrimPayload;
  stage: CardStage;
  committedValue?: string[];
  onSubmit: (field: string, value: string[]) => void;
  /** Ref for focus management — parent focuses first item after prior card commits. */
  autoFocusRef?: React.RefObject<HTMLElement | null>;
}

export function CeoChecklist({
  payload,
  stage,
  committedValue,
  onSubmit,
  autoFocusRef,
}: CeoChecklistProps) {
  const [checked, setChecked] = useState<Set<string>>(
    () =>
      new Set(
        payload.items
          .filter((item) => item.default_checked !== false)
          .map((item) => item.id),
      ),
  );
  const submitRef = useRef<HTMLButtonElement>(null);

  // Merge externally-provided ref so the parent can focus the submit button.
  if (autoFocusRef && submitRef.current !== null) {
    (autoFocusRef as React.MutableRefObject<HTMLElement | null>).current =
      submitRef.current;
  }

  if (stage === "committed") {
    const labels =
      committedValue ??
      payload.items
        .filter((item) => checked.has(item.id))
        .map((item) => item.label);
    return (
      <div className="ceo-card ceo-card--committed" role="status">
        <span className="ceo-card-committed-text">
          &#10003; {labels.join(", ")}
        </span>
      </div>
    );
  }

  const toggle = (id: string) => {
    if (stage === "submitting") return;
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleSubmit = () => {
    if (stage === "submitting") return;
    onSubmit(payload.field, [...checked]);
  };

  return (
    <div className="ceo-card ceo-card--checklist" data-testid="ceo-checklist">
      {payload.label ? (
        <div className="ceo-card-label">{payload.label}</div>
      ) : null}
      <ul className="ceo-checklist-items">
        {payload.items.map((item) => (
          <li key={item.id} className="ceo-checklist-item">
            <label className="ceo-checklist-label">
              <input
                type="checkbox"
                className="ceo-checklist-checkbox"
                checked={checked.has(item.id)}
                disabled={stage === "submitting"}
                onChange={() => toggle(item.id)}
                aria-label={item.label}
              />
              {/* Render as text — never innerHTML */}
              <span className="ceo-checklist-item-text">{item.label}</span>
            </label>
          </li>
        ))}
      </ul>
      <div className="ceo-card-actions">
        <button
          ref={submitRef}
          type="button"
          className="btn btn-primary btn-sm ceo-card-submit"
          disabled={stage === "submitting" || checked.size === 0}
          onClick={handleSubmit}
        >
          {stage === "submitting" ? (
            <span className="ceo-card-spinner" aria-hidden="true" />
          ) : null}
          {stage === "submitting"
            ? "Saving…"
            : (payload.submit_label ?? "Submit")}
        </button>
      </div>
    </div>
  );
}
