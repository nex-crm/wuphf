import { useEffect, useMemo, useState } from "react";

import { get, post } from "../../api/client";
import { LocalProviderPicker } from "./LocalProviderPicker";
import { isValidUrl, OpenAICompatibleInput } from "./OpenAICompatibleInput";
import { PrePickApiKeyRow } from "./PrePickApiKeyRow";
import { RuntimeLogo } from "./RuntimeLogos";
import { API_KEY_FIELDS, sanitizeConfigString } from "./runtimeConstants";
import type { PrereqResult, RuntimeSpec } from "./runtimes";
import { RUNTIMES } from "./runtimes";

// Phase 1 of the onboarding-into-office redesign
// (docs/specs/onboarding-into-office.md). The 9-step wizard collapses
// into one pre-office screen -- runtime selection. Everything else
// (company name, blueprint pick, team trim, first task) moves into a
// CEO DM inside the real Shell in Phase 2 and later.
//
// Phase 5 ports the deleted wizard's API-key, local-provider, and
// OpenAI-compatible sections forward as inline sections below the three
// CLI runtime cards. The screen remains a single screen with no pagination.
//
// `onComplete` is invoked after runtime config is saved and the deterministic
// CEO onboarding state machine has entered the greet phase. RootRoute then
// renders the Shell around the CEO DM instead of marking onboarding complete.

interface PrePickScreenProps {
  onComplete: () => void;
}

interface RuntimeCardState {
  spec: RuntimeSpec;
  detected: PrereqResult | undefined;
  available: boolean;
}

interface RuntimeCardProps {
  state: RuntimeCardState;
  prereqsLoaded: boolean;
  isSubmitting: boolean;
  anySubmitting: boolean;
  onPick: (spec: RuntimeSpec) => void;
  onInstall: (url: string) => void;
}

function cardStatusLabel({
  prereqsLoaded,
  available,
  version,
}: {
  prereqsLoaded: boolean;
  available: boolean;
  version: string | undefined;
}): string {
  if (!prereqsLoaded) return "Checking…";
  if (!available) return "Not installed";
  return version ? `Detected · ${version}` : "Detected";
}

function RuntimeCard({
  state,
  prereqsLoaded,
  isSubmitting,
  anySubmitting,
  onPick,
  onInstall,
}: RuntimeCardProps) {
  const { spec, detected, available } = state;
  const statusLabel = isSubmitting
    ? "Starting your office…"
    : cardStatusLabel({
        prereqsLoaded,
        available,
        version: detected?.version,
      });
  const installHint =
    !available && prereqsLoaded ? (
      <div className="pre-pick-card-install">Install &rarr;</div>
    ) : null;
  return (
    <button
      key={spec.label}
      type="button"
      className={`pre-pick-card${available ? " available" : " missing"}`}
      data-testid={`pre-pick-card-${spec.provider}`}
      // CodeRabbit fix (PR #889): guard against clicks during prereq detection
      // and any in-flight submission. Both disable conditions must hold.
      disabled={anySubmitting || !prereqsLoaded}
      onClick={() => {
        // Belt-and-suspenders guard: early-return if prereqs haven't settled.
        if (!prereqsLoaded) return;
        if (!available) {
          onInstall(spec.installUrl);
          return;
        }
        onPick(spec);
      }}
    >
      <div className="pre-pick-card-head">
        <RuntimeLogo label={spec.label} />
        <span className="pre-pick-card-name">{spec.label}</span>
      </div>
      <div className="pre-pick-card-status">{statusLabel}</div>
      {installHint}
    </button>
  );
}

type PrereqsPayload = { prereqs?: PrereqResult[] } | PrereqResult[];

function prereqsFromPayload(data: PrereqsPayload): PrereqResult[] {
  if (Array.isArray(data)) return data;
  return data.prereqs ?? [];
}

// Phase 1 ships the three runtimes the broker can dispatch to: Claude
// Code, Codex, Opencode. Cursor and Windsurf (provider:null in RUNTIMES)
// have no dispatch path so we omit them here.
const DISPATCHABLE_RUNTIMES = RUNTIMES.filter((r) => r.provider !== null);

// Empty initial state for the API-key map.
const EMPTY_API_KEYS: Record<string, string> = {};

/** Returns true when the OAI-compatible endpoint URL+key pair is usable. */
function oaiCompatFilled(url: string): boolean {
  return url.trim().length > 0 && isValidUrl(url);
}

/** Returns true when at least one API key has been pasted. */
function anyApiKeyFilled(keys: Record<string, string>): boolean {
  return API_KEY_FIELDS.some((f) => (keys[f.key] ?? "").trim().length > 0);
}

/** Returns true when provider is a known dispatchable CLI runtime. */
function isCliProvider(provider: RuntimeSpec["provider"] | string): boolean {
  return (
    provider !== null &&
    provider !== "" &&
    provider !== "form" &&
    provider !== "skip"
  );
}

