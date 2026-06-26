import { useId, useState } from "react";

import type { TaskVerification, TaskVerificationResult } from "../../api/tasks";

/**
 * VerificationBadge — evidence surface for the machine-checkable
 * definition of done (U1.2).
 *
 * Four states, resolved from the task's `verification` /
 * `verification_result` wire fields:
 *   verified   — last check ran and passed. Success styling; the proof
 *                (check kind + output detail) expands on click.
 *   failing    — last check ran and failed. Danger styling; the failure
 *                detail expands on click.
 *   pending    — a check is defined but has not run yet. Neutral.
 *   unverified — no machine check attached (or kind "none"). Muted and
 *                honest, not alarming: most legacy tasks are unverified.
 */
export type VerificationBadgeState =
  | "verified"
  | "failing"
  | "pending"
  | "unverified";

interface VerificationBadgeProps {
  verification?: TaskVerification;
  result?: TaskVerificationResult;
}

/**
 * Resolve the badge state. A stamped result wins over the spec (the
 * check ran — show its outcome); a spec with kind "none" is explicitly
 * "no check", rendered the same as no spec at all (U1.1: `none` allowed
 * but rendered as UNVERIFIED).
 */
export function resolveVerificationBadgeState(
  verification?: TaskVerification,
  result?: TaskVerificationResult,
): VerificationBadgeState {
  if (result) return result.pass ? "verified" : "failing";
  if (verification && verification.kind !== "none") return "pending";
  return "unverified";
}

const BADGE_LABEL: Record<VerificationBadgeState, string> = {
  verified: "Verified",
  failing: "Check failing",
  pending: "Check pending",
  unverified: "Unverified",
};

const BADGE_GLYPH: Record<VerificationBadgeState, string> = {
  verified: "✓",
  failing: "✕",
  pending: "◷",
  unverified: "—",
};

function formatCheckedAt(iso: string): string {
  if (!iso) return "";
  try {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleString();
  } catch {
    return iso;
  }
}

interface DetailRowProps {
  label: string;
  value: string;
}

function DetailRow({ label, value }: DetailRowProps) {
  return (
    <div className="verification-detail-row">
      <span className="verification-detail-label">{label}</span>
      <pre className="verification-detail-value">{value}</pre>
    </div>
  );
}

export function VerificationBadge({
  verification,
  result,
}: VerificationBadgeProps) {
  const [open, setOpen] = useState(false);
  const detailId = useId();
  const state = resolveVerificationBadgeState(verification, result);
  const label = BADGE_LABEL[state];

  // Unverified: nothing to prove, nothing to expand. A quiet pill with a
  // title is enough — being check-less is the honest default, not an error.
  if (state === "unverified") {
    return (
      <span
        className="verification-badge verification-badge--unverified"
        data-testid="verification-badge"
        data-verification-state="unverified"
        title="No machine check attached to this task"
      >
        <span className="verification-badge-glyph" aria-hidden="true">
          {BADGE_GLYPH.unverified}
        </span>
        {label}
      </span>
    );
  }

  const kind = result?.kind || verification?.kind || "";

  return (
    <span className="verification-badge-wrap">
      <button
        type="button"
        className={`verification-badge verification-badge--${state}`}
        aria-expanded={open}
        aria-controls={detailId}
        aria-label={`${label}. Show check detail.`}
        onClick={() => setOpen((prev) => !prev)}
        data-testid="verification-badge"
        data-verification-state={state}
      >
        <span className="verification-badge-glyph" aria-hidden="true">
          {BADGE_GLYPH[state]}
        </span>
        {label}
      </button>
      {open ? (
        <div
          id={detailId}
          className="verification-badge-detail"
          role="group"
          aria-label="Verification detail"
          data-testid="verification-badge-detail"
        >
          {kind ? <DetailRow label="Check" value={kind} /> : null}
          {verification?.spec ? (
            <DetailRow label="Spec" value={verification.spec} />
          ) : null}
          {result?.detail ? (
            <DetailRow
              label={result.pass ? "Proof" : "Failure detail"}
              value={result.detail}
            />
          ) : null}
          {result?.checked_at ? (
            <DetailRow
              label="Checked"
              value={formatCheckedAt(result.checked_at)}
            />
          ) : null}
          {!(verification?.spec || result?.detail) ? (
            <p className="verification-detail-empty">
              The check has not produced output yet.
            </p>
          ) : null}
        </div>
      ) : null}
    </span>
  );
}
