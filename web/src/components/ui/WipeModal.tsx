import { type ReactNode, useEffect, useId, useRef, useState } from "react";

export type WipeSeverity = "warn" | "critical";

export const WIPE_CONFIRM_PHRASE = "i can spell responsibility";

const styles = {
  backdrop: {
    position: "fixed" as const,
    inset: 0,
    background: "rgba(0,0,0,0.6)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    zIndex: 1000,
  },
  panel: {
    width: "min(520px, calc(100vw - 40px))",
    background: "var(--bg-card)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
    padding: 24,
    boxShadow: "0 20px 60px rgba(0,0,0,0.4)",
  } as const,
  title: {
    fontSize: 17,
    fontWeight: 700,
    color: "var(--text)",
    marginBottom: 10,
  } as const,
  body: {
    fontSize: 13,
    color: "var(--text-secondary)",
    lineHeight: 1.55,
    marginBottom: 16,
  } as const,
  inputLabel: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.06em",
    color: "var(--text-tertiary)",
    marginBottom: 6,
    display: "block",
  } as const,
  input: {
    width: "100%",
    background: "var(--bg-warm)",
    border: "1px solid var(--border)",
    color: "var(--text)",
    borderRadius: "var(--radius-sm)",
    height: 38,
    fontSize: 14,
    padding: "0 12px",
    outline: "none",
    fontFamily: "var(--font-mono)",
  } as const,
  row: {
    display: "flex",
    gap: 8,
    justifyContent: "flex-end",
    marginTop: 18,
  } as const,
  cancel: {
    padding: "9px 16px",
    fontSize: 13,
    fontWeight: 500,
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-sm)",
    cursor: "pointer" as const,
    color: "var(--text)",
    background: "transparent",
    fontFamily: "var(--font-sans)",
  } as const,
  confirm: (severity: WipeSeverity, enabled: boolean) => ({
    padding: "9px 16px",
    fontSize: 13,
    fontWeight: 600,
    border: "none",
    borderRadius: "var(--radius-sm)",
    cursor: enabled ? "pointer" : ("not-allowed" as const),
    color: "#fff",
    background: enabled
      ? severity === "critical"
        ? "var(--red, #e5484d)"
        : "var(--yellow, #e5a00d)"
      : "var(--bg-warm)",
    opacity: enabled ? 1 : 0.6,
    fontFamily: "var(--font-sans)",
  }),
};

export interface WipeModalProps {
  title: string;
  severity: WipeSeverity;
  intro: ReactNode;
  confirmLabel: string;
  busy: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

// WipeModal gates a destructive action behind a type-the-exact-phrase confirm.
// The placeholder and the body copy both surface the full phrase so there's no mystery
// about what to type — we want the friction, not the guesswork.
export function WipeModal({
  title,
  severity,
  intro,
  confirmLabel,
  busy,
  onConfirm,
  onCancel,
}: WipeModalProps) {
  const [value, setValue] = useState("");
  const enabled = !busy && value.trim().toLowerCase() === WIPE_CONFIRM_PHRASE;
  const titleId = useId();
  const bodyId = useId();
  const inputRef = useRef<HTMLInputElement | null>(null);
  // Track where the press started so accidental drag-out from the input to
  // the backdrop (common while selecting the displayed phrase to copy) does
  // not dismiss the modal — `click` would otherwise fire on the backdrop and
  // cancel.
  const backdropPressRef = useRef(false);

  // Focus the input on mount. biome's a11y/noAutofocus rule explicitly allows
  // imperative focus via `useRef`+`useEffect` — only the JSX `autoFocus`
  // attribute is forbidden — and the type-the-phrase friction is much worse
  // when users have to click into the input first.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Escape closes the modal — keyboard parity with backdrop click. Skipped
  // while busy so an in-flight wipe cannot be canceled mid-flight by a stray
  // keypress (matches the backdrop guard on line 119).
  useEffect(() => {
    if (busy) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onCancel();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onCancel]);

  return (
    <div
      style={styles.backdrop}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={bodyId}
      onMouseDown={(e) => {
        backdropPressRef.current = e.target === e.currentTarget;
      }}
      onMouseUp={(e) => {
        if (busy) return;
        if (backdropPressRef.current && e.target === e.currentTarget) {
          onCancel();
        }
        backdropPressRef.current = false;
      }}
    >
      <div style={styles.panel}>
        <div id={titleId} style={styles.title}>
          {title}
        </div>
        <div id={bodyId} style={styles.body}>
          {intro}
        </div>
        <label style={styles.inputLabel}>
          Type <code>{WIPE_CONFIRM_PHRASE}</code> to confirm
        </label>
        <input
          ref={inputRef}
          type="text"
          style={styles.input}
          placeholder={WIPE_CONFIRM_PHRASE}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          disabled={busy}
        />
        <div style={styles.row}>
          <button
            type="button"
            style={styles.cancel}
            onClick={onCancel}
            disabled={busy}
          >
            Cancel
          </button>
          <button
            type="button"
            style={styles.confirm(severity, enabled)}
            onClick={enabled ? onConfirm : undefined}
            disabled={!enabled}
          >
            {busy ? "Working…" : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
