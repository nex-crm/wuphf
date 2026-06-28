/**
 * EmbeddingChoice — the wiki step's "Power semantic memory" section.
 *
 * The wiki is the team's shared brain, so this is where the user picks how that
 * brain recalls a rule: by meaning (semantic memory) or by exact word (keyword
 * search). The backend's EnsureBrain auto-selects in priority order — an OpenAI
 * key, then a local Ollama model, then keyword — so this section is mostly
 * informational: it recommends the key, presents the alternatives in that same
 * order, and reflects the resulting state. It is always optional. Keyword search
 * works with zero setup, so the user can ignore this entirely and proceed.
 *
 * Two exports:
 *  - `EmbeddingChoiceView` is the pure presentational surface. It owns no data
 *    fetching, so Storybook can drive every state by passing `options`.
 *  - `EmbeddingChoice` is the container the step mounts: it fetches the options,
 *    saves an entered key through the existing /config path, and re-reads.
 */

import { useCallback, useEffect, useState } from "react";

import { updateConfig } from "../../../api/client";
import {
  EMBEDDING_OPTIONS_FALLBACK,
  type EmbeddingOptions,
  fetchEmbeddingOptions,
  installGbrain,
  resolveEmbedder,
} from "../../../api/knowledge";
import { ONBOARDING_EMBEDDING_COPY as COPY } from "./wizardSteps";

/** How often to re-read the options while an install is running. */
const INSTALL_POLL_MS = 2_000;
/** Hard ceiling on the poll so a wedged install never loops forever (~6 min). */
const INSTALL_POLL_CAP_MS = 6 * 60 * 1_000;

interface EmbeddingChoiceViewProps {
  /** The current embedding options (drives every state). */
  options: EmbeddingOptions;
  /** Controlled value of the OpenAI key input. */
  keyValue: string;
  /** Update the key input value. */
  onKeyChange: (value: string) => void;
  /** Persist the entered key via the existing /config path. */
  onSaveKey: () => void;
  /** True while the save round-trip is in flight. */
  saving: boolean;
  /** A human-readable save error, or null when the last save was clean. */
  saveError: string | null;
  /** True once the user has picked the local (Ollama) semantic path. */
  ollamaChosen: boolean;
  /** Signal intent to use the local embeddings path (surfaces the installer). */
  onChooseOllama: () => void;
  /** True while the install POST round-trip is in flight. */
  installBusy: boolean;
  /** Kick off (or retry) the gbrain install. */
  onInstallGbrain: () => void;
}

/** The Ollama setup command, using the broker's model id when it gave one. */
function ollamaCommand(options: EmbeddingOptions): string {
  return options.ollama_model
    ? `ollama pull ${options.ollama_model}`
    : COPY.ollamaModelFallback;
}

/** Small check glyph for the success state. Decorative; the text carries meaning. */
function CheckMark() {
  return (
    <svg
      className="onboarding-embedding-check"
      viewBox="0 0 16 16"
      width="16"
      height="16"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M13.5 4.5 6.5 11.5 3 8" />
    </svg>
  );
}

interface InstallPanelProps {
  state: EmbeddingOptions["install_state"];
  progress: string;
  error: string;
  busy: boolean;
  onInstall: () => void;
}

/**
 * The gbrain install affordance. One block, four states: a one-time consent +
 * primary button (idle), a live progress row with a spinner (installing), a
 * ready line (installed), and a failure line with the keyword fallback plus a
 * retry (error). It never blocks the wizard — keyword search stays the default
 * and the install, once kicked off, continues in the background.
 */
function InstallPanel({
  state,
  progress,
  error,
  busy,
  onInstall,
}: InstallPanelProps) {
  if (state === "installing") {
    return (
      <div
        className="onboarding-embedding-install"
        data-state="installing"
        data-testid="onboarding-embedding-install"
        role="status"
        aria-live="polite"
      >
        <span className="onboarding-embedding-spinner" aria-hidden="true" />
        <div className="onboarding-embedding-install-body">
          <p className="onboarding-embedding-install-title">
            {COPY.install.installing}
          </p>
          <p
            className="onboarding-embedding-install-progress"
            data-testid="onboarding-embedding-install-progress"
          >
            {progress.trim() || COPY.install.progressPending}
          </p>
          <p className="onboarding-embedding-install-hint">
            {COPY.install.installingHint}
          </p>
        </div>
      </div>
    );
  }

  if (state === "installed") {
    return (
      <p
        className="onboarding-embedding-success"
        data-testid="onboarding-embedding-install"
        data-state="installed"
      >
        <CheckMark />
        {COPY.install.installed}
      </p>
    );
  }

  if (state === "error") {
    return (
      <div
        className="onboarding-embedding-install"
        data-state="error"
        data-testid="onboarding-embedding-install"
      >
        <div className="onboarding-embedding-install-body">
          <p
            className="onboarding-embedding-install-fail"
            role="alert"
            data-testid="onboarding-embedding-install-error"
          >
            {error.trim() || COPY.install.errorFallback}
          </p>
          <p className="onboarding-embedding-install-hint">
            {COPY.install.keywordFallback}
          </p>
        </div>
        <button
          type="button"
          className="btn btn-secondary onboarding-embedding-install-retry"
          onClick={onInstall}
          disabled={busy}
          data-testid="onboarding-embedding-install-retry"
        >
          {COPY.install.retry}
        </button>
      </div>
    );
  }

  // idle: the one-time consent and the primary call to action.
  return (
    <div
      className="onboarding-embedding-install"
      data-state="idle"
      data-testid="onboarding-embedding-install"
    >
      <p className="onboarding-embedding-install-consent">
        {COPY.install.consent}
      </p>
      <button
        type="button"
        className="btn btn-primary onboarding-embedding-install-cta"
        onClick={onInstall}
        disabled={busy}
        data-testid="onboarding-embedding-install-cta"
      >
        {COPY.install.cta}
      </button>
    </div>
  );
}

