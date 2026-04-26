import { type ReactNode, useState } from "react";

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

  return (
    <div style={styles.backdrop} onClick={busy ? undefined : onCancel}>
      <div style={styles.panel} onClick={(e) => e.stopPropagation()}>
        <div style={styles.title}>{title}</div>
        <div style={styles.body}>{intro}</div>
        <label style={styles.inputLabel}>
          Type <code>{WIPE_CONFIRM_PHRASE}</code> to confirm
        </label>
        <input
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
