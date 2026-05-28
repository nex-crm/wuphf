// biome-ignore-all lint/a11y/noStaticElementInteractions: Modal backdrop uses pointer hit-testing while dialog controls retain keyboard handling.
// biome-ignore-all lint/a11y/useKeyWithClickEvents: Backdrop pointer dismissal is paired with a window Escape listener while the modal is open.
import { useCallback, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  generateAgent,
  getConfig,
  type LLMRuntimeKind,
  post,
} from "../../api/client";
import { useWindowEscape } from "../../hooks/useWindowEscape";

// "inherit" is the wizard-only sentinel that maps to an absent ProviderBinding
// in the POST body (the broker then falls back to the install-wide default at
// dispatch time). Gateway kinds (openclaw / hermes-agent) are deliberately
// absent — agents bound to a gateway are imported through the Integrations
// app, not created in this wizard.
type ProviderChoice = "inherit" | LLMRuntimeKind;
type WizardMode = "describe" | "manual";

interface AgentFormData {
  name: string;
  slug: string;
  role: string;
  emoji: string;
  provider: ProviderChoice;
  model: string;
  expertise: string;
}

const INITIAL_FORM: AgentFormData = {
  name: "",
  slug: "",
  role: "",
  emoji: "",
  provider: "inherit",
  model: "",
  expertise: "",
};

// Human-readable labels for the runtime picker. Kinds the broker hasn't
// registered are skipped at render time, so missing labels here aren't fatal —
// they just fall back to the raw kind string.
const PROVIDER_LABELS: Record<LLMRuntimeKind, string> = {
  "claude-code": "Claude Code",
  codex: "Codex",
  opencode: "Opencode",
  "mlx-lm": "MLX-LM",
  ollama: "Ollama",
  exo: "Exo",
};

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

interface AgentWizardProps {
  open: boolean;
  onClose: () => void;
  onCreated?: () => void;
}

