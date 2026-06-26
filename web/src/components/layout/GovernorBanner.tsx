import { useCallback } from "react";

import type { GovernorStatus } from "../../api/governor";
import { useGovernor, useGovernorAction } from "../../hooks/useGovernor";
import { reasonDetail, reasonHeadline } from "./governorFormat";

// How much extra budget "Continue +budget" grants per click. Matches the
// session defaults so one click roughly doubles a fresh window. The reason
// "budget" covers both the token and cost ceilings, so bump both — otherwise a
// cost-triggered pause would re-trip on the next turn.
const BUDGET_BUMP_TOKENS = 150_000;
const BUDGET_BUMP_COST_USD = 3;

interface GovernorBannerViewProps {
  status: GovernorStatus;
  busy: boolean;
  onResume: () => void;
  onResumeMore: () => void;
  onStop: () => void;
}

/**
 * Presentational banner. Split from the data-wired GovernorBanner so Storybook
 * and tests can drive it with a plain status object.
 */
export function GovernorBannerView({
  status,
  busy,
  onResume,
  onResumeMore,
  onStop,
}: GovernorBannerViewProps) {
  const stopped = status.reason === "stop";
  // "Continue +budget" only makes sense for a budget pause — a manual or
  // turn-count pause is not relieved by more tokens.
  const showBudgetBump = !stopped && status.reason === "budget";
  return (
    <div className="governor-banner" role="alert">
      <div className="governor-banner-content">
        <svg
          aria-hidden="true"
          focusable="false"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="10" y1="9" x2="10" y2="15" />
          <line x1="14" y1="9" x2="14" y2="15" />
        </svg>
        <div className="governor-banner-text">
          <strong>{reasonHeadline(status.reason)}</strong>
          <span>{reasonDetail(status)}</span>
        </div>
      </div>
      <div className="governor-banner-actions">
        <button
          className="btn btn-sm governor-banner-continue"
          onClick={onResume}
          disabled={busy}
          type="button"
        >
          {stopped ? "Resume" : "Continue"}
        </button>
        {showBudgetBump ? (
          <button
            className="btn btn-sm"
            onClick={onResumeMore}
            disabled={busy}
            type="button"
          >
            Continue +budget
          </button>
        ) : null}
        {stopped ? null : (
          <button
            className="btn btn-sm governor-banner-stop"
            onClick={onStop}
            disabled={busy}
            type="button"
          >
            Stop
          </button>
        )}
      </div>
    </div>
  );
}

/**
 * GovernorBanner shows a prominent review checkpoint whenever the session is
 * paused — by a budget/turn gate or a manual pause/stop. Renders nothing while
 * the team is running. Mounted once in the Shell, next to DisconnectBanner.
 */
export function GovernorBanner() {
  const { data: status } = useGovernor();
  const { mutate, isPending } = useGovernorAction();

  const onResume = useCallback(() => mutate({ action: "resume" }), [mutate]);
  const onResumeMore = useCallback(
    () =>
      mutate({
        action: "resume_more",
        options: {
          addTokens: BUDGET_BUMP_TOKENS,
          addCostUsd: BUDGET_BUMP_COST_USD,
        },
      }),
    [mutate],
  );
  const onStop = useCallback(() => mutate({ action: "stop" }), [mutate]);

  if (!status?.paused) return null;

  return (
    <GovernorBannerView
      status={status}
      busy={isPending}
      onResume={onResume}
      onResumeMore={onResumeMore}
      onStop={onStop}
    />
  );
}
