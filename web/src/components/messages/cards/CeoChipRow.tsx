/**
 * CeoChipRow — single-select chip row.
 *
 * Used for: blueprint pick.
 *
 * Keyboard model per spec:
 *   Tab        → into row
 *   Arrow keys → move chip selection
 *   Enter      → commit selected chip
 *   Space      → commit selected chip
 *
 * All chip labels treated as plain text (never innerHTML).
 */

import { useRef, useState } from "react";

import type {
  CardStage,
  CeoChipOption,
  CeoChipRowPayload,
} from "../../onboarding/types";

interface CeoChipRowProps {
  payload: CeoChipRowPayload;
  stage: CardStage;
  committedValue?: string;
  onSubmit: (field: string, value: string) => void;
  /** Ref for focus management — parent focuses first chip after prior card commits. */
  autoFocusRef?: React.RefObject<HTMLButtonElement | null>;
}

export function CeoChipRow({
  payload,
  stage,
  committedValue,
  onSubmit,
  autoFocusRef,
}: CeoChipRowProps) {
  const [selected, setSelected] = useState<string | null>(null);
  const firstChipRef = useRef<HTMLButtonElement>(null);

  // Merge externally-provided ref so the parent can focus the first chip.
  if (autoFocusRef && firstChipRef.current !== null) {
    (autoFocusRef as React.MutableRefObject<HTMLButtonElement | null>).current =
      firstChipRef.current;
  }

  if (stage === "committed") {
    const label =
      committedValue ??
      payload.options.find((o) => o.id === selected)?.label ??
      selected ??
      "";
    return (
      <div className="ceo-card ceo-card--committed" role="status">
        <span className="ceo-card-committed-text">&#10003; {label}</span>
      </div>
    );
  }

  const handleSelect = (id: string) => {
    if (stage === "submitting") return;
    setSelected(id);
    onSubmit(payload.field, id);
  };

  const handleKeyDown = (
    e: React.KeyboardEvent<HTMLButtonElement>,
    id: string,
    idx: number,
  ) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      handleSelect(id);
      return;
    }
    // Arrow key navigation within the chip row
    const chips = payload.options;
    if (e.key === "ArrowRight" || e.key === "ArrowDown") {
      e.preventDefault();
      const next = chips[(idx + 1) % chips.length];
      const el = document.getElementById(
        `ceo-chip-${payload.field}-${next.id}`,
      );
      el?.focus();
    } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
      e.preventDefault();
      const prev = chips[(idx - 1 + chips.length) % chips.length];
      const el = document.getElementById(
        `ceo-chip-${payload.field}-${prev.id}`,
      );
      el?.focus();
    }
  };

  // Card layout kicks in whenever any option ships an icon or description —
  // that signals the row was meant as a richer picker (blueprint catalog)
  // rather than a flat pill row (bridge_choice). Plain rows degrade to the
  // original chip pill style so legacy chip_rows render unchanged.
  const isCardLayout = payload.options.some(
    (o) => (o.icon ?? "") !== "" || (o.description ?? "") !== "",
  );

  return (
    <div
      className={`ceo-card ceo-card--chip-row${isCardLayout ? " ceo-card--chip-grid" : ""}`}
      data-testid="ceo-chip-row"
      role="group"
      aria-label={payload.label}
    >
      {payload.label ? (
        <div className="ceo-card-label">{payload.label}</div>
      ) : null}
      <div
        className={
          isCardLayout ? "ceo-chip-grid-options" : "ceo-chip-row-options"
        }
        role="listbox"
        aria-label={payload.label}
      >
        {payload.options.map((opt, idx) => (
          <ChipOptionButton
            key={opt.id}
            opt={opt}
            field={payload.field}
            isCardLayout={isCardLayout}
            selected={selected === opt.id}
            isSubmitting={stage === "submitting"}
            firstChipRef={idx === 0 ? firstChipRef : undefined}
            onSelect={handleSelect}
            onKeyDown={(e) => handleKeyDown(e, opt.id, idx)}
          />
        ))}
      </div>
    </div>
  );
}

interface ChipOptionButtonProps {
  opt: CeoChipOption;
  field: string;
  isCardLayout: boolean;
  selected: boolean;
  isSubmitting: boolean;
  firstChipRef?: React.RefObject<HTMLButtonElement | null>;
  onSelect: (id: string) => void;
  onKeyDown: (e: React.KeyboardEvent<HTMLButtonElement>) => void;
}

function ChipOptionButton({
  opt,
  field,
  isCardLayout,
  selected,
  isSubmitting,
  firstChipRef,
  onSelect,
  onKeyDown,
}: ChipOptionButtonProps) {
  const hasDescription = (opt.description ?? "") !== "";
  const hasIcon = (opt.icon ?? "") !== "";
  const className = isCardLayout
    ? `ceo-chip-card${selected ? " ceo-chip-card--selected" : ""}`
    : `ceo-chip${selected ? " ceo-chip--selected" : ""}`;
  const descId = `ceo-chip-${field}-${opt.id}-desc`;
  return (
    <button
      id={`ceo-chip-${field}-${opt.id}`}
      ref={firstChipRef}
      type="button"
      role="option"
      aria-selected={selected}
      aria-describedby={hasDescription ? descId : undefined}
      className={className}
      disabled={isSubmitting}
      onClick={() => onSelect(opt.id)}
      onKeyDown={onKeyDown}
    >
      {isSubmitting && selected ? (
        <span className="ceo-card-spinner" aria-hidden="true" />
      ) : null}
      {isCardLayout ? (
        <>
          {hasIcon ? (
            <span className="ceo-chip-card-icon" aria-hidden="true">
              {opt.icon}
            </span>
          ) : null}
          <span className="ceo-chip-card-body">
            <span className="ceo-chip-card-label">{opt.label}</span>
            {hasDescription ? (
              <span className="ceo-chip-card-description" id={descId}>
                {opt.description}
              </span>
            ) : null}
          </span>
        </>
      ) : (
        /* Render as text — never innerHTML */
        opt.label
      )}
    </button>
  );
}