const AGENT_WIZARD_DIALOG_PROPS = {
  role: "dialog",
  "aria-modal": true,
  "aria-labelledby": "agent-wizard-title",
} as const;

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
export function AgentWizard({ open, onClose, onCreated }: AgentWizardProps) {
  const [mode, setMode] = useState<WizardMode>("describe");
  const [prompt, setPrompt] = useState("");
  const [generating, setGenerating] = useState(false);
  const [form, setForm] = useState<AgentFormData>(INITIAL_FORM);
  const [slugEdited, setSlugEdited] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  // Pull the registered runtime list off the wire so the picker stays in sync
  // with whatever providers the Go layer has registered. Falls back to a
  // hardcoded core set on first paint / fetch failure so the wizard is
  // usable even when /config is briefly unavailable. Lock state surfaces a
  // banner — when the global override is engaged the per-agent pick is
  // ignored at dispatch, so the user should know before saving.
  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    enabled: open,
    staleTime: 30_000,
  });
  const llmKinds: LLMRuntimeKind[] = (configQuery.data?.llm_provider_kinds ?? [
    "claude-code",
    "codex",
    "opencode",
    "mlx-lm",
    "ollama",
    "exo",
  ]) as LLMRuntimeKind[];
  const globalLocked = configQuery.data?.llm_provider_unlocked === false;
  const globalOverrideEngaged =
    configQuery.data?.llm_provider_unlocked === true;

  async function handleGenerate() {
    const trimmed = prompt.trim();
    if (!trimmed) {
      setError("Describe the agent you want first.");
      return;
    }
    setGenerating(true);
    setError(null);
    try {
      const tmpl = await generateAgent(trimmed);
      const generatedSlug = tmpl.slug || "";
      // The CEO-side generator may suggest a runtime/model pair. Only
      // honor it when the suggested provider is one we'd surface in the
      // picker (i.e. a non-gateway registered kind) — gateway kinds must
      // come in through the Integrations app, never through the wizard.
      const suggestedProvider = tmpl.provider as LLMRuntimeKind | undefined;
      const providerInList =
        suggestedProvider && llmKinds.includes(suggestedProvider);
      setForm({
        name: tmpl.name || "",
        slug: generatedSlug,
        role: tmpl.role || "",
        emoji: tmpl.emoji || "",
        provider: providerInList ? suggestedProvider : "inherit",
        model: tmpl.model || "",
        expertise: (tmpl.expertise || []).join(", "),
      });
      setSlugEdited(generatedSlug.length > 0);
      setMode("manual");
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Generation failed";
      setError(message);
    } finally {
      setGenerating(false);
    }
  }

  const updateField = useCallback(
    <K extends keyof AgentFormData>(field: K, value: AgentFormData[K]) => {
      setForm((prev) => {
        const next = { ...prev, [field]: value };
        if (field === "name" && !slugEdited) {
          next.slug = slugify(value as string);
        }
        return next;
      });
      setError(null);
    },
    [slugEdited],
  );

  const expertiseTags = useMemo(() => {
    return form.expertise
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);
  }, [form.expertise]);

  const canSubmit = form.name.trim().length > 0 && form.slug.trim().length > 0;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();

    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);

    try {
      // Encode provider as an explicit binding only when the user picked a
      // specific runtime. "inherit" sends an absent provider field so the
      // broker leaves Provider zero-valued; the dispatch resolver then
      // falls back to the install-wide default at turn time.
      const trimmedModel = form.model.trim();
      const providerBody =
        form.provider === "inherit"
          ? undefined
          : trimmedModel
            ? { kind: form.provider, model: trimmedModel }
            : { kind: form.provider };
      const body = {
        action: "create",
        slug: form.slug,
        name: form.name,
        role: form.role || undefined,
        emoji: form.emoji || undefined,
        provider: providerBody,
        expertise: expertiseTags.length > 0 ? expertiseTags : undefined,
      };

      await post("/office-members", body);
      await queryClient.invalidateQueries({ queryKey: ["office-members"] });

      setForm(INITIAL_FORM);
      setSlugEdited(false);
      onCreated?.();
      onClose();
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to create agent";
      setError(message);
    } finally {
      setSubmitting(false);
    }
  }

  const handleCancel = useCallback(() => {
    if (generating || submitting) return;
    setForm(INITIAL_FORM);
    setSlugEdited(false);
    setError(null);
    setMode("describe");
    setPrompt("");
    onClose();
  }, [generating, onClose, submitting]);

  function handleOverlayClick(e: React.MouseEvent) {
    if (e.target === e.currentTarget) {
      handleCancel();
    }
  }

  useWindowEscape(open, handleCancel);

  if (!open) return null;

  return (
    <div
      className="agent-wizard-overlay"
      role="presentation"
      onClick={handleOverlayClick}
    >
      <div className="agent-wizard-modal card" {...AGENT_WIZARD_DIALOG_PROPS}>
        <div className="agent-wizard-title" id="agent-wizard-title">
          Create agent
        </div>

        {/* Mode toggle */}
        <div className="channel-wizard-tabs" style={{ marginBottom: 16 }}>
          <button
            type="button"
            className={`channel-wizard-tab${mode === "describe" ? " active" : ""}`}
            onClick={() => {
              setMode("describe");
              setError(null);
            }}
          >
            Describe
          </button>
          <button
            type="button"
            className={`channel-wizard-tab${mode === "manual" ? " active" : ""}`}
            onClick={() => {
              setMode("manual");
              setError(null);
            }}
          >
            Manual
          </button>
        </div>

        {mode === "describe" ? (
          <div className="agent-wizard-form">
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-prompt">
                Describe the agent you want
              </label>
              <textarea
                id="agent-prompt"
                className="input"
                placeholder='e.g. "A DevOps engineer who manages CI/CD and infrastructure"'
                value={prompt}
                onChange={(e) => {
                  setPrompt(e.target.value);
                  setError(null);
                }}
                rows={3}
                style={{
                  minHeight: 80,
                  resize: "vertical",
                  padding: "10px 12px",
                  lineHeight: 1.5,
                }}
              />
              <span
                style={{
                  fontSize: 11,
                  color: "var(--text-tertiary)",
                  marginTop: 6,
                  display: "block",
                }}
              >
                AI will draft a slug, name, role, expertise, and personality.
                You can edit before creating.
              </span>
            </div>

            {error ? <div className="agent-wizard-error">{error}</div> : null}

            <div className="agent-wizard-footer">
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                onClick={handleCancel}
                disabled={generating}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-primary btn-sm"
                onClick={handleGenerate}
                disabled={generating || !prompt.trim()}
              >
                {generating ? "Generating..." : "Generate"}
              </button>
            </div>
          </div>
        ) : (
          <form className="agent-wizard-form" onSubmit={handleSubmit}>
            {/* Name */}
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-name">
                Name
              </label>
              <input
                id="agent-name"
                className="input"
                type="text"
                placeholder="e.g. Sales Rep"
                value={form.name}
                onChange={(e) => updateField("name", e.target.value)}
              />
            </div>

            {/* Slug */}
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-slug">
                Slug
              </label>
              <input
                id="agent-slug"
                className="input"
                type="text"
                placeholder="auto-generated-from-name"
                value={form.slug}
                onChange={(e) => {
                  setSlugEdited(true);
                  updateField("slug", e.target.value);
                }}
              />
            </div>

            {/* Role */}
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-role">
                Role
              </label>
              <input
                id="agent-role"
                className="input"
                type="text"
                placeholder="e.g. SDR, Engineer, Support"
                value={form.role}
                onChange={(e) => updateField("role", e.target.value)}
              />
            </div>

            {/* Emoji */}
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-emoji">
                Emoji
              </label>
              <input
                id="agent-emoji"
                className="input"
                type="text"
                placeholder="e.g. robot face"
                value={form.emoji}
                onChange={(e) => updateField("emoji", e.target.value)}
                maxLength={4}
                style={{ width: 80 }}
              />
            </div>

            {/* Provider + Model */}
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-provider">
                Runtime
              </label>
              <select
                id="agent-provider"
                value={form.provider}
                onChange={(e) =>
                  updateField("provider", e.target.value as ProviderChoice)
                }
              >
                <option value="inherit">
                  Inherit default (
                  {configQuery.data?.llm_provider ?? "claude-code"})
                </option>
                {llmKinds.map((kind) => (
                  <option key={kind} value={kind}>
                    {PROVIDER_LABELS[kind] ?? kind}
                  </option>
                ))}
              </select>
              {globalOverrideEngaged ? (
                <span
                  style={{
                    fontSize: 11,
                    color: "var(--text-tertiary)",
                    marginTop: 6,
                    display: "block",
                  }}
                >
                  The global runtime is unlocked and overriding every agent —
                  per-agent picks are ignored until it's re-locked in Settings.
                </span>
              ) : globalLocked && form.provider === "inherit" ? (
                <span
                  style={{
                    fontSize: 11,
                    color: "var(--text-tertiary)",
                    marginTop: 6,
                    display: "block",
                  }}
                >
                  Inheriting the install default. Change here to pin this agent
                  to a specific runtime.
                </span>
              ) : null}
            </div>
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-model">
                Model{" "}
                <span
                  style={{ fontWeight: 400, color: "var(--text-tertiary)" }}
                >
                  (optional)
                </span>
              </label>
              <input
                id="agent-model"
                className="input"
                type="text"
                placeholder={
                  form.provider === "inherit"
                    ? "Uses the runtime's default model"
                    : "e.g. claude-3-5-sonnet-latest, llama3.1:8b"
                }
                value={form.model}
                onChange={(e) => updateField("model", e.target.value)}
                disabled={form.provider === "inherit"}
              />
              <span
                style={{
                  fontSize: 11,
                  color: "var(--text-tertiary)",
                  marginTop: 6,
                  display: "block",
                }}
              >
                Validated by the runtime, not by WUPHF. Leave blank to use
                whatever model the runtime selects by default.
              </span>
            </div>

            {/* Expertise */}
            <div className="agent-wizard-field">
              <label className="label" htmlFor="agent-expertise">
                Expertise{" "}
                <span
                  style={{ fontWeight: 400, color: "var(--text-tertiary)" }}
                >
                  (comma-separated)
                </span>
              </label>
              <input
                id="agent-expertise"
                className="input"
                type="text"
                placeholder="e.g. outreach, cold email, pipeline"
                value={form.expertise}
                onChange={(e) => updateField("expertise", e.target.value)}
              />
              {expertiseTags.length > 0 && (
                <div className="agent-panel-tags" style={{ marginTop: 6 }}>
                  {expertiseTags.map((tag) => (
                    <span key={tag} className="agent-panel-tag">
                      {tag}
                    </span>
                  ))}
                </div>
              )}
            </div>

            {error ? <div className="agent-wizard-error">{error}</div> : null}

            {/* Footer */}
            <div className="agent-wizard-footer">
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                onClick={handleCancel}
                disabled={submitting}
              >
                Cancel
              </button>
              <button
                type="submit"
                className="btn btn-primary btn-sm"
                disabled={!canSubmit || submitting}
              >
                {submitting ? "Creating..." : "Create"}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

/**
 * Hook to manage wizard open/close state from any component.
 * Usage:
 *   const { open, show, hide } = useAgentWizard()
 *   <button onClick={show}>New Agent</button>
 *   <AgentWizard open={open} onClose={hide} />
 */
export function useAgentWizard() {
  const [open, setOpen] = useState(false);
  const show = useCallback(() => setOpen(true), []);
  const hide = useCallback(() => setOpen(false), []);
  return { open, show, hide };
}
