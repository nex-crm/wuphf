import { useCallback, useEffect, useMemo, useState } from "react";

import type { LLMRuntimeKind } from "../../api/client";
import { get, post } from "../../api/client";
import { runtimeProviderLabel } from "../../lib/runtimeProviders";
import { showNotice } from "../ui/Toast";
import { LocalProviderPicker } from "./LocalProviderPicker";
import { isValidUrl, OpenAICompatibleInput } from "./OpenAICompatibleInput";
import { PrePickApiKeyRow } from "./PrePickApiKeyRow";
import { RuntimeGuidePanel } from "./RuntimeGuidePanel";
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
  expanded: boolean;
  selected: boolean;
  onToggle: (spec: RuntimeSpec) => void;
  onInstall: (url: string) => void;
  onCopySignIn: (command: string) => void;
  onToggleGuide: (binary: string) => void;
}

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
}): string {
  if (!prereqsLoaded) return "Checking…";
  if (!available) return "Not installed";
  // Issue #932: when the runtime supports a session probe and we got a
  // definite "not signed in", surface that as the primary status. The user
  // would otherwise complete onboarding believing they were connected,
  // then hit "Not logged in" on the agent loop's first call.
  if (signedIn === false) {
    return version ? `Not signed in · ${version}` : "Not signed in";
  }
  if (signedIn === true) {
    return version ? `Signed in · ${version}` : "Signed in";
  }
  return version ? `Detected · ${version}` : "Detected";
}

function RuntimeCard({
  state,
  prereqsLoaded,
  isSubmitting,
  anySubmitting,
  expanded,
  selected,
  onToggle,
  onInstall,
  onCopySignIn,
  onToggleGuide,
}: RuntimeCardProps) {
  const { spec, detected, available, signedIn, signInCommand } = state;
  const statusLabel = isSubmitting
    ? "Starting your office…"
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
      <div className="pre-pick-card-install">Install &rarr;</div>
    ) : null;
  const signInHint =
    isUnauthed && prereqsLoaded ? (
      <div className="pre-pick-card-install">Copy sign-in command</div>
    ) : null;
  // The guide toggle stays usable while a card is in a blocking state
  // (missing / not signed in) — that is exactly when the user needs the
  // step-by-step setup. It is only disabled during an in-flight submit so a
  // second office cannot start underneath the first.
  return (
    <div
      className="pre-pick-cell"
      data-testid={`pre-pick-cell-${spec.provider}`}
    >
      <button
        type="button"
        className={`pre-pick-card${available ? " available" : " missing"}${isUnauthed ? " unauthed" : ""}${selected ? " selected" : ""}`}
        data-testid={`pre-pick-card-${spec.provider}`}
        aria-pressed={selected}
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
          onToggle(spec);
        }}
      >
        <div className="pre-pick-card-head">
          <RuntimeLogo label={spec.label} />
          <span className="pre-pick-card-name">{spec.label}</span>
        </div>
        <div className="pre-pick-card-status">{statusLabel}</div>
        {installHint}
        {signInHint}
      </button>
      <button
        type="button"
        className={`pre-pick-guide-toggle${expanded ? " open" : ""}`}
        data-testid={`pre-pick-guide-toggle-${spec.provider}`}
        aria-expanded={expanded}
        aria-controls={`pre-pick-guide-${spec.binary}`}
        disabled={isSubmitting}
        onClick={() => onToggleGuide(spec.binary)}
      >
        <span className="pre-pick-guide-toggle-chevron" aria-hidden="true">
          &rsaquo;
        </span>
        {expanded ? "Hide setup" : "Set up & verify"}
      </button>
    </div>
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
  localProviders: readonly LLMRuntimeKind[],
  oaiUrl: string,
  apiKeys: Record<string, string>,
): boolean {
  return (
    isCliRuntime ||
    localProviders.length > 0 ||
    oaiCompatFilled(oaiUrl) ||
    anyApiKeyFilled(apiKeys)
  );
}