interface AlternativesProps {
  options: EmbeddingOptions;
  resolved: ReturnType<typeof resolveEmbedder>;
  showOllamaChoose: boolean;
  onChooseOllama: () => void;
}

/** The two no-key alternatives, in EnsureBrain priority order: Ollama, keyword. */
function EmbeddingAlternatives({
  options,
  resolved,
  showOllamaChoose,
  onChooseOllama,
}: AlternativesProps) {
  return (
    <>
      <p className="onboarding-embedding-alts-label">
        {COPY.alternativesLabel}
      </p>
      <ul className="onboarding-embedding-alts">
        <li
          className="onboarding-embedding-alt"
          data-available={options.ollama_available}
          data-active={resolved === "ollama"}
          data-testid="onboarding-embedding-alt-ollama"
        >
          <span className="onboarding-embedding-alt-title">
            {COPY.ollamaTitle}
            {resolved === "ollama" ? (
              <span className="onboarding-embedding-alt-active">Active</span>
            ) : null}
          </span>
          {options.ollama_available ? (
            <span className="onboarding-embedding-alt-hint">
              {COPY.ollamaAvailable}
            </span>
          ) : (
            <span className="onboarding-embedding-alt-hint">
              {COPY.ollamaSetupPrefix}
              <code className="onboarding-embedding-code">
                {ollamaCommand(options)}
              </code>
              {COPY.ollamaSetupSuffix}
            </span>
          )}
          {showOllamaChoose ? (
            <button
              type="button"
              className="onboarding-embedding-alt-choose"
              onClick={onChooseOllama}
              data-testid="onboarding-embedding-alt-ollama-choose"
            >
              {COPY.ollamaChoose}
            </button>
          ) : null}
        </li>
        <li
          className="onboarding-embedding-alt"
          data-available={true}
          data-active={resolved === "keyword"}
          data-testid="onboarding-embedding-alt-keyword"
        >
          <span className="onboarding-embedding-alt-title">
            {COPY.keywordTitle}
            {resolved === "keyword" ? (
              <span className="onboarding-embedding-alt-active">Active</span>
            ) : null}
          </span>
          <span className="onboarding-embedding-alt-hint">
            {COPY.keywordHint}
          </span>
        </li>
      </ul>
    </>
  );
}

