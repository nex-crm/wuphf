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
//
// `phaseAlreadyComplete` flag (issue #979): if the broker reports phase=complete
// at click time the user is in a "session-loss" recovery state — they have
// finished onboarding before but the SPA's boot-time /onboarding/state fetch
// failed and dumped them back here. Bypass POST /onboarding/transition entirely
// (the broker would reject "complete → greet" with a 400 invalid transition)
// and signal the caller to route straight to the office.

interface PrePickScreenProps {
  onComplete: (info?: { phaseAlreadyComplete?: boolean }) => void;
}

/**
 * Inline right-arrow icon. Replaces the previous "→" Unicode glyph
 * used in CTA copy ("Install →", "Continue →", "I'll add one later
 * →") so the arrow renders as a proper icon at a consistent visual
 * weight rather than as a font-rendered character whose appearance
 * varies wildly by platform / font fallback. `currentColor` so the
 * icon picks up the surrounding text color in every context.
 */
function ArrowRightIcon({
  size = 14,
  className,
}: {
  size?: number;
  className?: string;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      <line x1="5" y1="12" x2="19" y2="12" />
      <polyline points="12 5 19 12 12 19" />
    </svg>
  );
}

interface RuntimeCardState {
  spec: RuntimeSpec;
  detected: PrereqResult | undefined;
  available: boolean;
  /**
   * Issue #932: when the broker session-probed this runtime and it
   * reported no active session, `signedIn` is explicitly false. When
   * the runtime supports no probe, `signedIn` is undefined — fall back
   * to legacy "Detected" behavior.
   */
  signedIn: boolean | undefined;
  signInCommand: string | undefined;
}

interface RuntimeCardProps {
  state: RuntimeCardState;
  prereqsLoaded: boolean;
  isSubmitting: boolean;
  anySubmitting: boolean;
  onPick: (spec: RuntimeSpec) => void;
  onInstall: (url: string) => void;
  onCopySignIn: (command: string) => void;
}

/**
 * Card status is rendered as TWO lines: the primary state (e.g.
 * "Signed in") and a secondary detail line (the runtime version /
 * provider string, e.g. "2.1.150 (Claude Code)"). Splitting them on
 * separate lines lets the eye scan the state at a glance without the
 * version cruft competing for attention.
 */
function cardStatusLabel({
  prereqsLoaded,
  available,
  version,
  signedIn,
}: {
  prereqsLoaded: boolean;
  available: boolean;
  version: string | undefined;
  signedIn: boolean | undefined;
}): { primary: string; detail: string | undefined } {
  if (!prereqsLoaded) return { primary: "Checking…", detail: undefined };
  if (!available) return { primary: "Not installed", detail: undefined };
  // Issue #932: when the runtime supports a session probe and we got a
  // definite "not signed in", surface that as the primary status. The user
  // would otherwise complete onboarding believing they were connected,
  // then hit "Not logged in" on the agent loop's first call.
  if (signedIn === false) {
    return { primary: "Not signed in", detail: version };
  }
  if (signedIn === true) {
    return { primary: "Signed in", detail: version };
  }
  return { primary: "Detected", detail: version };
}

