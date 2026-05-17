/**
 * CeoScanChip — displays async website scan status.
 *
 * Read-only (no keyboard interaction per spec — Tab focus but no Enter/Space).
 * Status transitions: scanning → done | failed, driven by SSE updates.
 *
 * Three status strings per spec (backend sets payload.status):
 *   scanning → payload.scanning_label or "Scanning…"
 *   done     → payload.done_label or "Wiki updated ✓"
 *   failed   → payload.failed_label or "Couldn't read that URL"
 *
 * All strings rendered as text, never innerHTML.
 */

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

  return (
    <div
      className={`ceo-scan-chip ceo-scan-chip--${status}`}
      data-testid="ceo-scan-chip"
      aria-live="polite"
      aria-label={label}
    >
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
  );
}
