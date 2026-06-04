/**
 * analytics: PostHog-backed onboarding capture, no SDK.
 *
 * A thin capture client that posts a handful of explicit events straight to
 * PostHog's ingestion endpoint with `fetch`. We deliberately do NOT pull in
 * posthog-js: no autocapture, no pageview tracking, no PostHog cookies, no
 * fingerprinting. Just three named events keyed to a random per-session id.
 * That keeps the bundle flat and the privacy story honest for a local,
 * self-hosted OSS app.
 *
 * Configuration (both build-time, Vite):
 *   - VITE_PUBLIC_POSTHOG_KEY   the PROJECT api key (phc_…). This is write-only
 *                               and safe to ship in client code by design; it
 *                               cannot read data back. UNSET in this repo, so a
 *                               stock build (and every fork) stays dormant.
 *   - VITE_PUBLIC_POSTHOG_HOST  ingestion host, defaults to us.i.posthog.com.
 *                               Set to eu.i.posthog.com or a reverse proxy as
 *                               needed.
 *
 * Two rules keep this honest:
 *
 *   1. DORMANT BY DEFAULT. With no project key, every function here is a no-op,
 *      so a default OSS build never phones home and forks never send their data
 *      to our project.
 *
 *   2. CONSENT GATES THE EMAIL. The two funnel events (`recordOnboardingEmail-
 *      Viewed` / `…Started`) carry only an anonymous per-session id and a
 *      source tag, never an address. The email is attached to the PostHog
 *      person (via `$set`) exactly once, at finish, and only when the caller
 *      passes consent (see useOnboardingWizard). It is never logged.
 *
 * All calls are best-effort and fire-and-forget: failures are swallowed and
 * never block onboarding.
 */

const VISIT_ID_KEY = "wuphf-analytics-visit-id";
const DEFAULT_POSTHOG_HOST = "https://us.i.posthog.com";

/** Source tag attached to every onboarding event, for filtering in PostHog. */
export const ONBOARDING_SOURCE = "onboarding-welcome";

/** Loose RFC-ish email shape. Good enough to skip obvious junk before sending. */
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** True when the string looks like an email address worth capturing. */
export function isValidEmail(email: string): boolean {
  return EMAIL_RE.test(email.trim());
}

interface PostHogConfig {
  key: string;
  host: string;
}

/**
 * The resolved PostHog config, or null when no project key is set (dormant).
 * Read on every call (not memoized at module load) so the dormant/active
 * decision is always live and so tests can flip it with `vi.stubEnv`.
 */
function getConfig(): PostHogConfig | null {
  const rawKey = import.meta.env.VITE_PUBLIC_POSTHOG_KEY;
  const key = typeof rawKey === "string" ? rawKey.trim() : "";
  if (!key) return null;

  const rawHost = import.meta.env.VITE_PUBLIC_POSTHOG_HOST;
  const host = typeof rawHost === "string" ? rawHost.trim() : "";
  return {
    key,
    host: (host || DEFAULT_POSTHOG_HOST).replace(/\/+$/, ""),
  };
}

/**
 * A short opaque id for the current browser session, used as the PostHog
 * distinct_id so the funnel events group into one person. Anonymous: a random
 * UUID, not derived from any user data, and held only in sessionStorage
 * (cleared when the tab closes).
 */
function getVisitId(): string {
  if (typeof window === "undefined" || typeof sessionStorage === "undefined") {
    return uuid();
  }
  let id = sessionStorage.getItem(VISIT_ID_KEY);
  if (!id) {
    id = uuid();
    sessionStorage.setItem(VISIT_ID_KEY, id);
  }
  return id;
}

function uuid(): string {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  // Fallback for very old browsers. Cryptographically weak, but this is only a
  // funnel-grouping id, never a security token.
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

/**
 * Send a single event to PostHog's /capture/ endpoint, fire-and-forget. When
 * `set` is provided, its keys are written onto the PostHog person via `$set`
 * (this is how an event both records a step and attaches the email). No-op when
 * the channel is dormant. Network/HTTP errors are swallowed: analytics must
 * never surface to, or block, the user who is onboarding.
 */
function capture(event: string, set?: Record<string, string>): void {
  const cfg = getConfig();
  if (!cfg || typeof fetch === "undefined") return;

  const properties: Record<string, unknown> = { source: ONBOARDING_SOURCE };
  if (set) properties.$set = set;

  try {
    void fetch(`${cfg.host}/capture/`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        api_key: cfg.key,
        event,
        distinct_id: getVisitId(),
        properties,
      }),
      keepalive: true,
    }).catch(() => {});
  } catch {
    /* swallow: a dropped analytics event must never break onboarding */
  }
}

/**
 * Record that the email step was seen. Anonymous: distinct id + source only,
 * never an address. Safe to call on step mount.
 */
export function recordOnboardingEmailViewed(): void {
  capture("onboarding_email_viewed");
}

/**
 * Record that the user began typing into the email field. Anonymous: distinct
 * id + source only, never the (partial) address. Call once, on the first
 * keystroke.
 */
export function recordOnboardingEmailStarted(): void {
  capture("onboarding_email_started");
}

/**
 * Record onboarding completion and attach the email to the PostHog person. This
 * is the one call that carries PII, so callers MUST gate it behind the consent
 * checkbox and `isValidEmail`. Best-effort and fire-and-forget.
 */
export function recordOnboardingEmailCaptured(email: string): void {
  const trimmed = email.trim();
  if (!trimmed) return;
  capture("onboarding_email_captured", { email: trimmed });
}
