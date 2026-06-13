/**
 * analytics: PostHog-backed product analytics + (optional) session replay.
 *
 * This module is the single analytics surface for the web app. It wraps
 * `posthog-js`, loaded **dynamically** so that a dormant build (no project
 * key) and every fork download zero analytics bytes. See
 * docs/specs/product-analytics.md for the full policy and event taxonomy.
 *
 * Privacy guarantees (enforced here by configuration, not by avoiding the SDK):
 *
 *   1. DORMANT BY DEFAULT. With no resolved project key, nothing is loaded or
 *      sent. A stock source build never phones home and forks never reach our
 *      project.
 *   2. TWO INDEPENDENT CONSENT CHANNELS, both default ON, both revocable:
 *      product analytics (events) and session recording. Either can be off
 *      while the other is on. Sourced from the broker's /api-token analytics
 *      block (operator runtime config) and the in-app toggles.
 *   3. NO COOKIES, NO AUTOCAPTURE. `persistence: "localStorage"`, autocapture
 *      off, pageviews captured manually on route change. No surveys, no
 *      heatmaps, no dead-click capture.
 *   4. STRICT-MASKED RECORDINGS. Every recording masks all text and all inputs
 *      (`maskAllInputs: true`, `maskTextSelector: "*"`). Replays show layout,
 *      cursor, clicks, scroll, and navigation — never readable content,
 *      customer data, agent output, or secrets.
 *   5. NO PII IN EVENTS. Event properties carry shapes and buckets, never
 *      content. The only personal datum that ever leaves is the onboarding
 *      email, attached to the PostHog person exactly once at finish and only
 *      with consent (see useOnboardingWizard).
 *
 * Configuration sources, in precedence order:
 *   - Runtime: the broker /api-token `analytics` block (lets a self-hosted
 *     operator point at their own project and flip toggles without rebuilding).
 *   - Build-time (Vite): VITE_PUBLIC_POSTHOG_KEY / VITE_PUBLIC_POSTHOG_HOST.
 *     UNSET in this repo, so a stock build stays dormant.
 *
 * Every public function is best-effort and never throws into the caller.
 */

import type { PostHog } from "posthog-js";

const DEFAULT_POSTHOG_HOST = "https://us.i.posthog.com";

/** Source tag attached to onboarding funnel events, for filtering in PostHog. */
export const ONBOARDING_SOURCE = "onboarding-welcome";

/** Loose RFC-ish email shape. Good enough to skip obvious junk before sending. */
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** True when the string looks like an email address worth capturing. */
export function isValidEmail(email: string): boolean {
  return EMAIL_RE.test(email.trim());
}

/**
 * The curated product-event names. Properties never contain content — only
 * shapes and buckets. See docs/specs/product-analytics.md for the taxonomy.
 */
export type ProductEvent =
  // Onboarding funnel
  | "onboarding_started"
  | "onboarding_step_completed"
  | "onboarding_blueprint_selected"
  | "onboarding_completed"
  | "onboarding_email_viewed"
  | "onboarding_email_started"
  | "onboarding_email_captured"
  // Core loop
  | "task_created"
  | "task_status_changed"
  | "decision_submitted"
  | "interview_shown"
  | "interview_answered"
  | "message_sent"
  | "action_failed"
  // Knowledge & expansion
  | "wiki_article_viewed"
  | "wiki_article_edited"
  | "notebook_entry_promoted"
  | "review_action"
  | "agent_created"
  | "skill_state_changed"
  | "routine_created"
  | "channel_created"
  | "integration_action"
  // Diagnostics & consent
  | "analytics_consent_set"
  | "app_error";

/** Loose property bag. Values must be non-PII shapes/buckets, never content. */
export type EventProps = Record<string, string | number | boolean | undefined>;

/** The runtime analytics config injected by the broker /api-token endpoint. */
export interface InjectedAnalyticsConfig {
  configured?: boolean;
  posthog_key?: string;
  posthog_host?: string;
  telemetry_enabled?: boolean;
  session_recording_enabled?: boolean;
}

interface ResolvedConfig {
  key: string;
  host: string;
  telemetryEnabled: boolean;
  recordingEnabled: boolean;
}

