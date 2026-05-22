/**
 * CeoScanChip — displays async website scan status.
 *
 * Read-only for `scanning` and `done`. The `failed` status surfaces the
 * underlying error reason and exposes two recovery CTAs (#934):
 *   - "Try another URL" — re-enters PhaseWebsite so the user can supply a
 *     different URL. The form-field still holds the previous URL string
 *     in /onboarding/state so the user can edit it inline.
 *   - "Skip and continue" — advances to PhaseBlueprint, same destination
 *     as the prior silent auto-skip.
 *
 * Three status strings per spec (backend sets payload.status):
 *   scanning → payload.scanning_label or "Scanning…"
 *   done     → payload.done_label or "Wiki updated ✓"
 *   failed   → payload.failed_label or "Couldn't read that URL"
 *
 * All strings rendered as text, never innerHTML.
 */

import { useState } from "react";

import { post } from "../../../api/client";
import { showNotice } from "../../ui/Toast";
import type { CeoScanChipPayload, ScanStatus } from "../../onboarding/types";

interface CeoScanChipProps {
  payload: CeoScanChipPayload;
}

function scanLabel(payload: CeoScanChipPayload, status: ScanStatus): string {
  switch (status) {
    case "scanning":
      return payload.scanning_label ?? `Scanning ${payload.url}…`;
    case "done":
      return payload.done_label ?? "Wiki updated ✓";
    case "failed":
      return payload.failed_label ?? "Couldn’t read that URL";
  }
}

export function CeoScanChip({ payload }: CeoScanChipProps) {
  const { status } = payload;
  const label = scanLabel(payload, status);
  const [recovering, setRecovering] = useState<"retry" | "skip" | null>(null);

  async function transitionTo(next: "website" | "blueprint") {
    if (recovering) return;
    setRecovering(next === "website" ? "retry" : "skip");
    try {
      await post("/onboarding/transition", { phase: next });
    } catch (err: unknown) {
      const message =
        err instanceof Error
          ? err.message
          : "Could not advance the onboarding flow.";
      showNotice(message, "error");
      setRecovering(null);
    }
    // Leave `recovering` set on success — the broker will swap the pending
    // suggestion to the new phase's card and this chip will unmount.
  }

  return (
    <div
      className={`ceo-scan-chip ceo-scan-chip--${status}`}
      data-testid="ceo-scan-chip"
      aria-live="polite"
      aria-label={label}
    >
      <div className="ceo-scan-chip-row">
        {status === "scanning" ? (
          <span className="ceo-scan-chip-spinner" aria-hidden="true" />
        ) : (
          <span
            className={`ceo-scan-chip-icon ceo-scan-chip-icon--${status}`}
            aria-hidden="true"
          >
            {status === "done" ? "✓" : "✗"}
          </span>
        )}
        {/* Render URL and label as plain text — never as HTML */}
        <span className="ceo-scan-chip-label">{label}</span>
      </div>
      {status === "failed" && payload.error_reason ? (
        <p
          className="ceo-scan-chip-reason"
          data-testid="ceo-scan-chip-reason"
        >
          {payload.error_reason}
        </p>
      ) : null}
      {status === "failed" ? (
        <div className="ceo-scan-chip-actions" data-testid="ceo-scan-chip-actions">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={recovering !== null}
            onClick={() => transitionTo("website")}
            data-testid="ceo-scan-chip-retry"
            aria-label="Try a different website URL"
          >
            {recovering === "retry" ? "Reopening…" : "Try another URL"}
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={recovering !== null}
            onClick={() => transitionTo("blueprint")}
            data-testid="ceo-scan-chip-skip"
            aria-label="Skip the website scan and continue"
          >
            {recovering === "skip" ? "Skipping…" : "Skip and continue"}
          </button>
        </div>
      ) : null}
    </div>
  );
}