/** Returns true when any of the form sections has meaningful content. */
function hasConfigContent(
  isCliRuntime: boolean,
  localProvider: string,
  oaiUrl: string,
  apiKeys: Record<string, string>,
): boolean {
  return (
    isCliRuntime ||
    localProvider.trim().length > 0 ||
    oaiCompatFilled(oaiUrl) ||
    anyApiKeyFilled(apiKeys)
  );
}

type ConfigPayload = {
  memory_backend: "markdown";
  llm_provider?: string;
  llm_provider_priority?: string[];
  anthropic_api_key?: string;
  openai_api_key?: string;
  gemini_api_key?: string;
  provider_endpoints?: Record<string, { base_url: string }>;
  openclaw_token?: string;
  openclaw_gateway_url?: string;
};

/**
 * Builds the /config POST payload. Extracted from commitChoice to keep the
 * function's cognitive complexity within the 15-branch biome limit.
 * All user-supplied strings are sanitized via sanitizeConfigString.
 */
function buildConfigPayload(
  isCliRuntime: boolean,
  provider: string,
  localProvider: string,
  oaiUrl: string,
  oaiKey: string,
  apiKeys: Record<string, string>,
): ConfigPayload {
  const payload: ConfigPayload = { memory_backend: "markdown" };

  if (isCliRuntime) {
    payload.llm_provider = provider;
    payload.llm_provider_priority = [provider];
  } else if (localProvider.trim().length > 0) {
    payload.llm_provider = localProvider;
    payload.llm_provider_priority = [localProvider];
  } else if (oaiCompatFilled(oaiUrl)) {
    payload.provider_endpoints = {
      "openai-compatible": { base_url: sanitizeConfigString(oaiUrl) },
    };
    if (oaiKey.trim()) {
      payload.openclaw_token = sanitizeConfigString(oaiKey);
      payload.openclaw_gateway_url = sanitizeConfigString(oaiUrl);
    }
  }

  for (const f of API_KEY_FIELDS) {
    const raw = apiKeys[f.key] ?? "";
    if (raw.trim().length > 0) {
      (payload as Record<string, unknown>)[f.configField] =
        sanitizeConfigString(raw);
    }
  }

  return payload;
}