/** Build-time key/host. Empty key => dormant. */
function buildTimeKeyHost(): { key: string; host: string } {
  const rawKey = import.meta.env.VITE_PUBLIC_POSTHOG_KEY;
  const key = typeof rawKey === "string" ? rawKey.trim() : "";
  const rawHost = import.meta.env.VITE_PUBLIC_POSTHOG_HOST;
  const host = typeof rawHost === "string" ? rawHost.trim() : "";
  return { key, host: (host || DEFAULT_POSTHOG_HOST).replace(/\/+$/, "") };
}

/**
 * The resolved config. `null` until configureAnalytics runs; callers fall back
 * to build-time defaults (with both channels ON, matching the backend default)
 * so the module still works in tests and before boot completes.
 */
let resolved: ResolvedConfig | null = null;

function effectiveConfig(): ResolvedConfig {
  if (resolved) return resolved;
  const { key, host } = buildTimeKeyHost();
  return { key, host, telemetryEnabled: true, recordingEnabled: true };
}

let posthog: PostHog | null = null;
let initPromise: Promise<PostHog | null> | null = null;

/**
 * Test-only: reset module singletons so each test starts dormant. Has no
 * production caller — it exists so the suite can exercise configure/init/consent
 * transitions in isolation without module-registry gymnastics.
 */
export function __resetAnalyticsForTests(): void {
  resolved = null;
  initPromise = null;
  posthog = null;
}

/**
 * Strip anything we never want to send and enrich with the active theme. Runs
 * on every captured event. Defense in depth: properties are constructed to be
 * content-free at the call site, but this guarantees it centrally.
 */
function sanitizeProperties(
  props: Record<string, unknown>,
): Record<string, unknown> {
  const out = { ...props };
  if (typeof document !== "undefined") {
    const theme = document.documentElement.getAttribute("data-theme");
    if (theme) out.theme = theme;
  }
  const version = import.meta.env.VITE_PUBLIC_APP_VERSION;
  if (typeof version === "string" && version) out.app_version = version;
  return out;
}

/**
 * Lazily import and initialize posthog-js. Resolves to null when dormant (no
 * key) or when the telemetry channel is off, so every caller is a safe no-op
 * in those states. The same promise is shared, so events fired before init
 * completes are captured in order once the SDK is ready.
 */
function ensurePostHog(): Promise<PostHog | null> {
  const cfg = effectiveConfig();
  if (!(cfg.key && cfg.telemetryEnabled)) return Promise.resolve(null);
  if (initPromise) return initPromise;
  initPromise = import("posthog-js")
    .then(({ default: ph }) => {
      ph.init(cfg.key, {
        api_host: cfg.host,
        autocapture: false,
        capture_pageview: false,
        capture_pageleave: true,
        capture_heatmaps: false,
        disable_surveys: true,
        persistence: "localStorage",
        person_profiles: "identified_only",
        // Recording is opt-in: start it explicitly only when the recording
        // channel is on (below + setAnalyticsConsent), never automatically.
        disable_session_recording: true,
        session_recording: {
          maskAllInputs: true,
          maskTextSelector: "*",
          blockSelector: undefined,
          recordCrossOriginIframes: false,
        },
        sanitize_properties: (properties) => sanitizeProperties(properties),
        loaded: (ph2) => {
          if (effectiveConfig().recordingEnabled) ph2.startSessionRecording();
        },
      });
      posthog = ph;
      return ph;
    })
    .catch(() => null);
  return initPromise;
}

/**
 * Configure analytics from the broker's injected runtime config and kick off
 * lazy init when live. Runtime values win over build-time; the toggles win
 * over the resolved key (a configured key with telemetry off stays dormant).
 */
export function configureAnalytics(injected?: InjectedAnalyticsConfig): void {
  const build = buildTimeKeyHost();
  const runtimeKey =
    typeof injected?.posthog_key === "string"
      ? injected.posthog_key.trim()
      : "";
  const runtimeHost =
    typeof injected?.posthog_host === "string"
      ? injected.posthog_host.trim()
      : "";
  resolved = {
    key: runtimeKey || build.key,
    host: (runtimeHost || build.host || DEFAULT_POSTHOG_HOST).replace(
      /\/+$/,
      "",
    ),
    telemetryEnabled: injected?.telemetry_enabled !== false,
    recordingEnabled: injected?.session_recording_enabled !== false,
  };
  if (resolved.key && resolved.telemetryEnabled) void ensurePostHog();
}

