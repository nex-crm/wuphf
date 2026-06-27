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
  resolveEmbedder,
} from "../../../api/knowledge";
import { ONBOARDING_EMBEDDING_COPY as COPY } from "./wizardSteps";

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

export function EmbeddingChoiceView({
  options,
  keyValue,
  onKeyChange,
  onSaveKey,
  saving,
  saveError,
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
                  <span className="onboarding-embedding-alt-active">
                    Active
                  </span>
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
                  <span className="onboarding-embedding-alt-active">
                    Active
                  </span>
                ) : null}
              </span>
              <span className="onboarding-embedding-alt-hint">
                {COPY.keywordHint}
              </span>
            </li>
          </ul>
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

  useEffect(() => {
    let cancelled = false;
    fetchEmbeddingOptions().then((next) => {
      if (!cancelled) setOptions(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);

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

  return (
    <EmbeddingChoiceView
      options={options}
      keyValue={keyValue}
      onKeyChange={setKeyValue}
      onSaveKey={onSaveKey}
      saving={saving}
      saveError={saveError}
    />
  );
}
