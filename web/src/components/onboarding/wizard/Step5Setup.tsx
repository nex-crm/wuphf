import { useEffect, useRef, useState } from "react";

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
import {
  canSetupContinue,
  detectedBinary,
  runtimeIsReady,
} from "./runtime-helpers";
import type { MemoryBackend, PrereqResult, RuntimeSpec } from "./types";

interface PrereqStatus {
  items: PrereqResult[];
  loading: boolean;
  error: string;
}

interface RuntimeSelection {
  priority: string[];
  onToggle: (label: string) => void;
  onReorder: (label: string, direction: -1 | 1) => void;
}

interface ApiKeyState {
  values: Record<string, string>;
  onChange: (key: string, value: string) => void;
}

interface MemoryState {
  backend: MemoryBackend;
  onChangeBackend: (value: MemoryBackend) => void;
  nexApiKey: string;
  onChangeNexApiKey: (v: string) => void;
  gbrainOpenAIKey: string;
  onChangeGBrainOpenAIKey: (v: string) => void;
  gbrainAnthropicKey: string;
  onChangeGBrainAnthropicKey: (v: string) => void;
}

interface LocalLLMState {
  // Local-LLM opt-in chosen here; submitted with the rest of the wizard
  // payload at /onboarding/complete and applied to llm_provider so the
  // user's selection takes effect on first agent turn.
  provider: string;
  onSelectProvider: (kind: string) => void;
}

interface SetupStepProps {
  prereqStatus: PrereqStatus;
  runtimeSelection: RuntimeSelection;
  apiKeyState: ApiKeyState;
  memoryState: MemoryState;
  localLLMState: LocalLLMState;
  onNext: () => void;
  onBack: () => void;
}

interface RuntimeTileProps {
  spec: RuntimeSpec;
  prereqs: PrereqResult[];
  prereqsError: string;
  runtimePriority: string[];
  onToggleRuntime: (label: string) => void;
}

function runtimeTileTitle(
  spec: RuntimeSpec,
  installed: boolean,
  prereqsError: string,
  version?: string,
) {
  if (installed) return version ? `${spec.label} — ${version}` : spec.label;
  if (prereqsError) {
    return `${spec.label} — detection failed, select if installed`;
  }
  return `${spec.label} — not installed`;
}

function RuntimeTileMeta({
  spec,
  installed,
  prereqsError,
  version,
}: {
  spec: RuntimeSpec;
  installed: boolean;
  prereqsError: string;
  version?: string;
}) {
  if (installed) return version ? version : "Installed";
  if (prereqsError) return "Select if installed";
  return (
    <>
      Not installed{" · "}
      <a
        className="runtime-tile-install-link"
        href={spec.installUrl}
        target="_blank"
        rel="noopener noreferrer"
      >
        install
      </a>
    </>
  );
}