/**
 * Apply a live consent change (from the Settings toggles) without a reload.
 * Telemetry off stops capture; recording off stops the recorder.
 */
export function setAnalyticsConsent(consent: {
  telemetry?: boolean;
  recording?: boolean;
}): void {
  const cfg = effectiveConfig();
  resolved = {
    ...cfg,
    telemetryEnabled: consent.telemetry ?? cfg.telemetryEnabled,
    recordingEnabled: consent.recording ?? cfg.recordingEnabled,
  };
  if (!resolved.telemetryEnabled) {
    posthog?.opt_out_capturing();
    posthog?.stopSessionRecording();
    return;
  }
  // Telemetry on (or back on): make sure the SDK is live, then sync recording.
  void ensurePostHog().then((ph) => {
    if (!ph) return;
    ph.opt_in_capturing();
    if (resolved?.recordingEnabled) ph.startSessionRecording();
    else ph.stopSessionRecording();
  });
}

/** Capture a product event. No-op when dormant or telemetry is off. */
export function track(event: ProductEvent, properties?: EventProps): void {
  void ensurePostHog().then((ph) => {
    if (!ph) return;
    ph.capture(event, properties ? { ...properties } : undefined);
  });
}

/**
 * Fire a product event after a promise resolves, preserving its value.
 * Success-gated (only on resolve) and best-effort. Used to instrument API
 * calls at the client layer without changing their control flow. Rejections
 * pass through untouched (callers and the global MutationCache handle them).
 */
export function trackOn<T>(
  p: Promise<T>,
  event: ProductEvent,
  properties?: EventProps,
): Promise<T> {
  return p.then((r) => {
    track(event, properties);
    return r;
  });
}

/**
 * Capture a manual SPA pageview. `route` is the matched route pattern (e.g.
 * "/tasks/$taskId"), `pathname` the concrete path with no query string. We
 * deliberately omit the query string to avoid leaking parameters.
 */
export function trackPageview(route: string, pathname: string): void {
  void ensurePostHog().then((ph) => {
    if (!ph) return;
    const url =
      typeof window !== "undefined"
        ? window.location.origin + pathname
        : pathname;
    ph.capture("$pageview", { $current_url: url, route });
  });
}

/**
 * Associate the session with a workspace group, keyed by a hashed workspace id
 * (never the raw id or name). distinct_id stays anonymous. Best-effort.
 */
export function identifyWorkspace(
  workspaceId: string,
  groupProps?: EventProps,
): void {
  const id = workspaceId.trim();
  if (!id) return;
  void hashId(id).then((hashed) => {
    void ensurePostHog().then((ph) => {
      ph?.group(
        "workspace",
        hashed,
        groupProps ? { ...groupProps } : undefined,
      );
    });
  });
}

/** SHA-256 the id and keep the first 16 hex chars; opaque, non-reversible. */
async function hashId(value: string): Promise<string> {
  if (typeof crypto === "undefined" || !crypto.subtle) return "ws_unknown";
  const data = new TextEncoder().encode(value);
  const digest = await crypto.subtle.digest("SHA-256", data);
  const hex = Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
  return `ws_${hex.slice(0, 16)}`;
}

// ── Onboarding funnel (preserved API; now routed through the SDK) ────────────

/**
 * Record that the email step was seen. Anonymous: source tag only, never an
 * address. Safe to call on step mount.
 */
export function recordOnboardingEmailViewed(): void {
  track("onboarding_email_viewed", { source: ONBOARDING_SOURCE });
}

/**
 * Record that the user began typing into the email field. Anonymous: never the
 * (partial) address. Call once, on the first keystroke.
 */
export function recordOnboardingEmailStarted(): void {
  track("onboarding_email_started", { source: ONBOARDING_SOURCE });
}

/**
 * Record onboarding completion and attach the email to the PostHog person.
 * This is the one call that carries PII, so callers MUST gate it behind the
 * keep-in-touch consent and isValidEmail. Best-effort and fire-and-forget.
 */
export function recordOnboardingEmailCaptured(email: string): void {
  const trimmed = email.trim();
  if (!trimmed) return;
  void ensurePostHog().then((ph) => {
    if (!ph) return;
    ph.setPersonProperties({ email: trimmed });
    ph.capture("onboarding_email_captured", { source: ONBOARDING_SOURCE });
  });
}
