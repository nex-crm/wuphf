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

  if (stage === "committed") {
    return (
      <div className="ceo-card ceo-card--committed" role="status">
        <span className="ceo-card-committed-text">
          &#10003; {committedValue ?? value}
        </span>
      </div>
    );
  }

  const canSubmit = value.trim().length > 0 || payload.optional;

  const handleSubmit = () => {
    if (stage === "submitting") return;
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
          disabled={stage === "submitting"}
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
            disabled={stage === "submitting"}
            onClick={() => onSkip(payload.field)}
            aria-label={`Skip ${payload.label}`}
          >
            Skip
          </button>
        ) : null}
        <button
          type="button"
          className="ceo-card-send"
          disabled={stage === "submitting" || !canSubmit}
          onClick={handleSubmit}
          aria-label={`Submit ${payload.label}`}
          title="Submit (Enter)"
        >
          {stage === "submitting" ? (
            <span className="ceo-card-spinner" aria-hidden="true" />
          ) : (
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
          )}
        </button>
      </div>
    </div>
  );
}
