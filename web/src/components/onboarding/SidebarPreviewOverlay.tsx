/**
 * SidebarPreviewOverlay — renders preview rows in the sidebar during Phase 2
 * onboarding, using the staged FormAnswers from /onboarding/state.
 *
 * Visual language (per spec "Design system / token bindings"):
 *   background:  var(--preview-row-bg)   = lavender tint
 *   border:      var(--preview-row-border) = dashed 1px accent-bg-strong
 *   text:        var(--preview-row-text)  = --text-secondary
 *
 * ARIA: aria-live="polite" region announces each preview row addition.
 * On "seed" phase complete, rows cross-fade out (240ms, reduced-motion: instant).
 *
 * Spec: docs/specs/onboarding-into-office.md "Surface 3 — Sidebar preview overlay"
 */

import { usePreviewOffice } from "./usePreviewOffice";

export function SidebarPreviewOverlay() {
  const preview = usePreviewOffice();

  if (!(preview.active || preview.seeding)) return null;
  if (preview.rows.length === 0 && !preview.workspaceLabel) return null;

  return (
    <div
      className={`sidebar-preview-overlay${preview.seeding ? " sidebar-preview-overlay--seeding" : ""}`}
      aria-live="polite"
      aria-label="Office forming preview"
      data-testid="sidebar-preview-overlay"
    >
      {preview.workspaceLabel ? (
        <div
          className="sidebar-preview-workspace"
          data-testid="sidebar-preview-workspace"
        >
          {preview.workspaceLabel}
        </div>
      ) : null}

      {preview.rows.length > 0 ? (
        <ul className="sidebar-preview-rows">
          {preview.rows.map((row) => (
            <li
              key={`${row.kind}:${row.label}`}
              className={`sidebar-preview-row sidebar-preview-row--${row.kind}`}
              data-testid={`sidebar-preview-row-${row.kind}`}
            >
              <span className="sidebar-preview-row-icon" aria-hidden="true">
                {row.kind === "channel" ? "#" : "@"}
              </span>
              {/* Render as text — never innerHTML */}
              <span className="sidebar-preview-row-label">{row.label}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