export function EmbeddingChoiceView({
  options,
  keyValue,
  onKeyChange,
  onSaveKey,
  saving,
  saveError,
  ollamaChosen,
  onChooseOllama,
  installBusy,
  onInstallGbrain,
}: EmbeddingChoiceViewProps) {
  const resolved = resolveEmbedder(options);
  const keySet = resolved === "openai";
  const statusName =
    resolved === "openai"
      ? COPY.statusOpenAI
      : resolved === "ollama"
        ? COPY.statusOllama
        : COPY.statusKeyword;

  const onSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSaveKey();
  };

  const canSave = keyValue.trim().length > 0 && !saving;

  // The install affordance is for the user who wants semantic memory but has no
  // gbrain index yet. "Wants semantic memory" means they saved an OpenAI key or
  // picked the local path. We also keep the panel up whenever an install is
  // mid-flight or has errored, so a run that started earlier stays visible.
  const wantsSemantic = options.openai_key_set || ollamaChosen;
  const showInstall =
    !options.gbrain_installed &&
    (wantsSemantic || options.install_state !== "idle");
  const showOllamaChoose = !(options.gbrain_installed || ollamaChosen);

  return (
    <section
      className="onboarding-embedding"
      aria-labelledby="onboarding-embedding-heading"
      data-testid="onboarding-embedding"
    >
      <div className="onboarding-embedding-head">
        <h3
          id="onboarding-embedding-heading"
          className="onboarding-embedding-heading"
        >
          {COPY.heading}
        </h3>
        <p
          className="onboarding-embedding-status"
          data-embedder={resolved}
          data-testid="onboarding-embedding-status"
        >
          {COPY.statusLabel} <strong>{statusName}</strong>
        </p>
      </div>

      <p className="onboarding-embedding-note">{COPY.note}</p>

      {keySet ? (
        <p
          className="onboarding-embedding-success"
          data-testid="onboarding-embedding-success"
        >
          <CheckMark />
          {COPY.openaiSet}
        </p>
      ) : (
        <>
          <form className="onboarding-embedding-key" onSubmit={onSubmit}>
            <div className="onboarding-embedding-key-head">
              <label
                className="onboarding-team-label"
                htmlFor="onboarding-embedding-openai-key"
              >
                {COPY.openaiLabel}
              </label>
              <span className="onboarding-embedding-recommended">
                {COPY.openaiRecommended}
              </span>
            </div>
            <div className="onboarding-embedding-key-row">
              <input
                id="onboarding-embedding-openai-key"
                className="onboarding-team-input"
                type="password"
                value={keyValue}
                placeholder={COPY.openaiPlaceholder}
                autoComplete="off"
                spellCheck={false}
                onChange={(event) => onKeyChange(event.target.value)}
                data-testid="onboarding-embedding-openai-key"
              />
              <button
                type="submit"
                className="btn btn-primary onboarding-embedding-save"
                disabled={!canSave}
                data-testid="onboarding-embedding-save"
              >
                {saving ? COPY.savingKey : COPY.saveKey}
              </button>
            </div>
            <p className="onboarding-embedding-hint">{COPY.openaiHint}</p>
            {saveError ? (
              <p
                className="onboarding-embedding-error"
                role="alert"
                data-testid="onboarding-embedding-error"
              >
                {saveError}
              </p>
            ) : null}
          </form>

          <EmbeddingAlternatives
            options={options}
            resolved={resolved}
            showOllamaChoose={showOllamaChoose}
            onChooseOllama={onChooseOllama}
          />

          {showInstall ? (
            <InstallPanel
              state={options.install_state}
              progress={options.install_progress}
              error={options.install_error}
              busy={installBusy}
              onInstall={onInstallGbrain}
            />
          ) : null}
        </>
      )}
    </section>
  );
}

/**
 * Container: fetches the embedding options on mount, saves an entered OpenAI key
 * through the existing /config path, and re-reads so the section flips to the
 * success state. Every failure degrades quietly — the fetch falls back to the
 * keyword default, and a failed save surfaces an inline, non-blocking error —
 * because nothing here is allowed to block onboarding.
 */
export function EmbeddingChoice() {
  const [options, setOptions] = useState<EmbeddingOptions>(
    EMBEDDING_OPTIONS_FALLBACK,
  );
  const [keyValue, setKeyValue] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [ollamaChosen, setOllamaChosen] = useState(false);
  const [installBusy, setInstallBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    fetchEmbeddingOptions().then((next) => {
      if (!cancelled) setOptions(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  // Poll while an install runs. Re-runs only when install_state crosses into or
  // out of "installing", so a steady stream of "installing" payloads keeps the
  // same timer chain rather than restarting it. Bounded by INSTALL_POLL_CAP_MS
  // so a wedged install degrades to an error instead of polling forever, and
  // every timer is cleared on unmount (no leaks, no setState after teardown).
  useEffect(() => {
    if (options.install_state !== "installing") return;
    let cancelled = false;
    const startedAt = Date.now();
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      if (cancelled) return;
      if (Date.now() - startedAt > INSTALL_POLL_CAP_MS) {
        setOptions((prev) => ({ ...prev, install_state: "error" }));
        return;
      }
      // fetchEmbeddingOptions never throws (it degrades internally), so a failed
      // read simply yields the keyword fallback and the loop settles.
      const next = await fetchEmbeddingOptions();
      if (cancelled) return;
      setOptions(next);
      if (next.install_state === "installing") {
        timer = setTimeout(tick, INSTALL_POLL_MS);
      }
    };
    timer = setTimeout(tick, INSTALL_POLL_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [options.install_state]);

  const onSaveKey = useCallback(async () => {
    const trimmed = keyValue.trim();
    if (!trimmed || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      await updateConfig({ openai_api_key: trimmed });
      setKeyValue("");
      setOptions(await fetchEmbeddingOptions());
    } catch {
      setSaveError(COPY.saveError);
    } finally {
      setSaving(false);
    }
  }, [keyValue, saving]);

  const onChooseOllama = useCallback(() => setOllamaChosen(true), []);

  const onInstallGbrain = useCallback(async () => {
    if (installBusy) return;
    setInstallBusy(true);
    try {
      // The 202 echoes the current options, where install_state has usually
      // flipped to "installing"; setting it here starts the poll effect.
      setOptions(await installGbrain());
    } finally {
      setInstallBusy(false);
    }
  }, [installBusy]);

  return (
    <EmbeddingChoiceView
      options={options}
      keyValue={keyValue}
      onKeyChange={setKeyValue}
      onSaveKey={onSaveKey}
      saving={saving}
      saveError={saveError}
      ollamaChosen={ollamaChosen}
      onChooseOllama={onChooseOllama}
      installBusy={installBusy}
      onInstallGbrain={onInstallGbrain}
    />
  );
}
