import { useEffect, useRef, useState } from "react";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { ApiKeyRow } from "./ApiKeyRow";
import { ArrowIcon, EnterHint } from "./components";
import { API_KEY_FIELDS, LOCAL_PROVIDER_LABELS, RUNTIMES } from "./constants";
import { LocalLLMPicker } from "./LocalLLMPicker";
import type { PackPreviewRequirement } from "./packPreview";
import {
  canSetupContinue,
  detectedBinary,
  runtimeIsReady,
} from "./runtime-helpers";
import type { PrereqResult, RuntimeSpec } from "./types";

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
  localLLMState: LocalLLMState;
  // Requirements derived from the selected pack. Empty array means
  // "no pack selected" or "pack has no declared requirements" — both
  // result in the panel being hidden entirely.
  packRequirements: PackPreviewRequirement[];
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

// PROVIDER_DOCTOR_PATH is the in-app route for the health & access panel.
// We link to it from the requirements panel when a required runtime is missing
// so the user can verify their install without leaving the flow.
const PROVIDER_DOCTOR_PATH = "/apps/health-check";

// Maps a requirement name to the matching RUNTIMES entry label so we can
// check whether the user has that runtime installed and selected.
function runtimeLabelForRequirement(name: string): string | undefined {
  const nameLower = name.toLowerCase();
  return RUNTIMES.find(
    (r) => r.label.toLowerCase() === nameLower || r.binary === nameLower,
  )?.label;
}

// Maps a requirement name to the matching API_KEY_FIELDS entry so we can
// tell whether the user has provided a key for that provider.
function apiKeyFieldForRequirement(name: string): string | undefined {
  const nameLower = name.toLowerCase();
  return API_KEY_FIELDS.find(
    (f) =>
      f.label.toLowerCase() === nameLower ||
      f.key.toLowerCase() === nameLower ||
      // e.g. "anthropic_api_key" matches "ANTHROPIC_API_KEY"
      f.key.toLowerCase() === name.toUpperCase(),
  )?.key;
}

type ReqReadiness = "ok" | "missing" | "unknown";

function requirementReadiness(
  req: PackPreviewRequirement,
  prereqs: PrereqResult[],
  prereqsError: string,
  runtimePriority: string[],
  apiKeys: Record<string, string>,
): ReqReadiness {
  if (req.kind === "runtime") {
    const label = runtimeLabelForRequirement(req.name);
    if (!label) return "unknown";
    const selected = runtimePriority.includes(label);
    if (!selected) return "missing";
    return runtimeIsReady(label, prereqs, prereqsError) ? "ok" : "missing";
  }
  if (req.kind === "api-key") {
    // We deliberately do NOT check the key value — only whether the field
    // has been filled. This never exposes key material in the UI.
    const fieldKey = apiKeyFieldForRequirement(req.name);
    if (!fieldKey) return "unknown";
    const filled = (apiKeys[fieldKey] ?? "").trim().length > 0;
    return filled ? "ok" : "missing";
  }
  // local-tool and unrecognized kinds: we cannot detect them reliably,
  // so show as "unknown" (neutral) rather than blocking.
  return "unknown";
}

interface RequirementRowProps {
  req: PackPreviewRequirement;
  readiness: ReqReadiness;
}

function RequirementRow({ req, readiness }: RequirementRowProps) {
  const statusDot =
    readiness === "ok" ? "ok" : readiness === "missing" ? "missing" : "unknown";
  const badgeLabel = req.required ? "required" : "optional";
  const badgeClass = req.required
    ? "pack-req-badge pack-req-badge--required"
    : "pack-req-badge pack-req-badge--optional";
  const kindLabel: Record<string, string> = {
    runtime: "CLI",
    "api-key": "API key",
    "local-tool": "tool",
  };
  const kindText = kindLabel[req.kind] ?? req.kind;

  return (
    <div
      className="pack-req-row"
      data-testid={`pack-req-row-${req.name}`}
      data-readiness={readiness}
    >
      <span
        className={`pack-req-dot pack-req-dot--${statusDot}`}
        aria-hidden="true"
      />
      <div className="pack-req-body">
        <span className="pack-req-name">{req.name}</span>
        <span className="pack-req-kind">{kindText}</span>
        {req.detail ? (
          <span className="pack-req-detail">{req.detail}</span>
        ) : null}
      </div>
      <span className={badgeClass} data-testid={`pack-req-badge-${req.name}`}>
        {badgeLabel}
      </span>
    </div>
  );
}

interface PackRequirementsPanelProps {
  requirements: PackPreviewRequirement[];
  prereqs: PrereqResult[];
  prereqsError: string;
  runtimePriority: string[];
  apiKeys: Record<string, string>;
}

export function PackRequirementsPanel({
  requirements,
  prereqs,
  prereqsError,
  runtimePriority,
  apiKeys,
}: PackRequirementsPanelProps) {
  if (requirements.length === 0) return null;

  const rows = requirements.map((req) => ({
    req,
    readiness: requirementReadiness(
      req,
      prereqs,
      prereqsError,
      runtimePriority,
      apiKeys,
    ),
  }));

  // Show the Provider Doctor link when any REQUIRED runtime is missing so the
  // user can verify their installs. Optional gaps are informational only.
  const hasRequiredRuntimeMissing = rows.some(
    (r) =>
      r.req.required && r.req.kind === "runtime" && r.readiness === "missing",
  );

  const requiredRows = rows.filter((r) => r.req.required);
  const optionalRows = rows.filter((r) => !r.req.required);

  return (
    <div
      className="pack-requirements-panel"
      data-testid="pack-requirements-panel"
    >
      <div className="pack-req-header">
        <p className="pack-req-title">Pack requirements</p>
        <p className="pack-req-subtitle">
          What this pack needs to run. Check off any gaps before finishing
          setup.
        </p>
      </div>

      {requiredRows.length > 0 && (
        <div className="pack-req-group">
          <p className="pack-req-group-label">Required</p>
          {requiredRows.map(({ req, readiness }) => (
            <RequirementRow key={req.name} req={req} readiness={readiness} />
          ))}
        </div>
      )}

      {optionalRows.length > 0 && (
        <div className="pack-req-group">
          <p className="pack-req-group-label">Optional</p>
          {optionalRows.map(({ req, readiness }) => (
            <RequirementRow key={req.name} req={req} readiness={readiness} />
          ))}
        </div>
      )}

      {hasRequiredRuntimeMissing && (
        <div
          className="pack-req-doctor-hint"
          data-testid="pack-req-doctor-hint"
        >
          <span className="pack-req-doctor-icon" aria-hidden="true">
            ⚠
          </span>
          <span>
            A required runtime is not ready.{" "}
            <a
              href={PROVIDER_DOCTOR_PATH}
              className="pack-req-doctor-link"
              data-testid="pack-req-doctor-link"
            >
              Open Provider Doctor
            </a>{" "}
            to check your install.
          </span>
        </div>
      )}
    </div>
  );
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

export function SetupStep({
  prereqStatus,
  runtimeSelection,
  apiKeyState,
  localLLMState,
  packRequirements,
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

      {packRequirements.length > 0 && (
        <PackRequirementsPanel
          requirements={packRequirements}
          prereqs={prereqs}
          prereqsError={prereqsError}
          runtimePriority={runtimePriority}
          apiKeys={apiKeys}
        />
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
