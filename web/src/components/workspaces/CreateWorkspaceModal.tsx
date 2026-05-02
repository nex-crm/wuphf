// biome-ignore-all lint/a11y/useKeyWithClickEvents: Pointer handler is paired with an existing modal, image, or routed-control keyboard path; preserving current interaction model.
/**
 * CreateWorkspaceModal — minimal name-capture modal for new workspace creation.
 *
 * Always creates from scratch and navigates to the new broker's /onboarding
 * URL with ?skip_identity=1 so the wizard skips the identity step (company
 * info) — you're an existing user, not a first-time setup.
 */
import { useEffect, useId, useMemo, useState } from "react";

import {
  type CreateWorkspaceInput,
  type Workspace,
  useCreateWorkspace,
  validateWorkspaceSlug,
} from "../../api/workspaces";

interface CreateWorkspaceModalProps {
  open: boolean;
  onClose: () => void;
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
    padding: 20,
  },
  panel: {
    width: "min(440px, calc(100vw - 40px))",
    background: "var(--bg-card)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
    padding: 24,
    boxShadow: "0 20px 60px rgba(0,0,0,0.4)",
  },
  title: {
    fontSize: 18,
    fontWeight: 700,
    color: "var(--text)",
    marginBottom: 4,
  },
  subtitle: {
    fontSize: 13,
    color: "var(--text-secondary)",
    marginBottom: 18,
  },
  field: {
    marginBottom: 14,
  },
  label: {
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
  errorText: {
    color: "var(--red)",
    fontSize: 12,
    marginTop: 6,
  },
  hintText: {
    color: "var(--text-tertiary)",
    fontSize: 12,
    marginTop: 6,
  },
  progress: {
    fontSize: 13,
    color: "var(--text-secondary)",
    margin: "12px 0",
    fontFamily: "var(--font-mono)",
  },
  row: {
    display: "flex" as const,
    gap: 8,
    justifyContent: "flex-end" as const,
    marginTop: 18,
  },
};

type Phase = "form" | "spawning" | "error";

const SPAWN_STAGES: readonly string[] = [
  "allocating ports",
  "writing registry entry",
  "spawning broker",
  "waiting for broker to bind",
  "ready",
];

export function CreateWorkspaceModal({
  open,
  onClose,
}: CreateWorkspaceModalProps) {
  const titleId = useId();
  const [name, setName] = useState("");
  const [phase, setPhase] = useState<Phase>("form");
  const [stageIdx, setStageIdx] = useState(0);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const navigateToWorkspace = (ws: Workspace) => {
    window.location.assign(
      `http://localhost:${ws.web_port}/onboarding?skip_identity=1`,
    );
  };

  const create = useCreateWorkspace({
    onSuccess: (ws) => navigateToWorkspace(ws),
    onError: (err) => {
      setPhase("error");
      setErrorMsg(err instanceof Error ? err.message : String(err));
    },
  });

  // Reset on open.
  useEffect(() => {
    if (open) {
      setName("");
      setPhase("form");
      setStageIdx(0);
      setErrorMsg(null);
    }
  }, [open]);

  // Animated stage hints while waiting on /workspaces/create.
  useEffect(() => {
    if (phase !== "spawning") return;
    setStageIdx(0);
    const tick = window.setInterval(() => {
      setStageIdx((i) => Math.min(i + 1, SPAWN_STAGES.length - 2));
    }, 1500);
    return () => window.clearInterval(tick);
  }, [phase]);

  // Esc closes when idle.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && phase !== "spawning") {
        onClose();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, phase, onClose]);

  const slugValidation = useMemo(() => validateWorkspaceSlug(name), [name]);
  const canSubmit =
    (phase === "form" || phase === "error") &&
    slugValidation.ok &&
    !create.isPending;

  if (!open) return null;

  const handleSubmit = () => {
    setErrorMsg(null);
    setPhase("spawning");
    const payload: CreateWorkspaceInput = {
      name: name.trim(),
      from_scratch: true,
    };
    create.mutate(payload);
  };

  return (
    <div
      style={styles.backdrop}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      onClick={(e) => {
        if (e.target === e.currentTarget && phase !== "spawning") onClose();
      }}
    >
      <div style={styles.panel} className="card">
        <h3 id={titleId} style={styles.title}>
          New workspace
        </h3>
        <p style={styles.subtitle}>
          Spawns a fresh broker, then drops you into the setup wizard.
        </p>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-slug`}>
            Workspace name
          </label>
          <input
            id={`${titleId}-slug`}
            style={styles.input}
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            placeholder="e.g. acme-demo"
            autoComplete="off"
            spellCheck={false}
            autoFocus
            data-testid="workspace-slug-input"
            disabled={phase === "spawning"}
            onKeyDown={(e) => {
              if (e.key === "Enter" && canSubmit) handleSubmit();
            }}
          />
          {name.length > 0 && !slugValidation.ok ? (
            <div style={styles.errorText} data-testid="workspace-slug-error">
              {slugValidation.reason}
            </div>
          ) : (
            <div style={styles.hintText}>
              Lowercase letters, digits, hyphens. Must start with a letter.
            </div>
          )}
        </div>

        {phase === "spawning" ? (
          <div style={styles.progress} data-testid="workspace-create-progress">
            ⏳ {SPAWN_STAGES[stageIdx]}…
          </div>
        ) : null}
        {phase === "error" && errorMsg ? (
          <div style={styles.errorText} data-testid="workspace-create-error">
            {errorMsg}
          </div>
        ) : null}

        <div style={styles.row}>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={onClose}
            disabled={phase === "spawning"}
          >
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={!canSubmit}
            data-testid="workspace-create-submit"
            onClick={handleSubmit}
          >
            {phase === "spawning" ? "Creating..." : "Create workspace"}
          </button>
        </div>
      </div>
    </div>
  );
}
