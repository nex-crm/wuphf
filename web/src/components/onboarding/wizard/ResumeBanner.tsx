// Resume banners shown at the top of the wizard. Extracted from
// Wizard.tsx so the parent component's complexity stays within the
// repo's biome budget. Two flavors:
//   - ResumeBanner: shown on mount when a saved draft was restored.
//     Offers Start-over (clears draft) and Dismiss.
//   - StaleBanner: shown once after loadDraft auto-discards a draft
//     older than the 30-day staleness threshold.
//
// OnboardingBanners is the combined surface Wizard renders — a single
// JSX node so the parent's render stays flat.

import type { OnboardingDraft } from "./onboardingDraft";

interface OnboardingBannersProps {
  resumeDraft: OnboardingDraft | null;
  staleBannerDays: number | null;
  onResetResume: () => void;
  onDismissResume: () => void;
  onDismissStale: () => void;
}

export function OnboardingBanners({
  resumeDraft,
  staleBannerDays,
  onResetResume,
  onDismissResume,
  onDismissStale,
}: OnboardingBannersProps) {
  return (
    <>
      {resumeDraft ? (
        <ResumeBanner
          draft={resumeDraft}
          onReset={onResetResume}
          onDismiss={onDismissResume}
        />
      ) : null}
      {staleBannerDays !== null ? (
        <StaleBanner days={staleBannerDays} onDismiss={onDismissStale} />
      ) : null}
    </>
  );
}

interface ResumeBannerProps {
  draft: OnboardingDraft;
  onReset: () => void;
  onDismiss: () => void;
}

export function ResumeBanner({ draft, onReset, onDismiss }: ResumeBannerProps) {
  return (
    <div
      className="wizard-resume-banner"
      role="status"
      data-testid="onboarding-resume-banner"
    >
      <span>
        Picking up where you left off ·{" "}
        {new Date(draft.savedAt).toLocaleString()}
      </span>
      <button
        type="button"
        className="link-btn"
        onClick={onReset}
        data-testid="onboarding-resume-reset"
      >
        Start over
      </button>
      <button
        type="button"
        className="link-btn"
        onClick={onDismiss}
        data-testid="onboarding-resume-dismiss"
      >
        Dismiss
      </button>
    </div>
  );
}

interface StaleBannerProps {
  days: number;
  onDismiss: () => void;
}

export function StaleBanner({ days, onDismiss }: StaleBannerProps) {
  return (
    <div
      className="wizard-resume-banner"
      role="status"
      data-testid="onboarding-stale-banner"
    >
      <span>
        Cleared an old setup from {days} day{days === 1 ? "" : "s"} ago.
      </span>
      <button
        type="button"
        className="link-btn"
        onClick={onDismiss}
        data-testid="onboarding-stale-dismiss"
      >
        Dismiss
      </button>
    </div>
  );
}
