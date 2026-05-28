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

import type { CardStage, CeoChipRowPayload } from "../../onboarding/types";

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

  // Committed state intentionally renders the SAME chip row as
  // pending/submitting — just disabled. The previous "✓ <label>" chip
  // was flashing between cards because the sticky-suggestion swap is
  // near-instant.
  void committedValue;

  const handleSelect = (id: string) => {
    if (stage !== "pending") return;
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

  return (
    <div
      className="ceo-card ceo-card--chip-row"
      data-testid="ceo-chip-row"
      role="group"
      aria-label={payload.label}
    >
      {payload.label ? (
        <div className="ceo-card-label">{payload.label}</div>
      ) : null}
      <div
        className="ceo-chip-row-options"
        role="listbox"
        aria-label={payload.label}
      >
        {payload.options.map((opt, idx) => (
          <button
            key={opt.id}
            id={`ceo-chip-${payload.field}-${opt.id}`}
            ref={idx === 0 ? firstChipRef : undefined}
            type="button"
            role="option"
            aria-selected={selected === opt.id}
            className={`ceo-chip${selected === opt.id ? " ceo-chip--selected" : ""}`}
            disabled={stage !== "pending"}
            onClick={() => handleSelect(opt.id)}
            onKeyDown={(e) => handleKeyDown(e, opt.id, idx)}
          >
            {/* Render as text — never innerHTML. No submitting spinner;
                the next card swaps in fast enough that a loader reads
                as flicker. `disabled` prevents double-click. */}
            {opt.label}
          </button>
        ))}
      </div>
    </div>
  );
}
