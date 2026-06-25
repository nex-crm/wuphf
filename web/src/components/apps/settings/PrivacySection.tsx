import type { CSSProperties } from "react";

import { setAnalyticsConsent, track } from "../../../lib/analytics";
import { Field } from "./components";
import { styles } from "./styles";
import type { SectionProps } from "./types";

// ─── Privacy & Analytics section ────────────────────────────────────────

const consentToggleRow: CSSProperties = {
  display: "flex",
  alignItems: "flex-start",
  gap: 8,
  cursor: "pointer",
};
const consentCheckbox: CSSProperties = {
  marginTop: 2,
  width: 16,
  height: 16,
  flexShrink: 0,
  accentColor: "var(--accent)",
  cursor: "pointer",
};

/**
 * Two independent consent toggles for product analytics. Both default ON and
 * apply live (no reload): a flip stops/starts capture or recording immediately
 * and persists to config. Honest copy states plainly that analytics never
 * collects content and that recordings mask everything you type (PostHog's
 * default input masking). See docs/specs/product-analytics.md.
 */
export function PrivacySection({ cfg, save }: SectionProps) {
  const telemetry = cfg.analytics_telemetry_enabled !== false;
  const recording = cfg.analytics_session_recording_enabled !== false;
  // The toggles only do something when analytics is actually configured (a
  // PostHog key resolved at build time or injected by the operator).
  const configured = cfg.analytics_configured === true;

  const setTelemetry = (enabled: boolean) => {
    // Apply live first so an opt-out takes effect before the consent event;
    // that keeps us from sending a tracking event the instant someone opts out.
    setAnalyticsConsent({ telemetry: enabled });
    track("analytics_consent_set", {
      channel: "telemetry",
      enabled,
      surface: "settings",
    });
    void save({ analytics_telemetry_enabled: enabled });
  };
  const setRecording = (enabled: boolean) => {
    setAnalyticsConsent({ recording: enabled });
    track("analytics_consent_set", {
      channel: "recording",
      enabled,
      surface: "settings",
    });
    void save({ analytics_session_recording_enabled: enabled });
  };

  return (
    <div>
      <h2 style={styles.sectionTitle}>Privacy &amp; Analytics</h2>
      <p style={styles.sectionDesc}>
        Two independent, optional controls, both on by default. Product
        analytics never collects your content, and session recordings mask
        everything you type — passwords, keys, and form fields. Changes take
        effect immediately.
      </p>

      <Field
        label="Product analytics"
        hint="Anonymous usage events — counts and shapes of what you do, never the content. Used to understand which flows work and which need help."
      >
        <label style={consentToggleRow}>
          <input
            type="checkbox"
            style={consentCheckbox}
            checked={telemetry}
            onChange={(e) => setTelemetry(e.target.checked)}
            data-testid="settings-telemetry-toggle"
          />
          <span>Share anonymous product analytics</span>
        </label>
      </Field>

      <Field
        label="Session recording"
        hint="Replays that mask everything you type — passwords, keys, and form fields — while capturing layout, clicks, and navigation to fix rough edges."
      >
        <label style={consentToggleRow}>
          <input
            type="checkbox"
            style={consentCheckbox}
            checked={recording}
            onChange={(e) => setRecording(e.target.checked)}
            data-testid="settings-recording-toggle"
          />
          <span>Allow session recordings (typed text masked)</span>
        </label>
      </Field>

      {configured ? null : (
        <p style={styles.sectionDesc}>
          Analytics is not configured on this install, so these settings have no
          effect until an operator sets a PostHog key (WUPHF_POSTHOG_KEY). Your
          choices are still saved.
        </p>
      )}
    </div>
  );
}