function RuntimeTile({
  spec,
  prereqs,
  prereqsError,
  runtimePriority,
  onToggleRuntime,
}: RuntimeTileProps) {
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
  const title = runtimeTileTitle(
    spec,
    installed,
    prereqsError,
    detection?.version,
  );
  const content = (
    <>
      {selected && (
        <span
          className="runtime-priority-badge"
          title={`Priority ${priorityIdx + 1}`}
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
        <RuntimeTileMeta
          spec={spec}
          installed={installed}
          prereqsError={prereqsError}
          version={detection?.version}
        />
      </div>
    </>
  );

  if (!selectable) {
    return (
      <div
        className={classes}
        data-testid={`setup-runtime-tile-${spec.label}`}
        aria-disabled="true"
        title={title}
      >
        {content}
      </div>
    );
  }

  return (
    <button
      className={classes}
      data-testid={`setup-runtime-tile-${spec.label}`}
      onClick={() => onToggleRuntime(spec.label)}
      type="button"
      aria-disabled="false"
      aria-pressed={selected}
      title={title}
    >
      {content}
    </button>
  );
}

interface MemoryBackendPanelProps {
  memoryBackend: MemoryBackend;
  onChangeMemoryBackend: (value: MemoryBackend) => void;
  gbrainOpenAIKey: string;
  onChangeGBrainOpenAIKey: (v: string) => void;
  gbrainAnthropicKey: string;
  onChangeGBrainAnthropicKey: (v: string) => void;
}

function MemoryBackendPanel({
  memoryBackend,
  onChangeMemoryBackend,
  gbrainOpenAIKey,
  onChangeGBrainOpenAIKey,
  gbrainAnthropicKey,
  onChangeGBrainAnthropicKey,
}: MemoryBackendPanelProps) {
  const gbrainSelected = memoryBackend === "gbrain";
  const gbrainOpenAIMissing =
    gbrainSelected && gbrainOpenAIKey.trim().length === 0;

  return (
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
            aria-pressed={memoryBackend === opt.value}
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

      {gbrainSelected ? (
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
            {gbrainOpenAIMissing ? (
              <p style={{ color: "var(--red)", fontSize: 11, marginTop: 4 }}>
                Required: GBrain can&apos;t create embeddings without an OpenAI
                key.
              </p>
            ) : null}
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
      ) : null}
    </div>
  );
}

export function SetupStep({
  prereqStatus,
  runtimeSelection,
  apiKeyState,
  memoryState,
  localLLMState,
  onNext,
  onBack,
}: SetupStepProps) {
  const {
    items: prereqs,
    loading: prereqsLoading,
    error: prereqsError,
  } = prereqStatus;
  const {
    priority: runtimePriority,
    onToggle: onToggleRuntime,
    onReorder: onReorderRuntime,
  } = runtimeSelection;
  const { values: apiKeys, onChange: onChangeApiKey } = apiKeyState;
  const {
    backend: memoryBackend,
    onChangeBackend: onChangeMemoryBackend,
    nexApiKey,
    onChangeNexApiKey,
    gbrainOpenAIKey,
    onChangeGBrainOpenAIKey,
    gbrainAnthropicKey,
    onChangeGBrainAnthropicKey,
  } = memoryState;
  const { provider: localProvider, onSelectProvider: onSelectLocalProvider } =
    localLLMState;

  // localModeOn governs whether the second-step LocalLLMPicker is
  // shown beneath the runtime grid. It opens either from the meta-tile
  // or an existing localProvider, and closes again when parent state
  // clears that provider via the fallback-chain remove button.
  const [localModeOn, setLocalModeOn] = useState<boolean>(localProvider !== "");
  const hasSeenLocalProvider = useRef(localProvider.trim().length > 0);

  useEffect(() => {
    const hasLocalProvider = localProvider.trim().length > 0;
    if (hasLocalProvider) {
      hasSeenLocalProvider.current = true;
      setLocalModeOn(true);
    } else if (hasSeenLocalProvider.current) {
      hasSeenLocalProvider.current = false;
      setLocalModeOn(false);
    }
  }, [localProvider]);

  // Any priority slot that satisfies runtimeIsReady satisfies the API-key hint.
  const hasInstalledSelection = runtimePriority.some((label) =>
    runtimeIsReady(label, prereqs, prereqsError),
  );
  const hasRuntimePath =
    hasInstalledSelection || localProvider.trim().length > 0;
  const canContinue = canSetupContinue({
    runtimePriority,
    prereqs,
    prereqsError,
    apiKeys,
    localProvider,
    memoryBackend,
    gbrainOpenAIKey,
  });

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
            {RUNTIMES.filter((spec) => spec.provider !== null).map((spec) => (
              <RuntimeTile
                key={spec.label}
                spec={spec}
                prereqs={prereqs}
                prereqsError={prereqsError}
                runtimePriority={runtimePriority}
                onToggleRuntime={onToggleRuntime}
              />
            ))}
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

        {localModeOn ? (
          <LocalLLMPicker
            selected={localProvider}
            onSelect={onSelectLocalProvider}
          />
        ) : null}

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
            API keys {hasRuntimePath ? "(optional fallback)" : "(required)"}
          </p>
          <p
            style={{
              fontSize: 12,
              color: "var(--text-secondary)",
              margin: "0 0 12px 0",
            }}
          >
            {hasRuntimePath
              ? "Only used if every selected runtime fails. Leave blank to rely on the selected runtime."
              : "No runtime selected. Add at least one key so agents can reason."}
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

      <MemoryBackendPanel
        memoryBackend={memoryBackend}
        onChangeMemoryBackend={onChangeMemoryBackend}
        gbrainOpenAIKey={gbrainOpenAIKey}
        onChangeGBrainOpenAIKey={onChangeGBrainOpenAIKey}
        gbrainAnthropicKey={gbrainAnthropicKey}
        onChangeGBrainAnthropicKey={onChangeGBrainAnthropicKey}
      />

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
                (optional during onboarding)
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