export function PrePickScreen({ onComplete }: PrePickScreenProps) {
  const [prereqs, setPrereqs] = useState<PrereqResult[]>([]);
  const [prereqsLoaded, setPrereqsLoaded] = useState(false);
  const [submitting, setSubmitting] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string>("");

  // API key state (one entry per API_KEY_FIELDS entry)
  const [apiKeys, setApiKeys] =
    useState<Record<string, string>>(EMPTY_API_KEYS);

  // Local provider single-select
  const [localProvider, setLocalProvider] = useState<string>("");

  // OpenAI-compatible custom endpoint
  const [oaiUrl, setOaiUrl] = useState<string>("");
  const [oaiKey, setOaiKey] = useState<string>("");

  useEffect(() => {
    let cancelled = false;
    get<PrereqsPayload>("/onboarding/prereqs")
      .then((data) => {
        if (cancelled) return;
        setPrereqs(prereqsFromPayload(data));
      })
      .catch(() => {
        // Prereqs unreachable -- render runtime cards in "Not installed" state
        // so the user can proceed via another path (API key / local / skip).
      })
      .finally(() => {
        if (!cancelled) setPrereqsLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const runtimes = useMemo<RuntimeCardState[]>(
    () =>
      DISPATCHABLE_RUNTIMES.map((spec) => {
        const detected = prereqs.find((p) => p.name === spec.binary);
        return {
          spec,
          detected,
          available: Boolean(detected?.found),
        };
      }),
    [prereqs],
  );

  // canContinue is true when any of the five paths is ready:
  // (a) a CLI runtime card was clicked (handled by commitChoice immediately)
  // (b) at least one API key is entered
  // (c) a local provider is selected
  // (d) OAI-compatible URL+key filled and URL valid
  // (e) "I'll add one later" skip (also handled directly)
  const canContinueFromForm =
    anyApiKeyFilled(apiKeys) ||
    localProvider.trim().length > 0 ||
    oaiCompatFilled(oaiUrl);

  function handleApiKeyChange(key: string, value: string): void {
    setApiKeys((prev) => ({ ...prev, [key]: value }));
  }

  async function commitChoice(
    provider: RuntimeSpec["provider"] | string,
    runtimeLabel: string,
  ): Promise<void> {
    setSubmitError("");
    setSubmitting(runtimeLabel);
    try {
      const isCli = isCliProvider(provider);
      // Skip path ("I'll add one later") must NOT persist anything the user
      // typed into the form sections — the contract is "sandbox until you
      // configure later". Trigger: provider === null only.
      const isSkipChoice = provider === null;
      const providerStr = isCli ? (provider as string) : "";
      const configPayload = buildConfigPayload(
        isCli,
        providerStr,
        localProvider,
        oaiUrl,
        oaiKey,
        apiKeys,
      );
      if (
        !isSkipChoice &&
        hasConfigContent(isCli, localProvider, oaiUrl, apiKeys)
      ) {
        await post("/config", configPayload);
      }
      await post("/onboarding/transition", { phase: "greet" });
      onComplete();
    } catch (err: unknown) {
      const msg =
        err instanceof Error ? err.message : "Failed to start the office";
      setSubmitError(msg);
      setSubmitting(null);
    }
  }

  function openInstallPage(url: string): void {
    if (typeof window === "undefined") return;
    window.open(url, "_blank", "noopener,noreferrer");
  }

  const anySubmitting = Boolean(submitting);

  return (
    <div className="pre-pick-screen" data-testid="pre-pick-screen">
      <div className="pre-pick-body">
        <div className="pre-pick-hero">
          <div className="pre-pick-eyebrow">WUPHF</div>
          <h1 className="pre-pick-headline">Pick a runtime.</h1>
          <p className="pre-pick-subhead">
            Your office needs an AI runtime. Pick one of the three below, or use
            an API key, a local model, or a custom endpoint.
          </p>
        </div>

        {/* ── Section 1: CLI runtime cards ─────────────────────────────── */}
        <div className="pre-pick-card-grid">
          {runtimes.map((state) => (
            <RuntimeCard
              key={state.spec.label}
              state={state}
              prereqsLoaded={prereqsLoaded}
              isSubmitting={submitting === state.spec.label}
              anySubmitting={anySubmitting}
              onPick={(spec) => void commitChoice(spec.provider, spec.label)}
              onInstall={openInstallPage}
            />
          ))}
        </div>

        {/* ── Section 2: API keys ──────────────────────────────────────── */}
        <div className="pre-pick-section" data-testid="pre-pick-api-keys">
          <p className="pre-pick-section-heading">API keys</p>
          <p className="pre-pick-section-hint">
            Use CLI login or paste a key. CLI login is the primary path -- keys
            are a fallback or alternative.
          </p>
          <div className="key-group">
            {API_KEY_FIELDS.map((field) => (
              <PrePickApiKeyRow
                key={field.key}
                field={field}
                value={apiKeys[field.key] ?? ""}
                onChange={(v) => handleApiKeyChange(field.key, v)}
              />
            ))}
          </div>
        </div>

        {/* ── Section 3: Local provider picker ────────────────────────── */}
        <div className="pre-pick-section" data-testid="pre-pick-local-section">
          <p className="pre-pick-section-heading">Local model</p>
          <p className="pre-pick-section-hint">
            Run inference on this machine. No cloud key required.
          </p>
          <LocalProviderPicker
            selected={localProvider}
            onSelect={(kind) => setLocalProvider(kind)}
          />
        </div>

        {/* ── Section 4: OpenAI-compatible endpoint ───────────────────── */}
        <div className="pre-pick-section" data-testid="pre-pick-oai-section">
          <p className="pre-pick-section-heading">Custom endpoint</p>
          <p className="pre-pick-section-hint">
            Any server that speaks the OpenAI REST protocol (OpenClaw, LiteLLM,
            vLLM, etc.).
          </p>
          <OpenAICompatibleInput
            endpointUrl={oaiUrl}
            apiKey={oaiKey}
            onChangeUrl={setOaiUrl}
            onChangeKey={setOaiKey}
          />
        </div>

        {/* ── Submit from form sections ────────────────────────────────── */}
        {canContinueFromForm ? (
          <div className="pre-pick-secondary-row">
            <button
              type="button"
              className="btn btn-primary pre-pick-form-submit"
              data-testid="pre-pick-form-submit"
              disabled={anySubmitting}
              onClick={() => void commitChoice("form", "form")}
            >
              {submitting === "form" ? "Opening your office…" : "Continue  →"}
            </button>
          </div>
        ) : null}

        {/* ── Skip / sandbox affordance ────────────────────────────────── */}
        <div className="pre-pick-secondary-row">
          <button
            type="button"
            className="pre-pick-secondary-button"
            data-testid="pre-pick-skip"
            disabled={anySubmitting}
            onClick={() => void commitChoice(null, "skip")}
          >
            {submitting === "skip"
              ? "Opening your office…"
              : "I'll add one later  →"}
          </button>
        </div>

        <p className="pre-pick-helper">
          You can change this any time in{" "}
          <strong>Settings &rarr; Runtimes</strong>.
        </p>

        {submitError ? (
          <div role="alert" className="pre-pick-error">
            {submitError}
          </div>
        ) : null}
      </div>
    </div>
  );
}
