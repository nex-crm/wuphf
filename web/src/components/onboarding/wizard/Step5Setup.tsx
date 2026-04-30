import { useState } from "react";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { ApiKeyRow } from "./ApiKeyRow";
import { ArrowIcon, EnterHint } from "./components";
import {
  API_KEY_FIELDS,
  LOCAL_PROVIDER_LABELS,
  MEMORY_BACKEND_OPTIONS,
  RUNTIMES,
} from "./constants";
import { LocalLLMPicker } from "./LocalLLMPicker";
import { detectedBinary, runtimeIsReady } from "./runtime-helpers";
import type { MemoryBackend, PrereqResult } from "./types";

interface SetupStepProps {
  prereqs: PrereqResult[];
  prereqsLoading: boolean;
  prereqsError: string;
  runtimePriority: string[];
  onToggleRuntime: (label: string) => void;
  onReorderRuntime: (label: string, direction: -1 | 1) => void;
  apiKeys: Record<string, string>;
  onChangeApiKey: (key: string, value: string) => void;
  memoryBackend: MemoryBackend;
  onChangeMemoryBackend: (value: MemoryBackend) => void;
  nexApiKey: string;
  onChangeNexApiKey: (v: string) => void;
  gbrainOpenAIKey: string;
  onChangeGBrainOpenAIKey: (v: string) => void;
  gbrainAnthropicKey: string;
  onChangeGBrainAnthropicKey: (v: string) => void;
  // Local-LLM opt-in chosen here; submitted with the rest of the wizard
  // payload at /onboarding/complete and applied to llm_provider so the
  // user's selection takes effect on first agent turn.
  localProvider: string;
  onSelectLocalProvider: (kind: string) => void;
  onNext: () => void;
  onBack: () => void;
}

