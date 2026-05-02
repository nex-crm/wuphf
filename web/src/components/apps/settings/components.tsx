import { type ReactNode, useState } from "react";

import { showNotice } from "../../ui/Toast";
import { styles } from "./styles";

// Reusable settings primitives. Field is the label/hint/control row;
// SaveButton handles the idle/saving/saved state machine; KeyField is
// the password-style input with the "Set" / "Not set" badge that every
// API-key row uses.

interface FieldProps {
  label: string;
  hint?: string;
  children: ReactNode;
}

export function Field({ label, hint, children }: FieldProps) {
  return (
    <div style={styles.row}>
      <div style={styles.rowLabel}>
        <div style={styles.rowLabelName}>{label}</div>
        {hint && <div style={styles.rowLabelHint}>{hint}</div>}
      </div>
      <div style={styles.rowField}>{children}</div>
    </div>
  );
}

interface SaveButtonProps {
  label: string;
  onSave: () => Promise<boolean | void> | boolean | void;
}

export function SaveButton({ label, onSave }: SaveButtonProps) {
  const [state, setState] = useState<"idle" | "saving" | "saved">("idle");

  const handle = async () => {
    if (state === "saving") return;
    setState("saving");
    try {
      const result = await onSave();
      if (result === false) {
        setState("idle");
        return;
      }
      setState("saved");
      setTimeout(() => setState("idle"), 1500);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showNotice(`Save failed: ${msg}`, "error");
      setState("idle");
    }
  };

  return (
    <div style={styles.saveRow}>
      <button
        className="btn btn-primary btn-sm"
        onClick={handle}
        disabled={state === "saving"}
      >
        {state === "saving" ? "Saving..." : state === "saved" ? "Saved" : label}
      </button>
    </div>
  );
}

interface KeyFieldProps {
  hasValue: boolean;
  placeholder: string;
  value: string;
  onChange: (v: string) => void;
}

export function KeyField({
  hasValue,
  placeholder,
  value,
  onChange,
}: KeyFieldProps) {
  return (
    <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
      <input
        type="password"
        className="input"
        style={{
          ...styles.input,
          flex: 1,
          fontFamily: "var(--font-mono)",
          fontSize: 12,
        }}
        placeholder={hasValue ? "•••••••• (set)" : placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <span style={styles.keyStatus(hasValue)}>
        {hasValue ? "Set" : "Not set"}
      </span>
    </div>
  );
}
