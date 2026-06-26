/**
 * CeoFormField — label + text input + submit chip + optional "Skip" chip.
 *
 * Used for: company name, description, website, owner_name, owner_role.
 *
 * Card lifecycle: pending → submitting → committed (one-line "✓ <value>").
 * All strings treated as plain text. Never render raw payload as HTML.
 *
 * Keyboard model per spec:
 *   Tab  → input focus
 *   Enter → submit (when input focused)
 *   Space → type normally (not a submit trigger here)
 */

import { useEffect, useRef, useState } from "react";

import type { CardStage, CeoFormFieldPayload } from "../../onboarding/types";

interface CeoFormFieldProps {
  payload: CeoFormFieldPayload;
  stage: CardStage;
  committedValue?: string;
  onSubmit: (field: string, value: string) => void;
  onSkip?: (field: string) => void;
  /** Ref for focus management — parent focuses this after prior card commits. */
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

  // Merge externally-provided ref with local ref so the parent can manage
  // focus while this component can also auto-focus on mount.
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
        // Suppress browser password-manager / autofill popups. The CEO card is
        // a single-field conversational prompt, not a login or profile form;
        // 1Password and Chrome were offering credential fills on company
        // name / website / description (issue #943). The `data-*-ignore`
        // attributes are vendor escape hatches for 1Password and LastPass,
        // which sometimes ignore plain `autoComplete="off"`.
        autoComplete="off"
        data-1p-ignore="true"
        data-lpignore="true"
      />
      <div className="ceo-card-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm ceo-card-submit"
          disabled={stage === "submitting" || !canSubmit}
          onClick={handleSubmit}
          aria-label={`Submit ${payload.label}`}
        >
          {stage === "submitting" ? (
            <span className="ceo-card-spinner" aria-hidden="true" />
          ) : null}
          {stage === "submitting" ? "Saving…" : "Submit"}
        </button>
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
      </div>
    </div>
  );
}
