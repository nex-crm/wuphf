/**
 * CeoFormField — label + text input + submit chip + optional inline "Skip".
 */

import { useEffect, useRef, useState } from "react";

import type { CardStage, CeoFormFieldPayload } from "../../onboarding/types";

interface CeoFormFieldProps {
  payload: CeoFormFieldPayload;
  stage: CardStage;
  committedValue?: string;
  onSubmit: (field: string, value: string) => void;
  onSkip?: (field: string) => void;
  autoFocusRef?: React.RefObject<HTMLInputElement | null>;
}

export function CeoFormField({
  payload,
  stage,
  committedValue,
  onSubmit,
  onSkip,
  autoFocusRef,
}: CeoFormFieldProps) {
  const [value, setValue] = useState(payload.default ?? "");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (autoFocusRef) {
      (
        autoFocusRef as React.MutableRefObject<HTMLInputElement | null>
      ).current = inputRef.current;
    }
  });

  useEffect(() => {
    if (stage === "pending" && inputRef.current) {
      inputRef.current.focus();
    }
  }, [stage]);

  // Committed state intentionally renders the SAME input view as
  // pending/submitting — just disabled. The previous "✓ <answer>"
  // confirmation chip was flashing briefly between cards (because the
  // sticky-suggestion swap is near-instant), and read as noise rather
  // than acknowledgement.

  const canSubmit = value.trim().length > 0 || payload.optional;

  const handleSubmit = () => {
    if (stage !== "pending") return;
    const trimmed = value.trim();
    if (!(trimmed || payload.optional)) return;
    onSubmit(payload.field, trimmed);
  };

  return (
    <div className="ceo-card ceo-card--form-field" data-testid="ceo-form-field">
      {payload.label ? (
        <label
          htmlFor={`ceo-field-${payload.field}`}
          className="ceo-card-label"
        >
          {payload.label}
        </label>
      ) : null}
      <div className="ceo-card-input-row">
        <input
          id={`ceo-field-${payload.field}`}
          ref={inputRef}
          type="text"
          className="ceo-card-input"
          value={value}
          placeholder={payload.placeholder ?? ""}
          disabled={stage !== "pending"}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              handleSubmit();
            }
          }}
          aria-label={payload.label}
          autoComplete="off"
          data-1p-ignore="true"
          data-lpignore="true"
        />
        {payload.optional && onSkip ? (
          <button
            type="button"
            className="btn btn-ghost btn-sm ceo-card-skip"
            disabled={stage !== "pending"}
            onClick={() => onSkip(payload.field)}
            aria-label={`Skip ${payload.label}`}
          >
            Skip
          </button>
        ) : null}
        <button
          type="button"
          className="ceo-card-send"
          disabled={stage !== "pending" || !canSubmit}
          onClick={handleSubmit}
          aria-label={`Submit ${payload.label}`}
          title="Submit (Enter)"
        >
          {/* Submitting state shows no spinner — the wizard now swaps
              to the next card almost instantly via setQueryData, so a
              loader between questions just flashes and reads as noise.
              The `disabled` attribute already prevents double-submit. */}
          <svg
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
            <line x1="5" y1="12" x2="19" y2="12" />
            <polyline points="12 5 19 12 12 19" />
          </svg>
        </button>
      </div>
    </div>
  );
}