function RuntimeCard({
  state,
  prereqsLoaded,
  isSubmitting,
  anySubmitting,
  onPick,
  onInstall,
  onCopySignIn,
}: RuntimeCardProps) {
  const { spec, detected, available, signedIn, signInCommand } = state;
  const statusLabel = isSubmitting
    ? { primary: "Starting your office…", detail: undefined }
    : cardStatusLabel({
        prereqsLoaded,
        available,
        version: detected?.version,
        signedIn,
      });
  // Issue #932: if the runtime is installed but the broker probed and found
  // no active session, block picking and surface a "Sign in" CTA. Clicking
  // copies the suggested command (e.g. `claude auth login`) to the
  // clipboard so the user can paste it into a terminal. We do not launch
  // a terminal — that's platform-specific and brittle.
  const isUnauthed = available && signedIn === false;
  const installHint =
    !available && prereqsLoaded ? (
      <div className="pre-pick-card-install">
        Install
        <ArrowRightIcon className="pre-pick-inline-arrow" />
      </div>
    ) : null;
  const signInHint =
    isUnauthed && prereqsLoaded ? (
      <div className="pre-pick-card-install">Copy sign-in command</div>
    ) : null;
  return (
    <button
      key={spec.label}
      type="button"
      className={`pre-pick-card${available ? " available" : " missing"}${isUnauthed ? " unauthed" : ""}`}
      data-testid={`pre-pick-card-${spec.provider}`}
      data-signed-in={signedIn === undefined ? "unknown" : String(signedIn)}
      // CodeRabbit fix (PR #889): guard against clicks during prereq detection
      // and any in-flight submission. Both disable conditions must hold.
      // Issue #932: don't fully disable when unauthed — we still want clicks
      // to fire the sign-in CTA. The pick path early-returns instead.
      disabled={anySubmitting || !prereqsLoaded}
      onClick={() => {
        // Belt-and-suspenders guard: early-return if prereqs haven't settled.
        if (!prereqsLoaded) return;
        if (!available) {
          onInstall(spec.installUrl);
          return;
        }
        // Auth gate: when the runtime probed and reported NOT signed in,
        // never fall through to onPick — even if sign_in_command is
        // missing/empty. Falling through would advance onboarding to an
        // un-authed runtime, which the agent loop's first LLM call would
        // immediately reject. Copy the command when we have it; otherwise
        // just no-op (the inline "not signed in" hint stays visible).
        if (isUnauthed) {
          if (signInCommand) {
            onCopySignIn(signInCommand);
          }
          return;
        }
        onPick(spec);
      }}
    >
      <div className="pre-pick-card-head">
        <RuntimeLogo label={spec.label} />
        <span className="pre-pick-card-name">{spec.label}</span>
      </div>
      <div className="pre-pick-card-status">
        <span className="pre-pick-card-status-primary">
          {statusLabel.primary}
        </span>
        {statusLabel.detail ? (
          <span className="pre-pick-card-status-detail">
            {statusLabel.detail}
          </span>
        ) : null}
      </div>
      {installHint}
      {signInHint}
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

/**
 * CollapsibleSection — accordion row used inside the pre-pick screen's
 * alternative-auth group (API keys, local model, custom endpoint).
 *
 * Controlled via `open`/`onToggle`. The whole header row (heading +
 * chevron) is a single button so clicking anywhere in it toggles. The
 * content area uses the `grid-template-rows: 0fr ⇆ 1fr` trick so the
 * height animates smoothly between auto and zero — supported in all
 * modern evergreen browsers without `interpolate-size`.
 */
interface CollapsibleSectionProps {
  testId: string;
  heading: string;
  open: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}
function CollapsibleSection({
  testId,
  heading,
  open,
  onToggle,
  children,
}: CollapsibleSectionProps) {
  const panelId = `${testId}-panel`;
  return (
    <div
      className="pre-pick-section pre-pick-section--collapsible"
      data-testid={testId}
      data-open={open ? "true" : "false"}
    >
      <button
        type="button"
        className="pre-pick-section-summary"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={onToggle}
      >
        <span className="pre-pick-section-heading">{heading}</span>
        <svg
          className="pre-pick-section-chevron"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>
      <div
        id={panelId}
        className="pre-pick-section-panel"
        role="region"
        aria-labelledby={panelId}
        aria-hidden={!open}
      >
        <div className="pre-pick-section-panel-inner">{children}</div>
      </div>
    </div>
  );
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

  // Collapsible sections — every alternative path (API keys, local model,
  // custom endpoint) is collapsed by default so the CLI cards above stay
  // the dominant choice. Single-open accordion: opening one section
  // closes the others, so only one alternative is expanded at a time.
  // Tracked as a single `openSection` slot (or `null` for all-closed)
  // and derived into the per-section booleans the JSX still expects.
  type AltSection = "apiKeys" | "local" | "custom";
  const [openSection, setOpenSection] = useState<AltSection | null>(null);
  const openSections = {
    apiKeys: openSection === "apiKeys",
    local: openSection === "local",
    custom: openSection === "custom",
  };
  const toggleSection = (key: AltSection) =>
    setOpenSection((prev) => (prev === key ? null : key));

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
        // Issue #932: signedIn is undefined when the broker didn't probe
        // this runtime (no session-status subcommand wired). Cards
        // distinguish that "unknown" case from a definite "not signed in".
        const signedIn = detected?.session_probed
          ? Boolean(detected?.signed_in)
          : undefined;
        return {
          spec,
          detected,
          available: Boolean(detected?.found),
          signedIn,
          signInCommand: detected?.sign_in_command,
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

  // Issue #979 guard: best-effort probe of /onboarding/state at click time.
  // If the broker reports phase=complete, the SPA is in a session-loss
  // recovery state — POSTing /onboarding/transition with phase=greet would
  // be rejected as "invalid transition" and leave the user stuck on the
  // picker. Endpoint errors fall through to the normal pick path so a
  // fresh install still works without runtime-dependent probe success.
  async function probePhaseAlreadyComplete(): Promise<boolean> {
    try {
      const state = await get<{ onboarded?: boolean; phase?: string }>(
        "/onboarding/state",
      );
      return state?.onboarded === true || state?.phase === "complete";
    } catch {
      return false;
    }
  }

  async function commitChoice(
    provider: RuntimeSpec["provider"] | string,
    runtimeLabel: string,
  ): Promise<void> {
    setSubmitError("");
    setSubmitting(runtimeLabel);
    try {
      if (await probePhaseAlreadyComplete()) {
        // Recovery path: skip /config and /onboarding/transition entirely
        // and signal the caller to route into the office.
        onComplete({ phaseAlreadyComplete: true });
        return;
      }

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

  // Issue #932: copy the suggested sign-in command (e.g. `claude auth login`)
  // to the clipboard when the user clicks an un-signed-in runtime tile.
  // navigator.clipboard.writeText resolves only in secure contexts; on the
  // null fallback we still surface the command via setSubmitError so the
  // user can copy it manually.
  function handleCopySignIn(command: string): void {
    setSubmitError("");
    const message = `Run \`${command}\` in a terminal, then click the tile again.`;
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(command).catch(() => {
        // Clipboard denied (e.g. iframe without permission) — fall through
        // to the inline message below.
      });
    }
    setSubmitError(message);
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
              onCopySignIn={handleCopySignIn}
            />
          ))}
        </div>

        {/* ── Alternative auth paths, grouped in one bordered card ────── */}
        <div
          className="pre-pick-collapsible-group"
          data-testid="pre-pick-collapsible-group"
        >
        {/* ── Section 2: API keys ──────────────────────────────────────── */}
        <CollapsibleSection
          testId="pre-pick-api-keys"
          heading="API keys"
          open={openSections.apiKeys}
          onToggle={() => toggleSection("apiKeys")}
        >
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
        </CollapsibleSection>

        {/* ── Section 3: Local provider picker ────────────────────────── */}
        <CollapsibleSection
          testId="pre-pick-local-section"
          heading="Local model"
          open={openSections.local}
          onToggle={() => toggleSection("local")}
        >
          <p className="pre-pick-section-hint">
            Run inference on this machine. No cloud key required.
          </p>
          <LocalProviderPicker
            selected={localProvider}
            onSelect={(kind) => setLocalProvider(kind)}
          />
        </CollapsibleSection>

        {/* ── Section 4: OpenAI-compatible endpoint ───────────────────── */}
        <CollapsibleSection
          testId="pre-pick-oai-section"
          heading="Custom endpoint"
          open={openSections.custom}
          onToggle={() => toggleSection("custom")}
        >
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
        </CollapsibleSection>
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
              {submitting === "form" ? (
                "Opening your office…"
              ) : (
                <>
                  Continue
                  <ArrowRightIcon className="pre-pick-inline-arrow" />
                </>
              )}
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
            {submitting === "skip" ? (
              "Opening your office…"
            ) : (
              <>
                I'll add one later
                <ArrowRightIcon className="pre-pick-inline-arrow" />
              </>
            )}
          </button>
        </div>

        <p className="pre-pick-helper">
          You can change this any time in{" "}
          <strong>
            Settings
            <ArrowRightIcon className="pre-pick-inline-arrow" size={12} />
            Runtimes
          </strong>
          .
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
