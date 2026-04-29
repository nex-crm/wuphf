/**
 * ShredConfirmModal — destructive workspace removal with a copy
 * escalation for the `main` workspace.
 *
 * Non-main: single confirm step.
 * Main: requires the user to type the workspace name into a text input
 * before the confirm button enables. This mirrors the existing
 * WipeModal's "type a phrase to confirm" pattern.
 */
import { useEffect, useId, useRef, useState } from "react";

import type { Workspace } from "../../api/workspaces";

interface ShredConfirmModalProps {
  workspace: Workspace;
  busy?: boolean;
  onConfirm: (opts: { permanent: boolean }) => void | Promise<void>;
  onCancel: () => void;
}

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
  },
  title: {
    fontSize: 17,
    fontWeight: 700,
    color: "var(--text)",
    marginBottom: 10,
  },
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
    display: "block" as const,
  },
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
  },
  row: {
    display: "flex" as const,
    gap: 8,
    justifyContent: "flex-end" as const,
    marginTop: 18,
  },
  permanentRow: {
    display: "flex" as const,
    alignItems: "center" as const,
    gap: 8,
    fontSize: 12,
    color: "var(--text-secondary)",
    margin: "12px 0 0",
  },
};

function formatCreated(ts?: string): string {
  if (!ts) return "today";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  return d.toLocaleDateString();
}

export function ShredConfirmModal({
  workspace,
  busy = false,
  onConfirm,
  onCancel,
}: ShredConfirmModalProps) {
  const isMain = workspace.name === "main";
  const [typed, setTyped] = useState("");
  const [permanent, setPermanent] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const titleId = useId();

  // Esc closes — match the WipeModal pattern.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && !busy) onCancel();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onCancel]);

  // Auto-focus the type-to-confirm input on main shred.
  useEffect(() => {
    if (isMain) inputRef.current?.focus();
  }, [isMain]);

  const canConfirm = !busy && (isMain ? typed.trim() === workspace.name : true);

  const titleText = isMain
    ? `Shred main workspace`
    : `Shred '${workspace.name}'?`;

  const introCopy = isMain ? (
    <>
      You are about to move{" "}
      <code style={{ fontFamily: "var(--font-mono)" }}>~/.wuphf/</code> (created{" "}
      {formatCreated(workspace.created_at)}) to trash. This is reversible from
      the trash for 30 days, but agents stop running and sessions are lost on
      the active broker.
    </>
  ) : (
    <>
      Shred &lsquo;{workspace.name}&rsquo;? Moves to trash for 30 days. You can
      restore it from <code>wuphf workspace restore</code> or via the undo toast
      that appears next.
    </>
  );

  return (
    <div
      style={styles.backdrop}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      onClick={(e) => {
        if (e.target === e.currentTarget && !busy) onCancel();
      }}
    >
      <div style={styles.panel} className="card">
        <h3 id={titleId} style={styles.title}>
          {titleText}
        </h3>
        <p style={styles.body}>{introCopy}</p>

        {isMain ? (
          <>
            <label style={styles.inputLabel} htmlFor={`${titleId}-typed`}>
              Type &lsquo;main&rsquo; to confirm
            </label>
            <input
              id={`${titleId}-typed`}
              ref={inputRef}
              style={styles.input}
              type="text"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              spellCheck={false}
              data-testid="shred-confirm-phrase"
            />
          </>
        ) : null}

        <label style={styles.permanentRow}>
          <input
            type="checkbox"
            checked={permanent}
            onChange={(e) => setPermanent(e.target.checked)}
            data-testid="shred-permanent-toggle"
          />
          Skip trash (permanent — cannot be undone)
        </label>

        <div style={styles.row}>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={onCancel}
            disabled={busy}
          >
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-danger btn-sm"
            onClick={() => onConfirm({ permanent })}
            disabled={!canConfirm}
            data-testid="shred-confirm-submit"
          >
            {busy
              ? "Shredding..."
              : permanent
                ? "Shred permanently"
                : "Move to trash"}
          </button>
        </div>
      </div>
    </div>
  );
}
