/**
 * CeoChecklist — list of checkboxes + submit chip.
 * CeoTeamTrim is an alias of this component with team-specific chrome.
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
      if (next.has(id)) next.delete(id);
      else next.add(id);
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
        {payload.items.map((item) => {
          const isChecked = checked.has(item.id);
          return (
            <li key={item.id} className="ceo-checklist-item">
              <label
                className="ceo-checklist-label"
                data-checked={isChecked ? "true" : "false"}
              >
                <input
                  type="checkbox"
                  className="ceo-checklist-checkbox"
                  checked={isChecked}
                  disabled={stage === "submitting"}
                  onChange={() => toggle(item.id)}
                  aria-label={item.label}
                />
                <svg
                  className="ceo-checklist-tick"
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <polyline points="20 6 9 17 4 12" />
                </svg>
                <span className="ceo-checklist-item-text">{item.label}</span>
              </label>
            </li>
          );
        })}
      </ul>
      <div className="ceo-card-actions">
        <button
          ref={submitRef}
          type="button"
          className="btn btn-primary ceo-card-submit"
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