type ConfigPayload = {
  memory_backend: "markdown";
  llm_provider?: string;
  llm_provider_priority?: LLMRuntimeKind[];
  anthropic_api_key?: string;
  openai_api_key?: string;
  gemini_api_key?: string;
  provider_endpoints?: Record<string, { base_url: string }>;
};

/**
 * Builds the /config POST payload. Extracted from commitChoice to keep the
 * function's cognitive complexity within the 15-branch biome limit.
 * All user-supplied strings are sanitized via sanitizeConfigString.
 */
function buildConfigPayload(
  selectedProviders: LLMRuntimeKind[],
  localProviders: LLMRuntimeKind[],
  oaiUrl: string,
  // _oaiKey is intentionally unused on the current write path: the OAI-
  // compatible endpoint section writes only to provider_endpoints, never
  // to openclaw_* (gateway config) or to an Anthropic/OpenAI key row.
  // Kept in the signature so the call site can stay 1:1 with form
  // state — renaming the parameter to _oaiKey marks the intent for
  // TypeScript-aware linters without forcing a 4-argument call shape.
  _oaiKey: string,
  apiKeys: Record<string, string>,
): ConfigPayload {
  const payload: ConfigPayload = { memory_backend: "markdown" };
  const providerPriority = Array.from(
    new Set(
      [...selectedProviders, ...localProviders].filter(
        Boolean,
      ) as LLMRuntimeKind[],
    ),
  );

  if (providerPriority.length > 0) {
    payload.llm_provider = providerPriority[0];
    payload.llm_provider_priority = providerPriority;
  } else if (oaiCompatFilled(oaiUrl)) {
    // Custom OAI-compatible endpoint goes into provider_endpoints. We do
    // NOT write to openclaw_* here — that conflated OpenClaw (a gateway
    // for importing existing agents) with generic OpenAI-compatible HTTP
    // runtimes. OpenClaw is configured through the Integrations app, not
    // through this onboarding step. The _oaiKey parameter is intentionally
    // unused on this path; users wanting to pair an API key with the
    // endpoint should paste it in the OpenAI API key row above.
    payload.provider_endpoints = {
      "openai-compatible": { base_url: sanitizeConfigString(oaiUrl) },
    };
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

// Static illustration of the "Set up & verify" flow. The GIF autoplays and
// cannot honor prefers-reduced-motion, so a <picture> serves a static poster
// to reduced-motion users. Extracted to a module-level component so the
// PrePickScreen render body stays within the per-function line budget.
function VerifyClipFigure() {
  return (
    <figure className="pre-pick-clip-figure">
      <picture>
        <source
          srcSet="/media/onboarding/provider-verify-still.png"
          media="(prefers-reduced-motion: reduce)"
        />
        <img
          className="pre-pick-clip"
          src="/media/onboarding/provider-verify.gif"
          width={808}
          height={600}
          alt="A runtime being verified: Claude Code is checked for an installed binary, an active sign-in, and a reachable model, then resolves to Connected."
          loading="lazy"
          decoding="async"
        />
      </picture>
      <figcaption className="pre-pick-clip-caption">
        "Set up &amp; verify" checks the binary, your sign-in, and that the
        model is reachable before you continue.
      </figcaption>
    </figure>
  );
}

export function PrePickScreen({ onComplete }: PrePickScreenProps) {
  const [prereqs, setPrereqs] = useState<PrereqResult[]>([]);
  const [prereqsLoaded, setPrereqsLoaded] = useState(false);
  const [submitting, setSubmitting] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string>("");
  const [selectedProviders, setSelectedProviders] = useState<LLMRuntimeKind[]>(
    [],
  );

  // API key state (one entry per API_KEY_FIELDS entry)
  const [apiKeys, setApiKeys] =
    useState<Record<string, string>>(EMPTY_API_KEYS);

  const [localProviders, setLocalProviders] = useState<LLMRuntimeKind[]>([]);

  // OpenAI-compatible custom endpoint
  const [oaiUrl, setOaiUrl] = useState<string>("");
  const [oaiKey, setOaiKey] = useState<string>("");

  // Guided-setup + verify state (spec section B). At most one runtime's guide
  // is expanded at a time. `verifiedReady` records the runtimes a live verify
  // pass classified as ready, keyed by the prereq binary — once one is ready
  // the primary advance button reads "Next" instead of "Skip for now".
  const [expandedGuide, setExpandedGuide] = useState<string | null>(null);
  const [verifiedReady, setVerifiedReady] = useState<Record<string, boolean>>(
    {},
  );

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

  // canContinue is true when any setup path is ready:
  // (a) at least one detected CLI runtime card is selected
  // (b) at least one API key is entered
  // (c) a local provider is selected
  // (d) OAI-compatible URL is valid
  const canContinueFromForm =
    selectedProviders.length > 0 ||
    anyApiKeyFilled(apiKeys) ||
    localProviders.length > 0 ||
    oaiCompatFilled(oaiUrl);

  function handleApiKeyChange(key: string, value: string): void {
    setApiKeys((prev) => ({ ...prev, [key]: value }));
  }

  // Toggle the guided-setup panel for a runtime (keyed by prereq binary).
  // Opening one collapses any other so the screen never stacks two guides.
  function handleToggleGuide(binary: string): void {
    setExpandedGuide((prev) => (prev === binary ? null : binary));
  }

  // A live verify pass classified this runtime as ready. Record it so the
  // primary advance button can read "Next". Keyed by prereq binary.
  // useCallback so RuntimeGuidePanel's effect (which lists onVerified as a
  // dependency) does not re-run on every parent render.
  const handleVerified = useCallback((runtimeBinary: string) => {
    setVerifiedReady((prev) =>
      prev[runtimeBinary] ? prev : { ...prev, [runtimeBinary]: true },
    );
  }, []);

  // The runtime whose guide is currently expanded, if any.
  const expandedRuntime = expandedGuide
    ? runtimes.find((r) => r.spec.binary === expandedGuide)
    : undefined;

  // True once any runtime has verified ready — drives the primary CTA copy.
  const anyRuntimeReady = Object.values(verifiedReady).some(Boolean);

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
      const selected =
        isCli && typeof provider === "string"
          ? Array.from(
              new Set([...selectedProviders, provider as LLMRuntimeKind]),
            )
          : selectedProviders;
      const configPayload = buildConfigPayload(
        selected,
        localProviders,
        oaiUrl,
        oaiKey,
        apiKeys,
      );
      if (
        !isSkipChoice &&
        (selected.length > 0 ||
          hasConfigContent(isCli, localProviders, oaiUrl, apiKeys))
      ) {
        await post("/config", configPayload);
      }
      // Hand off to the visual onboarding wizard. We deliberately do NOT POST
      // /onboarding/transition {phase:"greet"} here: that starts the legacy
      // CEO-chat phase machine (and a greet agent turn), which the wizard
      // replaces. The wizard collects answers across its steps and seeds the
      // office in one shot via POST /onboarding/complete at finish, so no
      // phase-machine transition is needed (or wanted — the greet turn could
      // block this pick on a runtime that is still being set up).
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
      navigator.clipboard
        .writeText(command)
        .then(() => {
          showNotice(`Copied: ${command}`, "success");
        })
        .catch(() => {
          // Clipboard denied (e.g. iframe without permission) — fall through
          // to the inline message + toast below so the user can copy manually.
          showNotice(message, "info");
        });
    } else {
      showNotice(message, "info");
    }
    setSubmitError(message);
  }

  const anySubmitting = Boolean(submitting);
  function toggleProvider(provider: LLMRuntimeKind): void {
    setSelectedProviders((prev) =>
      prev.includes(provider)
        ? prev.filter((id) => id !== provider)
        : [...prev, provider],
    );
  }

  function toggleLocalProvider(provider: string): void {
    setLocalProviders((prev) =>
      prev.includes(provider as LLMRuntimeKind)
        ? prev.filter((id) => id !== provider)
        : [...prev, provider as LLMRuntimeKind],
    );
  }

  const selectedProviderLabels = [...selectedProviders, ...localProviders].map(
    runtimeProviderLabel,
  );

  return (
    <div className="pre-pick-screen" data-testid="pre-pick-screen">
      <div className="pre-pick-body">
        <div className="pre-pick-hero">
          <div className="pre-pick-eyebrow">WUPHF</div>
          <h1 className="pre-pick-headline">Pick a default runtime.</h1>
          <p className="pre-pick-subhead">
            This is the runtime new agents will inherit when they are created.
            You can change it later in Settings, and most agents can be moved to
            a different runtime one at a time from their profile. Agents
            imported through a gateway (OpenClaw, Hermes) are managed from the
            Integrations app instead.
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
              expanded={expandedGuide === state.spec.binary}
              selected={
                state.spec.provider !== null &&
                selectedProviders.includes(state.spec.provider)
              }
              onToggle={(spec) => {
                if (spec.provider) toggleProvider(spec.provider);
              }}
              onInstall={openInstallPage}
              onCopySignIn={handleCopySignIn}
              onToggleGuide={handleToggleGuide}
            />
          ))}
        </div>

        {/* ── Guided setup + verify panel for the expanded runtime ──────── */}
        {expandedRuntime ? (
          <RuntimeGuidePanel
            key={expandedRuntime.spec.binary}
            runtime={expandedRuntime.spec.binary}
            label={expandedRuntime.spec.label}
            onVerified={handleVerified}
          />
        ) : null}

        {/* ── Advance when a runtime has verified ready ─────────────────── */}
        {anyRuntimeReady && expandedRuntime ? (
          <div className="pre-pick-secondary-row">
            <button
              type="button"
              className="btn btn-primary pre-pick-next-button"
              data-testid="pre-pick-next"
              disabled={anySubmitting}
              onClick={() =>
                void commitChoice(
                  expandedRuntime.spec.provider,
                  expandedRuntime.spec.label,
                )
              }
            >
              {submitting === expandedRuntime.spec.label
                ? "Opening your office…"
                : "Next  →"}
            </button>
          </div>
        ) : null}

        {/* What "Set up & verify" looks like. Sits BELOW the runtime cards so
            the providers stay first; the clip illustrates the verify flow. */}
        <VerifyClipFigure />

        {/* ── Section 2: API keys ──────────────────────────────────────── */}
        <section
          className="pre-pick-section"
          data-testid="pre-pick-api-keys"
          aria-labelledby="pre-pick-api-keys-h"
        >
          <h2 className="pre-pick-section-heading" id="pre-pick-api-keys-h">
            API keys
          </h2>
          <p className="pre-pick-section-hint">
            Use CLI login or paste a key. CLI login is the primary path; keys
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
        </section>

        {/* ── Section 3: Local provider picker ────────────────────────── */}
        <section
          className="pre-pick-section"
          data-testid="pre-pick-local-section"
          aria-labelledby="pre-pick-local-h"
        >
          <h2 className="pre-pick-section-heading" id="pre-pick-local-h">
            Local model
          </h2>
          <p className="pre-pick-section-hint">
            Run inference on this machine. No cloud key required.
          </p>
          <LocalProviderPicker
            selected={localProviders}
            onToggle={toggleLocalProvider}
          />
        </section>

        {/* ── Section 4: OpenAI-compatible endpoint ───────────────────── */}
        <section
          className="pre-pick-section"
          data-testid="pre-pick-oai-section"
          aria-labelledby="pre-pick-oai-h"
        >
          <h2 className="pre-pick-section-heading" id="pre-pick-oai-h">
            Custom endpoint
          </h2>
          <p className="pre-pick-section-hint">
            Any server that speaks the OpenAI REST protocol (LiteLLM, vLLM,
            llama.cpp server, etc.). For OpenClaw or Hermes, use the
            Integrations app after onboarding.
          </p>
          <OpenAICompatibleInput
            endpointUrl={oaiUrl}
            apiKey={oaiKey}
            onChangeUrl={setOaiUrl}
            onChangeKey={setOaiKey}
          />
        </section>

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
            {selectedProviderLabels.length > 0 ? (
              <div className="pre-pick-selected-summary">
                Selected: {selectedProviderLabels.join(", ")}
              </div>
            ) : null}
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
              : "I will add one later  →"}
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
