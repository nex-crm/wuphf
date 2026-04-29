/**
 * CreateWorkspaceModal — inline modal that creates a new WUPHF workspace.
 *
 * Two paths:
 *   1. Inherit from current (default): pre-fills blueprint, company info,
 *      LLM provider, team lead. Single screen + slug input.
 *   2. From-scratch: toggles off inherit. Shows the standard onboarding
 *      Wizard inline, scoped to the new workspace's runtime. Submit is
 *      handled by the wizard's own /onboarding/complete; the rail picks
 *      up the new workspace via /workspaces/list invalidation.
 *
 * Submit (inherit path) calls POST /workspaces/create. On success the
 * SPA navigates to the new workspace's URL via a full page reload —
 * page-reload-on-switch architecture, see design doc "Switch Protocol".
 */
import { useEffect, useId, useMemo, useState } from "react";

import { type ConfigSnapshot, getConfig } from "../../api/client";
import {
  type CreateWorkspaceInput,
  useCreateWorkspace,
  validateWorkspaceSlug,
} from "../../api/workspaces";
import { Wizard } from "../onboarding/Wizard";
import { showNotice } from "../ui/Toast";

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
    width: "min(620px, calc(100vw - 40px))",
    maxHeight: "calc(100vh - 80px)",
    overflowY: "auto" as const,
    background: "var(--bg-card)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
    padding: 24,
    boxShadow: "0 20px 60px rgba(0,0,0,0.4)",
  },
  panelWizard: {
    width: "min(960px, calc(100vw - 40px))",
    maxHeight: "calc(100vh - 60px)",
    overflowY: "auto" as const,
    background: "var(--bg-card)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
    padding: 0,
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
  textarea: {
    width: "100%",
    background: "var(--bg-warm)",
    border: "1px solid var(--border)",
    color: "var(--text)",
    borderRadius: "var(--radius-sm)",
    fontSize: 13,
    padding: "8px 12px",
    outline: "none",
    fontFamily: "var(--font-sans)",
    minHeight: 60,
    resize: "vertical" as const,
  },
  toggleRow: {
    display: "flex" as const,
    alignItems: "center" as const,
    gap: 8,
    fontSize: 13,
    color: "var(--text)",
    marginBottom: 16,
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
  row: {
    display: "flex" as const,
    gap: 8,
    justifyContent: "flex-end" as const,
    marginTop: 18,
  },
  progress: {
    fontSize: 13,
    color: "var(--text-secondary)",
    margin: "12px 0",
    fontFamily: "var(--font-mono)",
  },
};

type Phase = "form" | "spawning" | "ready" | "error";

/**
 * Tiny copy helper for the progress UI. We don't have an SSE stream
 * for create (that was deferred from the smooth-switch design), so the
 * stages are time-based hints rather than truth — the user gets a sense
 * of motion until /workspaces/create returns.
 */
const SPAWN_STAGES: readonly string[] = [
  "allocating ports",
  "writing registry entry",
  "spawning broker",
  "waiting for broker to bind",
  "ready",
];

interface FormState {
  name: string;
  inherit: boolean;
  blueprint: string;
  company_name: string;
  company_description: string;
  company_priority: string;
  llm_provider: string;
  team_lead_slug: string;
}

function defaultsFromConfig(config?: ConfigSnapshot): FormState {
  return {
    name: "",
    inherit: true,
    blueprint: config?.blueprint ?? "",
    company_name: config?.company_name ?? "",
    company_description: config?.company_description ?? "",
    company_priority: config?.company_priority ?? "",
    llm_provider: config?.llm_provider ?? "",
    team_lead_slug: config?.team_lead_slug ?? "",
  };
}

export function CreateWorkspaceModal({
  open,
  onClose,
}: CreateWorkspaceModalProps) {
  const titleId = useId();
  const [config, setConfig] = useState<ConfigSnapshot | undefined>(undefined);
  const [form, setForm] = useState<FormState>(defaultsFromConfig());
  const [phase, setPhase] = useState<Phase>("form");
  const [stageIdx, setStageIdx] = useState(0);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const create = useCreateWorkspace({
    onSuccess: (ws) => {
      setPhase("ready");
      // Full page reload to the new workspace — page-reload-on-switch.
      // Broker returns the Workspace row directly (not {workspace, url}),
      // so derive the URL from web_port.
      window.location.assign(`http://localhost:${ws.web_port}/`);
    },
    onError: (err) => {
      setPhase("error");
      setErrorMsg(err instanceof Error ? err.message : String(err));
    },
  });

  // Pre-fill inheritable fields from the active broker's /config when the
  // modal opens. We deliberately preserve `name` and `inherit` because the
  // user may have started typing before /config resolves.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void getConfig()
      .then((c) => {
        if (cancelled) return;
        setConfig(c);
        setForm((prev) => ({
          ...defaultsFromConfig(c),
          name: prev.name,
          inherit: prev.inherit,
        }));
      })
      .catch((err) => {
        // Non-fatal — let the user fill in the form manually.
        if (cancelled) return;
        // eslint-disable-next-line no-console
        console.warn("[CreateWorkspaceModal] /config fetch failed:", err);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  // Reset to a clean form whenever the modal re-opens.
  useEffect(() => {
    if (open) {
      setPhase("form");
      setStageIdx(0);
      setErrorMsg(null);
    }
  }, [open]);

  // Animated stage hint while we wait on /workspaces/create.
  useEffect(() => {
    if (phase !== "spawning") return;
    setStageIdx(0);
    const tick = window.setInterval(() => {
      setStageIdx((i) => Math.min(i + 1, SPAWN_STAGES.length - 2));
    }, 1500);
    return () => window.clearInterval(tick);
  }, [phase]);

  // Esc closes — only when the form isn't busy.
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

  const slugValidation = useMemo(
    () => validateWorkspaceSlug(form.name),
    [form.name],
  );
  const canSubmit =
    phase === "form" && form.inherit && slugValidation.ok && !create.isPending;

  if (!open) return null;

  // From-scratch: hand the inline wizard responsibility for the heavier
  // path. The wizard uses the active broker's /onboarding/* endpoints —
  // for a true "scoped to new WUPHF_RUNTIME_HOME" run, Lane C exposes
  // those endpoints on a per-workspace basis (via /workspaces/create
  // first, then /workspaces/<name>/onboarding/...). For v1 the wizard
  // runs against the current broker after the workspace is allocated.
  if (form.inherit === false && phase === "form") {
    return (
      <div
        style={styles.backdrop}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onClick={(e) => {
          if (e.target === e.currentTarget) onClose();
        }}
      >
        <div style={styles.panelWizard} className="card">
          <div
            style={{
              padding: "16px 24px",
              borderBottom: "1px solid var(--border)",
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <div>
              <h3 id={titleId} style={{ ...styles.title, marginBottom: 0 }}>
                New workspace from scratch
              </h3>
              <p style={{ ...styles.subtitle, marginBottom: 0 }}>
                Run the full onboarding wizard scoped to a fresh runtime.
              </p>
            </div>
            <label style={styles.toggleRow}>
              <input
                type="checkbox"
                checked={form.inherit}
                onChange={(e) =>
                  setForm((f) => ({ ...f, inherit: e.target.checked }))
                }
                data-testid="inherit-toggle"
              />
              Inherit from current
            </label>
          </div>
          <Wizard
            onComplete={() => {
              showNotice("Workspace created.", "success");
              onClose();
            }}
          />
        </div>
      </div>
    );
  }

  // Inherit path (default).
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
          Forks blueprint, company identity, LLM config, and agent roster from
          the current workspace. Wiki, tasks, notebooks, and broker state start
          empty.
        </p>

        <label style={styles.toggleRow}>
          <input
            type="checkbox"
            checked={form.inherit}
            onChange={(e) =>
              setForm((f) => ({ ...f, inherit: e.target.checked }))
            }
            data-testid="inherit-toggle"
            disabled={phase === "spawning"}
          />
          Inherit from current
        </label>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-slug`}>
            Workspace name
          </label>
          <input
            id={`${titleId}-slug`}
            style={styles.input}
            value={form.name}
            onChange={(e) =>
              setForm((f) => ({ ...f, name: e.target.value.toLowerCase() }))
            }
            placeholder="e.g. acme-demo"
            autoComplete="off"
            spellCheck={false}
            data-testid="workspace-slug-input"
            disabled={phase === "spawning"}
          />
          {form.name.length > 0 && !slugValidation.ok ? (
            <div style={styles.errorText} data-testid="workspace-slug-error">
              {slugValidation.reason}
            </div>
          ) : (
            <div style={styles.hintText}>
              Lowercase letters, digits, hyphens. Must start with a letter.
              Reserved: main, dev, prod, default, current, tokens, trash.
            </div>
          )}
        </div>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-blueprint`}>
            Blueprint
          </label>
          <input
            id={`${titleId}-blueprint`}
            style={styles.input}
            value={form.blueprint}
            onChange={(e) =>
              setForm((f) => ({ ...f, blueprint: e.target.value }))
            }
            placeholder="e.g. founding-team"
            autoComplete="off"
            spellCheck={false}
            disabled={phase === "spawning"}
          />
        </div>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-company`}>
            Company name
          </label>
          <input
            id={`${titleId}-company`}
            style={styles.input}
            value={form.company_name}
            onChange={(e) =>
              setForm((f) => ({ ...f, company_name: e.target.value }))
            }
            disabled={phase === "spawning"}
          />
        </div>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-description`}>
            Company description
          </label>
          <textarea
            id={`${titleId}-description`}
            style={styles.textarea}
            value={form.company_description}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                company_description: e.target.value,
              }))
            }
            disabled={phase === "spawning"}
          />
        </div>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-priority`}>
            Top priority right now
          </label>
          <input
            id={`${titleId}-priority`}
            style={styles.input}
            value={form.company_priority}
            onChange={(e) =>
              setForm((f) => ({ ...f, company_priority: e.target.value }))
            }
            disabled={phase === "spawning"}
          />
        </div>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-llm`}>
            LLM provider
          </label>
          <input
            id={`${titleId}-llm`}
            style={styles.input}
            value={form.llm_provider}
            onChange={(e) =>
              setForm((f) => ({ ...f, llm_provider: e.target.value }))
            }
            disabled={phase === "spawning"}
          />
        </div>

        <div style={styles.field}>
          <label style={styles.label} htmlFor={`${titleId}-lead`}>
            Team lead slug
          </label>
          <input
            id={`${titleId}-lead`}
            style={styles.input}
            value={form.team_lead_slug}
            onChange={(e) =>
              setForm((f) => ({ ...f, team_lead_slug: e.target.value }))
            }
            disabled={phase === "spawning"}
          />
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
            onClick={() => {
              setErrorMsg(null);
              setPhase("spawning");
              // Broker's CreateRequest only accepts {name, blueprint?,
              // inherit_from?, company_name?, from_scratch?}. Richer
              // onboarding fields (company_description, company_priority,
              // llm_provider, team_lead_slug) are scoped to the subsequent
              // /onboarding/* calls — see TODOS.md "two-step create+onboard".
              const payload: CreateWorkspaceInput = {
                name: form.name.trim(),
                from_scratch: !form.inherit,
                blueprint: form.blueprint || undefined,
                company_name: form.company_name || undefined,
              };
              create.mutate(payload);
            }}
          >
            {phase === "spawning" ? "Creating..." : "Create workspace"}
          </button>
        </div>
        {config?.config_path ? (
          <div style={{ ...styles.hintText, marginTop: 12 }}>
            API keys, agent roster, and onboarding state will be forked from{" "}
            <code>{config.config_path}</code>.
          </div>
        ) : null}
      </div>
    </div>
  );
}