export function SetupStep({
  prereqs,
  prereqsLoading,
  prereqsError,
  runtimePriority,
  onToggleRuntime,
  onReorderRuntime,
  apiKeys,
  onChangeApiKey,
  memoryBackend,
  onChangeMemoryBackend,
  nexApiKey,
  onChangeNexApiKey,
  gbrainOpenAIKey,
  onChangeGBrainOpenAIKey,
  gbrainAnthropicKey,
  onChangeGBrainAnthropicKey,
  localProvider,
  onSelectLocalProvider,
  onNext,
  onBack,
}: SetupStepProps) {
  // localModeOn governs whether the second-step LocalLLMPicker is
  // shown beneath the runtime grid. Initialised from the parent's
  // localProvider so re-entering the step preserves the user's
  // earlier "I want a local model" choice. Toggling the meta-tile
  // off clears localProvider via onSelectLocalProvider("").
  const [localModeOn, setLocalModeOn] = useState<boolean>(localProvider !== "");

  // Any priority slot that satisfies runtimeIsReady satisfies the gate.
  const hasInstalledSelection = runtimePriority.some((label) =>
    runtimeIsReady(label, prereqs, prereqsError),
  );
  const hasAnyApiKey = Object.values(apiKeys).some((v) => v.trim().length > 0);
  // GBrain requires an OpenAI key to function — the TUI gates on this in
  // InitGBrainOpenAIKey (see internal/tui/init_flow.go:215). Mirror the
  // gate here so the wizard doesn't let users commit an unusable config.
  const gbrainSelected = memoryBackend === "gbrain";
  const gbrainOpenAIMissing =
    gbrainSelected && gbrainOpenAIKey.trim().length === 0;
  // Picking a local LLM is an alternative path — once selected the user
  // doesn't need a cloud CLI installed or any cloud API key set.
  const hasLocalProvider = localProvider.trim().length > 0;
  const canContinue =
    (hasInstalledSelection || hasAnyApiKey || hasLocalProvider) &&
    !gbrainOpenAIMissing;

  return (
    <div className="wizard-step">
      <div className="wizard-panel">
        <p className="wizard-panel-title">How should agents run?</p>
        <p
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            margin: "-8px 0 12px 0",
          }}
        >
          Pick the CLIs you have installed. Each CLI&apos;s login handles its
          own provider auth, so no API keys are needed. Select multiple to set a
          fallback order — if the first one fails, agents fall through to the
          next.
        </p>

        {prereqsLoading ? (
          <div
            style={{
              color: "var(--text-tertiary)",
              fontSize: 13,
              padding: "8px 0",
            }}
          >
            Checking which CLIs are installed&hellip;
          </div>
        ) : prereqsError ? (
          <div
            data-testid="prereqs-error-banner"
            role="alert"
            style={{
              fontSize: 12,
              color: "var(--danger-500, #c33)",
              padding: "10px 12px",
              background: "var(--danger-50, #fee)",
              border: "1px solid var(--danger-200, #fcc)",
              borderRadius: 6,
              marginBottom: 12,
            }}
          >
            Could not detect installed CLIs:{" "}
            <code style={{ fontFamily: "var(--font-mono)" }}>
              {prereqsError}
            </code>
            . You can still select a runtime below or add an API key to
            continue.
          </div>
        ) : null}
        {prereqsLoading ? null : (
          <div className="runtime-grid">
            {RUNTIMES.map((spec) => {
              const detection = detectedBinary(prereqs, spec.binary);
              const installed = Boolean(detection?.found);
              const selectable = installed || Boolean(prereqsError);
              const priorityIdx = runtimePriority.indexOf(spec.label);
              const selected = priorityIdx >= 0;
              const classes = [
                "runtime-tile",
                selected ? "selected" : "",
                selectable ? "" : "disabled",
              ]
                .filter(Boolean)
                .join(" ");
              return (
                <button
                  key={spec.label}
                  className={classes}
                  data-testid={`setup-runtime-tile-${spec.label}`}
                  onClick={() => {
                    if (!selectable) return;
                    onToggleRuntime(spec.label);
                  }}
                  type="button"
                  disabled={!selectable}
                  aria-disabled={!selectable}
                  aria-pressed={selected}
                  title={
                    installed
                      ? detection?.version
                        ? `${spec.label} — ${detection.version}`
                        : spec.label
                      : prereqsError
                        ? `${spec.label} — detection failed, select if installed`
                        : `${spec.label} — not installed`
                  }
                >
                  {selected && (
                    <span
                      className="runtime-priority-badge"
                      aria-label={`Priority ${priorityIdx + 1}`}
                    >
                      {priorityIdx + 1}
                    </span>
                  )}
                  <div className="runtime-tile-head">
                    <span
                      className={`runtime-tile-status ${installed ? "installed" : ""}`}
                      aria-hidden="true"
                    />
                    {spec.label}
                  </div>
                  <div className="runtime-tile-meta">
                    {installed ? (
                      detection?.version ? (
                        detection.version
                      ) : (
                        "Installed"
                      )
                    ) : prereqsError ? (
                      "Select if installed"
                    ) : (
                      <>
                        Not installed{" · "}
                        <a
                          className="runtime-tile-install-link"
                          href={spec.installUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          onClick={(e) => e.stopPropagation()}
                        >
                          install
                        </a>
                      </>
                    )}
                  </div>
                </button>
              );
            })}
            {/* Run a local model — peer tile alongside the cloud CLIs.
                Selecting it reveals the second-step picker (LocalLLMPicker)
                below the grid. The dot stays grey because there's no single
                binary to detect; the picker resolves which runtime is
                actually running. */}
            <button
              type="button"
              className={["runtime-tile", localModeOn ? "selected" : ""]
                .filter(Boolean)
                .join(" ")}
              onClick={() => {
                const next = !localModeOn;
                setLocalModeOn(next);
                if (!next) onSelectLocalProvider("");
              }}
              aria-pressed={localModeOn}
              data-testid="onboarding-local-llm-toggle"
              title="Run a local model on this machine"
            >
              <div className="runtime-tile-head">
                <span className="runtime-tile-status" aria-hidden="true" />
                Run a local model
              </div>
              <div className="runtime-tile-meta">
                {localProvider
                  ? (LOCAL_PROVIDER_LABELS.find((m) => m.kind === localProvider)
                      ?.label ?? "selected")
                  : "MLX-LM, Ollama, or Exo"}
              </div>
            </button>
          </div>
        )}

        {localModeOn && (
          <LocalLLMPicker
            selected={localProvider}
            onSelect={onSelectLocalProvider}
          />
        )}

        {runtimePriority.length > 1 && (
          <div className="runtime-priority-controls">
            <p className="runtime-priority-title">Fallback order</p>
            <p className="runtime-priority-hint">
              Agents try these in order. Drop a local model into the chain so a
              cloud quota hit falls through to your machine instead of
              pay-as-you-go billing. Use the arrows to reorder.
            </p>
            {runtimePriority.map((label, idx) => (
              <div key={label} className="runtime-priority-row">
                <span className="runtime-priority-row-rank">#{idx + 1}</span>
                <span className="runtime-priority-row-label">{label}</span>
                <button
                  type="button"
                  className="runtime-priority-btn"
                  onClick={() => onReorderRuntime(label, -1)}
                  disabled={idx === 0}
                  aria-label={`Move ${label} up`}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="runtime-priority-btn"
                  onClick={() => onReorderRuntime(label, 1)}
                  disabled={idx === runtimePriority.length - 1}
                  aria-label={`Move ${label} down`}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="runtime-priority-btn"
                  onClick={() => onToggleRuntime(label)}
                  aria-label={`Remove ${label}`}
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        )}

        <div
          style={{
            marginTop: 16,
            paddingTop: 16,
            borderTop: "1px solid var(--border)",
          }}
        >
          <p
            style={{
              fontSize: 13,
              fontWeight: 600,
              margin: "0 0 4px 0",
              color: "var(--text)",
            }}
          >
            API keys{" "}
            {hasInstalledSelection ? "(optional fallback)" : "(required)"}
          </p>
          <p
            style={{
              fontSize: 12,
              color: "var(--text-secondary)",
              margin: "0 0 12px 0",
            }}
          >
            {hasInstalledSelection
              ? "Only used if every selected CLI fails. Leave blank to rely on the CLI login."
              : "No installed CLI selected. Add at least one key so agents can reason."}
          </p>
          {API_KEY_FIELDS.map((field) => (
            <ApiKeyRow
              key={field.key}
              field={field}
              value={apiKeys[field.key] ?? ""}
              onChange={(v) => onChangeApiKey(field.key, v)}
            />
          ))}
        </div>
      </div>

      <div className="wizard-panel">
        <p className="wizard-panel-title">Organizational memory</p>
        <p
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            margin: "-8px 0 12px 0",
          }}
        >
          Where agents store shared context, relationships, and learnings across
          sessions. You can change this later in Settings or via{" "}
          <code>--memory-backend</code>.
        </p>
        <div className="runtime-grid">
          {MEMORY_BACKEND_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              className={`runtime-tile ${memoryBackend === opt.value ? "selected" : ""}`}
              onClick={() => onChangeMemoryBackend(opt.value)}
              type="button"
              title={opt.hint}
            >
              <div style={{ fontWeight: 600 }}>{opt.label}</div>
              <div
                style={{
                  fontSize: 11,
                  color: "var(--text-tertiary)",
                  marginTop: 4,
                  fontWeight: 400,
                }}
              >
                {opt.hint}
              </div>
            </button>
          ))}
        </div>

        {gbrainSelected && (
          <div className="wiz-backend-keys">
            <p className="wiz-backend-keys-title">GBrain keys</p>
            <p className="wiz-backend-keys-hint">
              GBrain uses OpenAI for embeddings (required) and optionally
              Anthropic for reasoning.
            </p>
            <div className="form-group">
              <label className="label" htmlFor="wiz-gbrain-openai">
                OpenAI API key <span style={{ color: "var(--red)" }}>*</span>
              </label>
              <input
                className="input"
                id="wiz-gbrain-openai"
                type="password"
                placeholder="sk-..."
                value={gbrainOpenAIKey}
                onChange={(e) => onChangeGBrainOpenAIKey(e.target.value)}
                autoComplete="off"
              />
              {gbrainOpenAIMissing && (
                <p style={{ color: "var(--red)", fontSize: 11, marginTop: 4 }}>
                  Required: GBrain can&apos;t create embeddings without an
                  OpenAI key.
                </p>
              )}
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="label" htmlFor="wiz-gbrain-anthropic">
                Anthropic API key{" "}
                <span style={{ fontSize: 11, color: "var(--text-tertiary)" }}>
                  (optional)
                </span>
              </label>
              <input
                className="input"
                id="wiz-gbrain-anthropic"
                type="password"
                placeholder="sk-ant-..."
                value={gbrainAnthropicKey}
                onChange={(e) => onChangeGBrainAnthropicKey(e.target.value)}
                autoComplete="off"
              />
            </div>
          </div>
        )}
      </div>

      {memoryBackend === "nex" && (
        // Only show the Nex API key panel when the chosen memory backend
        // actually needs it. Team wiki, GBrain, and "None" don't talk to
        // Nex's hosted memory — surfacing the input would suggest the
        // user has a missing piece when they don't.
        <div className="wizard-panel" data-testid="wizard-nex-api-key-panel">
          <p className="wizard-panel-title">Nex API key</p>
          <p
            style={{
              fontSize: 12,
              color: "var(--text-secondary)",
              margin: "-8px 0 12px 0",
            }}
          >
            Unlocks the hosted memory graph plus managed integrations (HubSpot,
            Slack, Gmail, Calendar, …) so agents can read your tools without you
            wiring each one up. You can skip this and paste later from Settings.
            Don&apos;t have one? Sign up on the Identity step above.
          </p>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="label" htmlFor="wiz-nex-api-key">
              Nex API key{" "}
              <span style={{ fontSize: 11, color: "var(--text-tertiary)" }}>
                (optional, paste if you have one)
              </span>
            </label>
            <input
              className="input"
              id="wiz-nex-api-key"
              type="password"
              placeholder="nex-..."
              value={nexApiKey}
              onChange={(e) => onChangeNexApiKey(e.target.value)}
              autoComplete="off"
            />
          </div>
        </div>
      )}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button
          className="btn btn-primary"
          onClick={onNext}
          disabled={!canContinue}
          type="button"
        >
          {ONBOARDING_COPY.step2_cta}
          <ArrowIcon />
          <EnterHint />
        </button>
      </div>
    </div>
  );
}
